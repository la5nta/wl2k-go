// Copyright 2015 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package winmor

import (
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/la5nta/wl2k-go/transport"
)

type tncConn struct {
	net.Conn
	tnc *TNC

	remoteAddr Addr
	localAddr  Addr

	// The flushLock is used to keep track of the "out queued" buffer.
	//
	// It is locked on write, and Flush() will block until it's unlocked.
	// It is the control loop's responsibility to unlock this lock when buffer reached zero.
	flushLock lock

	mu       sync.Mutex
	buffers  []int
	nWritten int
}

func (conn *tncConn) SetRobust(r bool) error { return conn.tnc.SetRobust(r) }

func (conn *tncConn) Write(p []byte) (int, error) {
	n, err := conn.Conn.Write(p)

	conn.mu.Lock()
	conn.nWritten += n
	conn.flushLock.Lock()
	conn.mu.Unlock()

	return n, err
} // TODO: Maybe wait if out buffer queue is larger than some value (maybe 128?)

func (conn *tncConn) Flush() error {
	conn.flushLock.Wait()
	return nil
} // bug(martinhpedersen): Should check for connection error instead of returning nil

// TxBufferLen returns the number of bytes in the out buffer queue.
func (conn *tncConn) TxBufferLen() int {
	conn.mu.Lock()
	defer conn.mu.Unlock()

	if conn.buffers == nil {
		return 0
	}

	// We don't use BufferOutQueued, because it may be outdated (not updated since last Write call).
	return conn.nWritten - conn.buffers[BufferOutConfirmed]
}

func (conn *tncConn) updateBuffers(b []int) {
	if conn == nil {
		return
	}

	conn.mu.Lock()
	defer conn.mu.Unlock()
	conn.buffers = b

	if b[BufferOutConfirmed] >= conn.nWritten && b[BufferOutQueued] == 0 {
		conn.flushLock.Unlock()
	}
}

// DialURL dials winmor:// URLs
func (tnc *TNC) DialURL(url *transport.URL) (net.Conn, error) {
	if url.Scheme != "winmor" {
		return nil, transport.ErrUnsupportedScheme
	}
	return tnc.Dial(url.Target)
}

func (tnc *TNC) Dial(targetcall string) (net.Conn, error) {
	if err := tnc.connect(targetcall); err != nil {
		return nil, err
	}

	time.Sleep(200 * time.Millisecond) // To give WINMOR time to listen
	dataConn, err := net.Dial("tcp", tnc.connAddr)
	if err != nil {
		return nil, err
	}

	mycall, err := tnc.MyCall()
	if err != nil {
		return nil, fmt.Errorf("Error when getting mycall: %s", err)
	}

	tnc.data = &tncConn{
		Conn:       dataConn,
		remoteAddr: Addr{targetcall},
		localAddr:  Addr{mycall},
		tnc:        tnc,
	}

	// Try to minimize read/write buffer on connection.
	tnc.data.Conn.(*net.TCPConn).SetReadBuffer(0)
	tnc.data.Conn.(*net.TCPConn).SetWriteBuffer(0)

	return tnc.data, nil
}

func (conn *tncConn) Close() error {
	if conn.Conn == nil {
		return nil
	}

	conn.Flush() // TODO: Timeout and return error here?

	r := conn.tnc.in.Listen()
	defer r.Close()

	conn.tnc.out <- fmt.Sprint(cmdDisconnect)
	for { // Wait for TNC to disconnect
		select {
		case msg := <-r.Msgs():
			if msg.cmd == cmdDisconnect {
				// The command echo
			} else if msg.cmd == cmdNewState && msg.State() == Disconnected {
				// The control loop have already closed the data connection
				return nil
				//return conn.Conn.Close()
			}
		case <-time.After(15 * time.Second): // Sensible timeout
			// Timeout
			if debugEnabled() {
				log.Printf("conn.Close(): timeout while waiting for newstate. Sending %s", cmdDirtyDisconnect)
			}
			conn.tnc.out <- fmt.Sprint(cmdDirtyDisconnect)
		}
	}
	return errors.New("TNC hung up while waiting for requested disconnect")
}

func (conn *tncConn) RemoteAddr() net.Addr {
	return conn.remoteAddr
}

func (conn *tncConn) LocalAddr() net.Addr {
	return conn.localAddr
}
