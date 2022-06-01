// Copyright 2015 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

// Package ax25 provides a net.Conn and net.Listener interfaces for AX.25.
//
// Supported TNCs
//
// This package currently implements interfaces for Linux' AX.25 stack and Tasco-like TNCs (Kenwood transceivers).
//
// Build tags
//
// The Linux AX.25 stack bindings are guarded by some custom build tags:
//
//    libax25 // Include support for Linux' AX.25 stack by linking against libax25.
//    static  // Link against static libraries only.
//
package ax25

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/la5nta/wl2k-go/transport"
)

const (
	// DefaultSerialBaud is the default serial_baud value of the serial-tnc scheme.
	DefaultSerialBaud = 9600
)

const _NETWORK = "AX.25"

var DefaultDialer = &Dialer{Timeout: 45 * time.Second}

func init() {
	transport.RegisterDialer("ax25", DefaultDialer)
	transport.RegisterDialer("serial-tnc", DefaultDialer)
}

type addr interface {
	Address() Address // Callsign
	Digis() []Address // Digipeaters
}

type AX25Addr struct{ addr }

func (a AX25Addr) Network() string { return _NETWORK }
func (a AX25Addr) String() string {
	var buf bytes.Buffer

	fmt.Fprint(&buf, a.Address())
	if len(a.Digis()) > 0 {
		fmt.Fprint(&buf, " via")
	}
	for _, digi := range a.Digis() {
		fmt.Fprintf(&buf, " %s", digi)
	}

	return buf.String()
}

type Address struct {
	Call string
	SSID uint8
}

type Conn struct {
	io.ReadWriteCloser
	localAddr  AX25Addr
	remoteAddr AX25Addr
}

func (c *Conn) LocalAddr() net.Addr {
	if !c.ok() {
		return nil
	}
	return c.localAddr
}

func (c *Conn) RemoteAddr() net.Addr {
	if !c.ok() {
		return nil
	}
	return c.remoteAddr
}

func (c *Conn) ok() bool { return c != nil }

func (c *Conn) SetDeadline(t time.Time) error {
	return errors.New(`SetDeadline not implemented`)
}

func (c *Conn) SetReadDeadline(t time.Time) error {
	return errors.New(`SetReadDeadline not implemented`)
}

func (c *Conn) SetWriteDeadline(t time.Time) error {
	return errors.New(`SetWriteDeadline not implemented`)
}

type Beacon interface {
	Now() error
	Every(d time.Duration) error

	LocalAddr() net.Addr
	RemoteAddr() net.Addr

	Message() string
}

type Dialer struct {
	Timeout time.Duration
}

// DialURL dials ax25:// and serial-tnc:// URLs.
//
// See DialURLContext.
func (d Dialer) DialURL(url *transport.URL) (net.Conn, error) {
	return d.DialURLContext(context.Background(), url)
}

// DialURLContext dials ax25:// and serial-tnc:// URLs.
//
// If the context is cancelled while dialing, the connection may be closed gracefully before returning an error.
func (d Dialer) DialURLContext(ctx context.Context, url *transport.URL) (net.Conn, error) {
	target := url.Target
	if len(url.Digis) > 0 {
		target = fmt.Sprintf("%s via %s", target, strings.Join(url.Digis, " "))
	}

	switch url.Scheme {
	case "ax25":
		ctx, cancel := context.WithTimeout(ctx, d.Timeout)
		defer cancel()
		conn, err := DialAX25Context(ctx, url.Host, url.User.Username(), target)
		if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			// Local timeout reached.
			err = fmt.Errorf("Dial timeout")
		}
		return conn, err
	case "serial-tnc":
		// TODO: This is some badly designed legacy stuff. Need to re-think the whole
		// serial-tnc scheme. See issue #34.
		hbaud := HBaud(1200)
		if i, _ := strconv.Atoi(url.Params.Get("hbaud")); i > 0 {
			hbaud = HBaud(i)
		}
		serialBaud := DefaultSerialBaud
		if i, _ := strconv.Atoi(url.Params.Get("serial_baud")); i > 0 {
			serialBaud = i
		}

		return DialKenwood(
			url.Host,
			url.User.Username(),
			target,
			NewConfig(hbaud, serialBaud),
			nil,
		)
	default:
		return nil, transport.ErrUnsupportedScheme
	}
}

func AddressFromString(str string) Address {
	parts := strings.Split(str, "-")
	addr := Address{Call: parts[0]}
	if len(parts) > 1 {
		ssid, err := strconv.ParseInt(parts[1], 10, 32)
		if err == nil && ssid >= 0 && ssid <= 255 {
			addr.SSID = uint8(ssid)
		}
	}
	return addr
}

func (a Address) String() string {
	if a.SSID > 0 {
		return fmt.Sprintf("%s-%d", a.Call, a.SSID)
	} else {
		return a.Call
	}
}
