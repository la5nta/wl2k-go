// Copyright 2021 Jeremy Bush (N8JJA). All rights reserved.
// Copyright 2026 Rhizomatica. All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package mercury

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// conn is the data-port connection handed to clients. It implements net.Conn.
//
// The data port carries raw application payload in both directions; the modem
// handles all segmentation, so no framing is applied here.
type conn struct {
	*Modem
	remoteCall string

	lastWrite time.Time
	closeOnce sync.Once
	closing   bool
}

func (m *Modem) newConn(remoteCall string) *conn {
	m.dataConn.SetDeadline(time.Time{}) // Reset any previous deadlines
	return &conn{
		Modem:      m,
		remoteCall: remoteCall,
	}
}

// Flush blocks until the modem's TX buffer is empty.
func (v *conn) Flush() error {
	debugPrint("flushing...")
	defer debugPrint("flushed")
	cmds, cancel := v.cmds.Subscribe("DISCONNECTED", "BUFFER")
	defer cancel()
	if v.closing {
		return nil
	}

	timeout := time.NewTimer(time.Minute)
	defer timeout.Stop()

	count := v.bufferCount.get()
	for count > 0 {
		select {
		case cmd, ok := <-cmds:
			switch {
			case !ok:
				return ErrModemClosed
			case cmd == "DISCONNECTED":
				return io.EOF
			default:
				if !timeout.Stop() {
					<-timeout.C
				}
				timeout.Reset(time.Minute)
				count = parseBuffer(cmd)
			}
		case <-timeout.C:
			return errors.New("flush: buffer timeout")
		}
	}
	return nil
}

// SetDeadline sets the read and write deadlines associated with the connection.
func (v *conn) SetDeadline(t time.Time) error { return v.dataConn.SetDeadline(t) }

// SetWriteDeadline sets the write deadline associated with the connection.
func (v *conn) SetWriteDeadline(t time.Time) error { return v.dataConn.SetWriteDeadline(t) }

// SetReadDeadline sets the read deadline associated with the connection.
func (v *conn) SetReadDeadline(t time.Time) error { return v.dataConn.SetReadDeadline(t) }

// LocalAddr returns the local network address.
func (v *conn) LocalAddr() net.Addr { return Addr{v.myCall} }

// RemoteAddr returns the remote network address.
func (v *conn) RemoteAddr() net.Addr { return Addr{v.remoteCall} }

// Close closes the connection.
//
// Any blocked Read or Write operations will be unblocked and return errors.
func (v *conn) Close() error {
	var err error
	v.closeOnce.Do(func() {
		debugPrint("closing connection...")
		if v.Modem.closed {
			err = ErrModemClosed
			return
		}
		defer func() {
			// Discard any remaining data
			v.dataConn.SetReadDeadline(time.Now().Add(time.Second))
			n, _ := io.Copy(io.Discard, v.dataConn)
			debugPrint("close: discarded %d bytes of remaining data", n)
		}()
		v.closing = true
		connectChange, cancel := v.cmds.Subscribe("DISCONNECTED")
		defer cancel()
		if v.connectedState == disconnected {
			return
		}

		// Workaround for race condition between write and close (cmd and data
		// are not synchronized, being on separate TCP sockets): the modem
		// flushes the TX buffer before closing on DISCONNECT, but we need to
		// make sure the last data written has reached the modem first.
		if dur := time.Since(v.lastWrite); dur < 2*time.Second {
			<-time.After(2*time.Second - dur)
		}

		v.writeCmd("DISCONNECT")
		select {
		case _, ok := <-connectChange:
			if !ok {
				err = ErrModemClosed
			}
			// Happy path: connection gracefully closed.
			return
		case <-time.After(60 * time.Second):
			debugPrint("disconnect timeout - aborting connection")
			v.Abort()
			err = fmt.Errorf("disconnect timeout - connection aborted")
			return
		}
	})
	return err
}

func (v *conn) Read(b []byte) (n int, err error) {
	connectChange, cancel := v.cmds.Subscribe("DISCONNECTED")
	defer cancel()
	if v.connectedState != connected {
		debugPrint("read: not connected")
		return 0, io.EOF
	}

	type res struct {
		n   int
		err error
	}
	ready := make(chan res, 1)
	go func() {
		defer close(ready)
		v.dataConn.SetReadDeadline(time.Time{}) // Disable read deadline
		n, err = v.dataConn.Read(b)
		if err != nil {
			debugPrint("read error: %v", err)
		}
		ready <- res{n, err}
	}()
	select {
	case res := <-ready:
		return res.n, res.err
	case _, ok := <-connectChange:
		debugPrint("read: disconnected while reading")
		if !ok {
			return 0, ErrModemClosed
		}
		// Workaround for race condition between cmd and data conn. The data was
		// sent before the DISCONNECT, but is received out of order since the
		// two streams are independent.
		select {
		case res := <-ready:
			debugPrint("read: got data (%d bytes) after disconnect (err: %v)", res.n, res.err)
			if res.err != nil {
				return res.n, io.EOF
			}
			return res.n, nil
		case <-time.After(2 * time.Second):
			debugPrint("read: timeout waiting for data after disconnect")
			v.dataConn.SetReadDeadline(time.Now())
			return 0, io.EOF
		}
	}
}

func (v *conn) Write(b []byte) (int, error) {
	cmds, cancel := v.cmds.Subscribe("DISCONNECTED", "BUFFER")
	defer cancel()
	if v.connectedState != connected {
		return 0, io.EOF
	}

	// Throttle to match the transmitted data rate by blocking if the tx buffer
	// grows much larger than the payloads being sent.
	//
	// A magic number: we don't know the actual on-air packet length, max
	// outstanding frames, or BUFFER update cadence. Too small causes needless
	// IDLE time; too large makes Close() block for a long time. This value works
	// well enough for Mercury (and the VARA modems this code was adapted from).
	const magicNumber = 7

	bufferTimeout := time.NewTimer(time.Minute)
	defer bufferTimeout.Stop()
	bufferCount := v.bufferCount.get()
	for bufferCount >= magicNumber*len(b) && !v.closing {
		debugPrint("write: buffer full (%d >= %d)", bufferCount, magicNumber*len(b))
		select {
		case cmd, ok := <-cmds:
			switch {
			case !ok:
				return 0, ErrModemClosed
			case cmd == "DISCONNECTED":
				debugPrint("write: state changed while waiting for buffer space")
				return 0, io.EOF
			default:
				bufferCount = parseBuffer(cmd)
				if !bufferTimeout.Stop() {
					<-bufferTimeout.C
				}
				bufferTimeout.Reset(time.Minute)
			}
		case <-bufferTimeout.C:
			return 0, fmt.Errorf("write: buffer timeout")
		}
	}

	// The modem keeps accepting data after a DISCONNECT command has been sent,
	// adding it to the TX buffer queue, and keeps the connection open until the
	// buffer is empty. Make sure we don't keep feeding the buffer after we've
	// sent DISCONNECT by blocking until the disconnect is complete.
	if v.closing && v.connectedState == connected {
		debugPrint("write: waiting for disconnect to complete...")
		for cmd := range cmds {
			if cmd == "DISCONNECTED" {
				break
			}
		}
		debugPrint("write: disconnect complete")
		return 0, io.EOF
	}

	debugPrint("write: sending %d bytes", len(b))
	v.bufferCount.incr(len(b))
	v.lastWrite = time.Now()
	return v.dataConn.Write(b)
}

// TxBufferLen implements the transport.TxBuffer interface. It returns the
// current number of bytes in the TX buffer queue or in transit to the modem.
func (v *conn) TxBufferLen() int { return v.bufferCount.get() }
