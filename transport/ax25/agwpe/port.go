package agwpe

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/la5nta/wl2k-go/transport"
)

// Port represents a registered AGWPE Port.
type Port struct {
	tnc          *TNC
	port         uint8
	mycall       string
	demux        *demux
	inboundConns <-chan *Conn
}

func newPort(tnc *TNC, port uint8, mycall string) *Port {
	demux := tnc.demux.Chain(framesFilter{port: &port})
	p := &Port{
		tnc:    tnc,
		port:   port,
		mycall: mycall,
		demux:  demux,
	}
	p.inboundConns = p.handleInbound()
	return p
}

func (p *Port) handleInbound() <-chan *Conn {
	conns := make(chan *Conn)
	go func() {
		defer close(conns)
		connects, cancel := p.demux.Frames(1, framesFilter{
			kinds: []kind{kindConnect},
			to:    callsignFromString(p.mycall),
		})
		defer cancel()
		for f := range connects {
			if !bytes.HasPrefix(f.Data, []byte("*** CONNECTED To ")) {
				debugf("inbound connection from %s not initiated by remote. ignoring.", f.From)
				continue
			}
			conn := newConn(p, f.From.String())
			select {
			case conns <- conn:
				debugf("inbound connection from %s accepted", f.From)
			default:
				// No one is calling Listener.Accept() just now. Close it.
				conn.Close()
				debugf("inbound connection from %s refused", f.From)
			}
		}
	}()
	return conns
}

func (p *Port) register(ctx context.Context) error {
	ack := p.demux.NextFrame(kindRegister)
	if err := p.write(registerCallsignFrame(p.mycall, p.port)); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case f := <-ack:
		if len(f.Data) != 1 {
			return fmt.Errorf("unexpected registration response (%c)", f.DataKind)
		}
		if f.Data[0] != 0x01 {
			return fmt.Errorf("callsign in use")
		}
		debugf("Port %d registered as %s", p.port, p.mycall)
		return nil
	}
}

func (p *Port) write(f frame) error {
	if f.Port != p.port {
		panic("incorrect port in frame")
	}
	return p.tnc.write(f)
}

func (p *Port) Close() error {
	p.write(unregisterCallsignFrame(p.mycall, p.port))
	return p.demux.Close()
}

func (p *Port) DialURLContext(ctx context.Context, url *transport.URL) (net.Conn, error) {
	if url.Scheme != "ax25" && url.Scheme != "ax25+agwpe" && url.Scheme != "agwpe+ax25" {
		return nil, fmt.Errorf("unsupported scheme '%s'", url.Scheme)
	}
	return p.DialContext(ctx, url.Target, url.Digis...)
}

func (p *Port) DialContext(ctx context.Context, target string, via ...string) (net.Conn, error) {
	if p.demux.isClosed() {
		return nil, ErrPortClosed
	}
	c := newConn(p, target, via...)
	if err := c.connect(ctx); err != nil {
		c.demux.Close()
		return nil, err
	}
	return c, nil
}

func (p *Port) Listen() (net.Listener, error) {
	if p.demux.isClosed() {
		return nil, ErrPortClosed
	}
	return newListener(p), nil
}

func (p *Port) numOutstandingFrames() (int, error) {
	resp := p.demux.NextFrame(kindOutstandingFramesForPort)
	f := outstandingFramesForPortFrame(p.port)
	if err := p.write(f); err != nil {
		return 0, err
	}
	select {
	case f, ok := <-resp:
		if !ok {
			return 0, nil
		}
		if len(f.Data) != 4 {
			return 0, fmt.Errorf("'%c' frame with unexpected data length", f.DataKind)
		}
		return int(binary.LittleEndian.Uint32(f.Data)), nil
	case <-time.After(3 * time.Second):
		debugf("'%c' answer timeout. frame kind probably unsupported by TNC.", f.DataKind)
		return 0, fmt.Errorf("'%c' frame timeout", f.DataKind)
	}
}
