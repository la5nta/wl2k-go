// Copyright 2015 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

//go:build !libax25
// +build !libax25

package ax25

import (
	"context"
	"errors"
	"net"
	"time"
)

var ErrNoLibax25 = errors.New("AX.25 support not included in this build")

func ListenAX25(axPort, mycall string) (net.Listener, error) {
	return nil, ErrNoLibax25
}

func DialAX25Timeout(axPort, mycall, targetcall string, timeout time.Duration) (*Conn, error) {
	return nil, ErrNoLibax25
}

func DialAX25(axPort, mycall, targetcall string) (*Conn, error) {
	return nil, ErrNoLibax25
}

func DialAX25Context(ctx context.Context, axPort, mycall, targetcall string) (*Conn, error) {
	return nil, ErrNoLibax25
}
