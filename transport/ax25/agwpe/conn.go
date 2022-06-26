package agwpe

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

type Conn struct {
	p          *Port
	demux      *demux
	dataFrames <-chan frame

	srcCall, dstCall string
	via              []string

	readDeadline, writeDeadline time.Time
}

func newConn(p *Port, dstCall string, via ...string) *Conn {
	demux := p.demux.Chain(framesFilter{call: callsignFromString(dstCall)})
	disconnect := demux.NextFrame(kindDisconnect)
	dataFrames, cancelData := demux.Frames(10, framesFilter{kinds: []kind{kindConnectedData}})
	go func() {
		<-disconnect
		debugf("disconnect frame received - connection teardown...")
		cancelData()
		demux.Close()
	}()
	return &Conn{
		p:          p,
		demux:      demux,
		srcCall:    p.mycall,
		dstCall:    dstCall,
		via:        via,
		dataFrames: dataFrames,
	}
}

func (c *Conn) numOutstandingFrames() (int, error) {
	// TODO: From Direwolf 1.5 we could get connection specific value using the 'Y' frame.
	return c.p.numOutstandingFrames()
}

// Flush implements the transport.Flusher interface.
func (c *Conn) Flush() error {
	debugf("flushing...")
	for {
		n, err := c.p.numOutstandingFrames()
		if err != nil {
			return err
		}
		if n == 0 {
			debugf("flushed")
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (c *Conn) Write(p []byte) (int, error) {
	// TODO: c.writeDeadline
	cp := make([]byte, len(p))
	copy(cp, p)
	f := connectedDataFrame(c.p.port, c.srcCall, c.dstCall, p)
	if err := c.p.write(f); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *Conn) Read(p []byte) (int, error) {
	ctx := context.Background()
	if !c.readDeadline.IsZero() {
		var cancel func()
		ctx, cancel = context.WithDeadline(ctx, c.readDeadline)
		defer cancel()
	}
	select {
	case <-ctx.Done():
		// TODO (read timeout error)
		return 0, ctx.Err()
	case f, ok := <-c.dataFrames:
		if !ok {
			return 0, io.EOF
		}
		if len(p) < len(f.Data) {
			panic("buffer overflow")
		}
		copy(p, f.Data)
		return len(f.Data), nil
	}
}

func (c *Conn) Close() error {
	if c.demux.isClosed() {
		return nil
	}
	c.Flush()
	defer c.demux.Close()
	return c.p.write(disconnectFrame(c.srcCall, c.dstCall, c.p.port))
	// TODO: Block until disconnect ack
}

func (c *Conn) connect(ctx context.Context) error {
	connectFrame := func() frame {
		if len(c.via) > 0 {
			return connectViaFrame(c.srcCall, c.dstCall, c.p.port, c.via)
		}
		return connectFrame(c.srcCall, c.dstCall, c.p.port)
	}

	ack := c.demux.NextFrame(kindConnect, kindDisconnect)
	if err := c.p.write(connectFrame()); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		c.p.write(disconnectFrame(c.srcCall, c.dstCall, c.p.port))
		return ctx.Err()
	case f, ok := <-ack:
		if !ok {
			return ErrPortClosed
		}
		switch f.DataKind {
		case kindConnect:
			if !bytes.HasPrefix(f.Data, []byte("*** CONNECTED With ")) {
				c.p.write(disconnectFrame(c.srcCall, c.dstCall, c.p.port))
				return fmt.Errorf("connect precondition failed")
			}
			return nil
		case kindDisconnect:
			return fmt.Errorf("%s", strings.TrimSpace(strFromBytes(f.Data)))
		default:
			panic("impossible")
		}
	}
}

func (c *Conn) LocalAddr() net.Addr  { return addr{dest: c.srcCall} }
func (c *Conn) RemoteAddr() net.Addr { return addr{dest: c.dstCall, digis: c.via} }

func (c *Conn) SetWriteDeadline(t time.Time) error { c.writeDeadline = t; return nil }
func (c *Conn) SetReadDeadline(t time.Time) error  { c.readDeadline = t; return nil }
func (c *Conn) SetDeadline(t time.Time) error      { c.readDeadline, c.writeDeadline = t, t; return nil }
