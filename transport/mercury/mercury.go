// Copyright 2021 Jeremy Bush (N8JJA). All rights reserved.
// Copyright 2026 Rhizomatica. All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

// Package mercury provides means of establishing a connection to a remote node
// using the Mercury HF modem (https://github.com/Rhizomatica/mercury).
//
// Mercury is an independent OFDM modem that deliberately exposes a
// VARA-compatible TCP TNC interface: a CR-terminated ASCII command/status port
// and, on the following port number, a raw binary data port. This driver speaks
// that interface.
//
// It is adapted from github.com/n8jja/Pat-Vara (the VARA transport for Pat),
// used under the MIT license, with the VARA-only behaviour removed. Mercury and
// VARA are separate implementations and are expected to diverge over time.
package mercury

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/la5nta/wl2k-go/transport"
)

// SchemeMercury is the transport.URL scheme served by this package.
const SchemeMercury = "mercury"

var (
	ErrModemClosed    = errors.New("modem closed")
	errNotImplemented = errors.New("not implemented")
)

// ModemConfig defines configuration options for connecting to the modem program.
type ModemConfig struct {
	// Host on the network which is hosting the modem; defaults to "localhost".
	Host string
	// CmdPort is the TCP command port on which to reach the modem; defaults to 8300.
	CmdPort int
	// DataPort is the TCP port on which to exchange over-the-air payloads with
	// the modem; defaults to CmdPort+1 (8301).
	DataPort int
}

const (
	defaultHost    = "localhost"
	defaultCmdPort = 8300
)

// withDefaults returns a copy of c with any zero-value fields back-filled.
func (c ModemConfig) withDefaults() ModemConfig {
	if c.Host == "" {
		c.Host = defaultHost
	}
	if c.CmdPort == 0 {
		c.CmdPort = defaultCmdPort
	}
	if c.DataPort == 0 {
		c.DataPort = c.CmdPort + 1
	}
	return c
}

type Modem struct {
	myCall         string
	config         ModemConfig
	bandwidth      string
	cmdConn        *net.TCPConn
	dataConn       *net.TCPConn
	busy           bool
	busyFunc       BusyFunc
	cmds           pubSub
	inboundConns   chan *conn
	connectedState connectedState
	rig            transport.PTTController

	bufferCount *bufferCount
	closeOnce   sync.Once
	closed      bool
}

type connectedState int

const (
	connected connectedState = iota
	disconnected
	connecting
)

// bandwidths are the ARQ bandwidth tokens accepted by Mercury.
//
// Note: Mercury currently treats "2750" as a VARA-compatibility alias of
// "2300" (same payload-mode ceiling); it is preserved only as a
// negotiated/reporting token. See Mercury's docs/TNC.md.
var bandwidths = []string{"500", "2300", "2750"}

// Bandwidths returns the list of supported ARQ bandwidth tokens.
func Bandwidths() []string { return bandwidths }

// NewModem initializes and connects a new Mercury modem client.
func NewModem(myCall string, config ModemConfig) (*Modem, error) {
	m := &Modem{
		myCall:         myCall,
		config:         config.withDefaults(),
		cmds:           newPubSub(),
		inboundConns:   make(chan *conn),
		connectedState: disconnected,
		bufferCount:    newBufferCount(),
	}
	if err := m.start(); err != nil {
		return nil, err
	}
	return m, nil
}

// BusyFunc is a function that is called when the dialed channel is busy.
//
// If the channel is busy, the dialer blocks on this function call until it returns.
// The provided context is cancelled if/when the channel clears.
// The return value determines if the dialer should abort or continue dialing.
type BusyFunc func(context.Context) (abort bool)

// SetBusyFunc sets the function that will be called if the channel is busy when dialing.
func (m *Modem) SetBusyFunc(fn BusyFunc) { m.busyFunc = fn }

// start establishes the TCP connections with the modem and configures it.
func (m *Modem) start() error {
	var err error
	if m.cmdConn, err = m.connectTCP("command", m.config.CmdPort); err != nil {
		return err
	}
	if m.dataConn, err = m.connectTCP("data", m.config.DataPort); err != nil {
		m.cmdConn.Close()
		return err
	}

	// Accept calls addressed to any callsign (Winlink RMS behaviour). Callers
	// that want strict addressing can send "PUBLIC OFF" afterwards.
	if err := m.writeCmd("PUBLIC ON"); err != nil {
		return err
	}
	// Mercury does not compress (this is a no-op kept for VARA client
	// compatibility); the B2F session layer already handles compression.
	if err := m.writeCmd("COMPRESSION OFF"); err != nil {
		return err
	}
	if err := m.writeCmd(fmt.Sprintf("MYCALL %s", m.myCall)); err != nil {
		return err
	}
	if err := m.writeCmd("LISTEN OFF"); err != nil {
		return err
	}

	go m.cmdListen()
	return nil
}

// SetBandwidth sets the default ARQ bandwidth for outbound and inbound connections.
func (m *Modem) SetBandwidth(bandwidth string) error {
	if err := m.setBandwidth(bandwidth); err != nil {
		return err
	}
	// Save this so we can revert on disconnect in case it's changed via a
	// connect URL parameter.
	m.bandwidth = bandwidth
	return nil
}

// Idle returns true if the modem is not in a connecting or connected state.
func (m *Modem) Idle() bool { return m.connectedState == disconnected }

// Ping returns true as long as the modem connection is open.
func (m *Modem) Ping() bool { return !m.closed }

// Version queries the modem identification string.
func (m *Modem) Version() (string, error) {
	resp, cancel := m.cmds.Subscribe("VERSION", "WRONG")
	defer cancel()
	if err := m.writeCmd("VERSION"); err != nil {
		return "", err
	}
	str := <-resp
	if str == "WRONG" {
		return "", errNotImplemented
	}
	return strings.TrimPrefix(str, "VERSION "), nil
}

// Close closes the RF link and then the TCP connections to the modem.
// It blocks until finished.
func (m *Modem) Close() error {
	m.closeOnce.Do(func() {
		m.closed = true
		defer func() {
			m.cmds.Close()
			close(m.inboundConns)
			m.dataConn.Close()
			m.cmdConn.Close()
		}()

		connectChange, cancel := m.cmds.Subscribe("DISCONNECTED", "CONNECTED")
		defer cancel()
		if m.connectedState != disconnected {
			if err := m.writeCmd("DISCONNECT"); err != nil {
				// We've already lost the modem; just fake the state change.
				m.cmds.Publish("DISCONNECTED")
				m.handleDisconnected()
				return
			}
			select {
			case res := <-connectChange:
				if res != "DISCONNECTED" {
					log.Println("mercury: disconnect failed, aborting!")
					m.Abort()
				}
			case <-time.After(60 * time.Second):
				m.Abort()
			}
		}

		// Make sure TX is stopped (should have already happened).
		m.sendPTT(false)
	})
	return nil
}

func (m *Modem) connectTCP(name string, port int) (*net.TCPConn, error) {
	debugPrint("connecting %s port", name)
	addr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%d", m.config.Host, port))
	if err != nil {
		return nil, fmt.Errorf("couldn't resolve modem %s address: %w", name, err)
	}
	conn, err := net.DialTCP("tcp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("couldn't connect to modem %s port: %w", name, err)
	}
	return conn, nil
}

// writeCmd sends a single CR-terminated command on the command port.
func (m *Modem) writeCmd(cmd string) error {
	debugPrint3("writing cmd: %v", cmd)
	if m.closed {
		return ErrModemClosed
	}
	m.cmdConn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := m.cmdConn.Write([]byte(cmd + "\r"))
	if err != nil {
		m.closed = true
		debugPrint3("writeCmd err: %v", err)
	}
	return err
}

// cmdListen reads asynchronous status lines from the command port.
func (m *Modem) cmdListen() {
	defer m.Close()
	buf := make([]byte, 1<<16)
	for !m.closed {
		// The modem sends IAMALIVE periodically while idle, so if we've heard
		// nothing for 2 minutes assume the connection is dead.
		m.cmdConn.SetReadDeadline(time.Now().Add(2 * time.Minute))
		l, err := m.cmdConn.Read(buf)
		if err != nil {
			if m.connectedState != disconnected {
				log.Println("mercury: modem disconnected unexpectedly!")
			}
			debugPrint("cmdListen err: %v", err)
			m.cmdConn.Close()
			return
		}
		for _, c := range strings.Split(string(buf[:l]), "\r") {
			if c == "" {
				continue
			}
			m.handleCmd(c)
			m.cmds.Publish(c)
		}
	}
}

// handleCmd acts on a single status line coming from the modem.
func (m *Modem) handleCmd(c string) {
	debugPrint("got cmd: %v", c)
	switch c {
	case "PTT ON":
		m.sendPTT(true)
	case "PTT OFF":
		m.sendPTT(false)
	case "BUSY ON":
		m.busy = true
	case "BUSY OFF":
		m.busy = false
	case "OK", "WRONG":
		// nothing to do
	case "IAMALIVE", "PENDING", "CANCELPENDING":
		// nothing to do
	case "LINK REGISTERED", "LINK UNREGISTERED":
		// nothing to do
	case "ENCRYPTION DISABLED", "ENCRYPTION READY", "ENCRYPTED LINK", "UNENCRYPTED LINK":
		// nothing to do
	case "DISCONNECTED":
		m.handleDisconnected()
	default:
		switch {
		case strings.HasPrefix(c, "BUFFER "):
			m.bufferCount.set(parseBuffer(c))
		case strings.HasPrefix(c, "CONNECTED "):
			m.handleConnected(c)
		case strings.HasPrefix(c, "REGISTERED"):
			if parts := strings.Fields(c); len(parts) > 1 {
				log.Printf("mercury: registered callsign %s", parts[1])
			}
		case strings.HasPrefix(c, "CQFRAME "):
			// Incoming CQ frame decoded; nothing to do.
		case strings.HasPrefix(c, "SN "), strings.HasPrefix(c, "BITRATE "):
			// Link quality / throughput updates; nothing to do.
		case strings.HasPrefix(c, "VERSION"):
			// Handled by Version() through pubsub.
		default:
			debugPrint("unexpected modem command: %q", c)
		}
	}
}

func (m *Modem) sendPTT(on bool) {
	if m.rig != nil {
		_ = m.rig.SetPTT(on)
	}
}

func (m *Modem) handleDisconnected() {
	m.connectedState = disconnected
	m.bufferCount.reset()       // discard any outstanding frame accounting
	m.setBandwidth(m.bandwidth) // reset bandwidth to the configured default
}

func (m *Modem) handleConnected(cmd string) {
	m.connectedState = connected
	parts := strings.Fields(cmd)
	if len(parts) < 3 {
		panic(fmt.Sprintf("unexpected CONNECTED command: %q", cmd))
	}
	switch src, dst := parts[1], parts[2]; {
	case src == m.myCall:
		// Outbound connection, handled by DialURLContext through pubsub.
	case dst == m.myCall:
		m.offerInbound(src)
	default:
		// PUBLIC ON: neither endpoint matches our callsign. Treat as inbound.
		m.offerInbound(src)
	}
}

func (m *Modem) offerInbound(src string) {
	select {
	case m.inboundConns <- m.newConn(src):
	default:
		debugPrint("no Accept() waiting; dropping connection from %s", src)
		m.writeCmd("DISCONNECT")
	}
}
