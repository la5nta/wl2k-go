package agwpe

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

type Conn struct {
	p          *Port
	demux      *demux
	inbound    bool
	dataFrames <-chan frame

	srcCall, dstCall string
	via              []string

	readDeadline, writeDeadline time.Time

	closing bool // Guard against Write calls once Close() is called.
}

func newConn(p *Port, dstCall string, via ...string) *Conn {
	demux := p.demux.Chain(framesFilter{call: callsignFromString(dstCall)})
	disconnect := demux.NextFrame(kindDisconnect)
	dataFrames, cancelData := demux.Frames(10, framesFilter{kinds: []kind{kindConnectedData}})
	go func() {
		_, ok := <-disconnect
		if !ok {
			debugf("demux closed while waiting for disconnect frame")
			return
		}
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

// TODO: How can we tell?
func notDirewolf() bool { return false }

// This requires Direwolf >= 1.4, but reliability improved as late as 1.6. It's required in order to flush tx buffers before link teardown.
func (c *Conn) numOutstandingFrames() (int, error) {
	if c.demux.isClosed() {
		return 0, io.EOF
	}
	resp := c.demux.NextFrame(kindOutstandingFramesForConn)

	// According to the docs, the CallFrom and CallTo "should reflect the order used to start the connection".
	// However, neither Direwolf nor QtSoundModem seems to implement this...
	from, to := c.srcCall, c.dstCall
	if c.inbound && notDirewolf() {
		from, to = to, from
	}
	f := outstandingFramesForConnFrame(c.p.port, from, to)
	if err := c.p.write(f); err != nil {
		return 0, err
	}
	select {
	case f, ok := <-resp:
		if !ok {
			return 0, io.EOF
		}
		if len(f.Data) != 4 {
			return 0, fmt.Errorf("'%c' frame with unexpected data length", f.DataKind)
		}
		return int(binary.LittleEndian.Uint32(f.Data)), nil
	case <-time.After(30 * time.Second):
		debugf("'%c' answer timeout. frame kind probably unsupported by TNC.", f.DataKind)
		return 0, fmt.Errorf("'%c' frame timeout", f.DataKind)
	}
}

// Flush implements the transport.Flusher interface.
func (c *Conn) Flush() error {
	debugf("flushing...")
	defer debugf("flushed")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return c.waitOutstandingFrames(ctx, func(n int) bool { return n == 0 })
}

// waitOutstandingFrames blocks until the number of outstanding frames is less than the given limit.
func (c *Conn) waitOutstandingFrames(ctx context.Context, stop func(int) bool) error {
	errs := make(chan error, 1)
	go func() {
		defer close(errs)
		tick := time.NewTicker(200 * time.Millisecond)
		defer tick.Stop()
		for {
			n, err := c.numOutstandingFrames()
			if err != nil {
				errs <- err
				return
			}
			if stop(n) {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				continue
			}
		}
	}()
	select {
	case <-ctx.Done():
		debugf("outstanding frames wait ended: %v", ctx.Err())
		return ctx.Err()
	case err := <-errs:
		if err != nil {
			debugf("outstanding frames wait error: %v", err)
		}
		return err
	}
}

func (c *Conn) Write(p []byte) (int, error) {
	if c.closing {
		return 0, io.EOF
	}

	ctx := context.Background()
	if !c.writeDeadline.IsZero() {
		var cancel func()
		ctx, cancel = context.WithDeadline(ctx, c.writeDeadline)
		defer cancel()
	}
	// Block until we have no more than MAXFRAME outstanding frames, so we don't keep filling the TX buffer.
	// bug(martinhpedersen): MAXFRAME is not always correct. EMAXFRAME could apply for this connection, but there is no way of knowing.
	if err := c.waitOutstandingFrames(ctx, func(n int) bool { return n <= c.p.maxFrame }); err != nil {
		return 0, err
	}
	cp := make([]byte, len(p))
	copy(cp, p)
	f := connectedDataFrame(c.p.port, c.srcCall, c.dstCall, p)
	if err := c.p.write(f); err != nil {
		return 0, err
	}
	// Block until we see at least one outstanding frame to avoid race condition if Flush() is called immediately after this.
	if err := c.waitOutstandingFrames(ctx, func(n int) bool { return n > 0 }); err != nil {
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
	if c.closing || c.demux.isClosed() {
		return nil
	}
	c.closing = true
	defer c.demux.Close()
	if err := c.Flush(); err == io.EOF {
		debugf("link closed while flushing")
		return nil
	}
	ack := c.demux.NextFrame(kindDisconnect)
	if err := c.p.write(disconnectFrame(c.srcCall, c.dstCall, c.p.port)); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute) // TODO
	defer cancel()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ack:
		return nil
	}
}

func (c *Conn) connect(ctx context.Context) error {
	// We handle context cancellation by sending a disconect to the TNC. This will
	// cause the TNC to send a disconnect frame back to us if the TNC supports it, or
	// keep dialing until connect or timeout. The latter is the case with Direwolf as
	// of 2021-05-07. This will be fixed in a future release of Direwolf.
	done := make(chan struct{}, 1)
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			debugf("context cancellation - sending disconnect frame...")
			c.p.write(disconnectFrame(c.srcCall, c.dstCall, c.p.port))
		case <-done:
			debugf("dial completed - context cancellation no longer possible")
		}
	}()

	ack := c.demux.NextFrame(kindConnect, kindDisconnect)
	if err := c.p.write(connectFrame(c.srcCall, c.dstCall, c.p.port, c.via)); err != nil {
		return err
	}
	f, ok := <-ack
	if !ok {
		return ErrPortClosed
	}
	done <- struct{}{} // Dial cancellation is no longer possible.
	switch f.DataKind {
	case kindConnect:
		if !bytes.HasPrefix(f.Data, []byte("*** CONNECTED With ")) {
			c.p.write(disconnectFrame(c.srcCall, c.dstCall, c.p.port))
			return fmt.Errorf("connect precondition failed")
		}
		return nil
	case kindDisconnect:
		if err := ctx.Err(); err != nil {
			return err
		}
		return fmt.Errorf("%s", strings.TrimSpace(strFromBytes(f.Data)))
	default:
		panic("impossible")
	}
}

func (c *Conn) LocalAddr() net.Addr  { return addr{dest: c.srcCall} }
func (c *Conn) RemoteAddr() net.Addr { return addr{dest: c.dstCall, digis: c.via} }

func (c *Conn) SetWriteDeadline(t time.Time) error { c.writeDeadline = t; return nil }
func (c *Conn) SetReadDeadline(t time.Time) error  { c.readDeadline = t; return nil }
func (c *Conn) SetDeadline(t time.Time) error      { c.readDeadline, c.writeDeadline = t, t; return nil }
