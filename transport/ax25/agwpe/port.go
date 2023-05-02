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
	maxFrame     int
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
			conn.inbound = true
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
	capabilities, err := p.getCapabilities(ctx)
	if err != nil {
		debugf("failed to get port capabilities: %v", err)
		p.maxFrame = 7 // Set a reasonable default.
	} else {
		p.maxFrame = int(capabilities.MaxFrame)
	}

	// QtSoundModem responds with a 'x' frame instead of the expected 'X' frame.
	ack := p.demux.NextFrame(kindRegister, 'x')
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
		debugf("Port %d registered as %s. MAXFRAME=%d", p.port, p.mycall, p.maxFrame)
		return nil
	}
}

type portCapabilities struct {
	_        byte  // On air baud rate (0=1200/1=2400/2=4800/3=9600…)
	_        byte  // Traffic level (if 0xFF the port is not in autoupdate mode)
	_        byte  // TX Delay
	_        byte  // TX Tail
	_        byte  // Persist
	_        byte  // SlotTime
	MaxFrame uint8 // MaxFrame
	_        byte  // How Many connections are active on this port
	_        int32 // HowManyBytes (received in the last 2 minutes)
}

func (p *Port) getCapabilities(ctx context.Context) (*portCapabilities, error) {
	resp := p.demux.NextFrame(kindPortCapabilities)
	if err := p.write(portCapabilitiesFrame(p.port)); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case f := <-resp:
		var v portCapabilities
		if err := binary.Read(bytes.NewReader(f.Data), binary.LittleEndian, &v); err != nil {
			return nil, err
		}
		return &v, nil
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

func (p *Port) SendUI(data []byte, dst string) error {
	if p.demux.isClosed() {
		return ErrPortClosed
	}
	f := unprotoInformationFrame(p.mycall, dst, p.port, data)
	return p.tnc.write(f)
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
