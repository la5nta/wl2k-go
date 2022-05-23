// Copyright 2015 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package ardop

import (
	"context"
	"fmt"
	"net"

	"github.com/la5nta/wl2k-go/transport"
)

// DialURL dials ardop:// URLs.
//
// Parameter bw can be used to set the ARQ bandwidth for this connection. See DialBandwidth for details.
func (tnc *TNC) DialURL(url *transport.URL) (net.Conn, error) {
	if url.Scheme != "ardop" {
		return nil, transport.ErrUnsupportedScheme
	}
	bwStr := url.Params.Get("bw")
	if bwStr == "" {
		return tnc.Dial(url.Target)
	}
	bw, err := BandwidthFromString(bwStr)
	if err != nil {
		return nil, err
	}
	return tnc.DialBandwidth(url.Target, bw)
}

// DialURLContext dials ardop:// URLs with cancellation support. See DialURL.
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

// Dial dials a ARQ connection.
func (tnc *TNC) Dial(targetcall string) (net.Conn, error) {
	return tnc.DialBandwidth(targetcall, Bandwidth{})
}

// DialBandwidth dials a ARQ connection after setting the given ARQ bandwidth temporarily.
//
// The ARQ bandwidth setting is reverted on any Dial error and when calling conn.Close().
func (tnc *TNC) DialBandwidth(targetcall string, bw Bandwidth) (net.Conn, error) {
	if tnc.closed {
		return nil, ErrTNCClosed
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

	if err := tnc.arqCall(targetcall, 10); err != nil {
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
