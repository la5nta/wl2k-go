// Copyright 2016 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package transport

import "net"

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
	SetPTT(on bool)
}

// Dialer is implemented by transports that supports dialing a transport.URL.
type Dialer interface {
	DialURL(url *URL) (net.Conn, error)
}
