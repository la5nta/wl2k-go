// Copyright 2016 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package transport

import (
	"context"
	"net"
)

type Flusher interface {
	// Flush flushes the transmit buffers of the underlying modem.
	Flush() error
}

type TxBuffer interface {
	// TransmitBufferLen returns the number of bytes in the out buffer queue.
	TxBufferLen() int
}

type Robust interface {
	// Enables/disables robust mode.
	SetRobust(r bool) error
}

// A BusyChannelChecker is a generic busy detector for a physical transmission medium.
type BusyChannelChecker interface {
	// Returns true if the channel is not clear
	Busy() bool
}

type PTTController interface {
	SetPTT(on bool) error
}

// Dialer is implemented by transports that supports dialing a transport.URL.
type Dialer interface {
	DialURL(url *URL) (net.Conn, error)
}

// ContextDialer is implemented by transports that supports dialing with cancellation.
type ContextDialer interface {
	// DiaulURLContext dials a connection.
	//
	// If the given context is cancelled before the connection is made, the operation is aborted and an error returned.
	// Once successfully connected, any expiration of the context will not affect the connection.
	DialURLContext(ctx context.Context, url *URL) (net.Conn, error)
}
