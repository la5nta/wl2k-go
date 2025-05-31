// Copyright 2015 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package ardop

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/la5nta/wl2k-go/transport"
)

// The default number of connect requests when dialing.
const DefaultConnectRequests = 10

// DialURL dials ardop:// URLs.
//
// Accepted query parameters:
//   - bw: The ARQ bandwidth for this connection.
//   - connect_requests: The number of connect frames to send before giving up (default: 10).
func (tnc *TNC) DialURL(url *transport.URL) (net.Conn, error) {
	if url.Scheme != "ardop" {
		return nil, transport.ErrUnsupportedScheme
	}

	var bw Bandwidth
	if str := url.Params.Get("bw"); str != "" {
		var err error
		bw, err = BandwidthFromString(str)
		if err != nil {
			return nil, err
		}
	}

	var connectRequests int
	if str := url.Params.Get("connect_requests"); str != "" {
		var err error
		connectRequests, err = strconv.Atoi(url.Params.Get("connect_requests"))
		if err != nil {
			return nil, fmt.Errorf("invalid connect_requests value: %w", err)
		}
	}

	return tnc.DialBandwidth(url.Target, bw, connectRequests)
}

// DialURLContext dials ardop:// URLs with cancellation support. See DialURL.
//
// If the context is cancelled while dialing, the connection may be closed gracefully before returning an error.
// Use Abort() for immediate cancellation of a dial operation.
func (tnc *TNC) DialURLContext(ctx context.Context, url *transport.URL) (net.Conn, error) {
	var (
		conn net.Conn
		err  error
		done = make(chan struct{})
	)
	go func() {
		conn, err = tnc.DialURL(url)
		close(done)
	}()
	select {
	case <-done:
		return conn, err
	case <-ctx.Done():
		tnc.Disconnect()
		return nil, ctx.Err()
	}
}

// Dial dials a ARQ connection with default bandwidth and connect requests.
func (tnc *TNC) Dial(targetcall string) (net.Conn, error) {
	return tnc.DialBandwidth(targetcall, Bandwidth{}, DefaultConnectRequests)
}

// DialBandwidth dials a ARQ connection after setting the given ARQ bandwidth temporarily.
//
// The ARQ bandwidth setting is reverted on any Dial error and when calling conn.Close().
func (tnc *TNC) DialBandwidth(targetcall string, bw Bandwidth, connectRequests int) (net.Conn, error) {
	if tnc.closed {
		return nil, ErrTNCClosed
	}

	if connectRequests == 0 {
		connectRequests = DefaultConnectRequests
	}

	var defers []func() error
	if !bw.IsZero() {
		currentBw, err := tnc.ARQBandwidth()
		if err != nil {
			return nil, err
		}
		if err := tnc.SetARQBandwidth(bw); err != nil {
			return nil, err
		}
		defers = append(defers, func() error { return tnc.SetARQBandwidth(currentBw) })
	}

	// Handle busy channel with BusyFunc if provided.
	if tnc.busyFunc != nil {
		if abort := tnc.waitIfBusy(tnc.busyFunc); abort {
			return nil, fmt.Errorf("aborted while waiting for clear channel")
		}
	}

	if err := tnc.arqCall(targetcall, connectRequests); err != nil {
		for _, fn := range defers {
			_ = fn()
		}
		return nil, err
	}

	mycall, err := tnc.MyCall()
	if err != nil {
		for _, fn := range defers {
			_ = fn()
		}
		return nil, fmt.Errorf("Error when getting mycall: %s", err)
	}

	tnc.data = &tncConn{
		remoteAddr: Addr{targetcall},
		localAddr:  Addr{mycall},
		ctrlOut:    tnc.out,
		dataOut:    tnc.dataOut,
		ctrlIn:     tnc.in,
		dataIn:     tnc.dataIn,
		eofChan:    make(chan struct{}),
		isTCP:      tnc.isTCP,
		onClose:    defers,
	}

	return tnc.data, nil
}

// waitIfBusy waits for signal from the BusyFunc if the channel is busy.
func (tnc *TNC) waitIfBusy(busyFunc BusyFunc) (abort bool) {
	if !tnc.Busy() {
		return false
	}

	// Start a goroutine to cancel the context if/when the channel clears
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer cancel()
		for tnc.Busy() && ctx.Err() == nil {
			time.Sleep(300 * time.Millisecond)
		}
	}()

	// Block until busyFunc returns
	return busyFunc(ctx)
}
