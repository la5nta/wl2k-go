// Copyright 2021 Jeremy Bush (N8JJA). All rights reserved.
// Copyright 2026 Rhizomatica. All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package mercury

import (
	"errors"
	"net"
	"sync"
)

var ErrListenerClosed = errors.New("listener closed")

type listener struct {
	*Modem

	closeOnce sync.Once
	done      chan struct{}
}

// Listen enables listening for incoming ARQ connections and returns a
// net.Listener that yields them.
func (m *Modem) Listen() (net.Listener, error) {
	if m.closed {
		return nil, ErrModemClosed
	}
	if err := m.writeCmd("LISTEN ON"); err != nil {
		return nil, err
	}
	return &listener{Modem: m, done: make(chan struct{})}, nil
}

// Accept waits for and returns the next inbound connection.
func (ln *listener) Accept() (net.Conn, error) {
	select {
	case conn, ok := <-ln.inboundConns:
		debugPrint("Accept() got: %v %v", conn, ok)
		if !ok {
			return nil, ErrModemClosed
		}
		return conn, nil
	case <-ln.done:
		return nil, ErrListenerClosed
	}
}

// Addr returns the listener's network address.
func (ln *listener) Addr() net.Addr { return Addr{ln.myCall} }

// Close closes the listener; any blocked Accept operations will be unblocked.
func (ln *listener) Close() error {
	var err error
	ln.closeOnce.Do(func() {
		err = ln.writeCmd("LISTEN OFF")
		if err == nil {
			close(ln.done)
		}
	})
	return err
}
