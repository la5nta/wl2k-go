// Copyright 2021 Jeremy Bush (N8JJA). All rights reserved.
// Copyright 2026 Rhizomatica. All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package mercury

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/la5nta/wl2k-go/transport"
)

// Implementations for various wl2k-go/transport interfaces.

// DialURL dials a mercury:// URL. See DialURLContext.
func (m *Modem) DialURL(url *transport.URL) (net.Conn, error) {
	return m.DialURLContext(context.Background(), url)
}

// DialURLContext dials a mercury:// URL with cancellation support.
//
// Accepted query parameters:
//   - bw: The ARQ bandwidth for this connection ("500", "2300" or "2750").
//
// If the context is cancelled while dialing, the connection may be closed
// gracefully before returning an error. Use Abort() for immediate cancellation.
func (m *Modem) DialURLContext(ctx context.Context, url *transport.URL) (net.Conn, error) {
	if url.Scheme != SchemeMercury {
		return nil, transport.ErrUnsupportedScheme
	}
	if m.closed {
		return nil, ErrModemClosed
	}
	if len(url.Digis) > 0 {
		return nil, transport.ErrDigisUnsupported
	}

	// TODO: Handle race condition here. Should prevent concurrent dialing.
	if m.connectedState != disconnected {
		return nil, errors.New("modem busy")
	}

	// Set temporary bandwidth from the URL. Reset on disconnect by handleDisconnected.
	if err := m.setBandwidth(url.Params.Get("bw")); err != nil {
		return nil, err
	}

	// Handle busy channel with BusyFunc if provided.
	if m.busyFunc != nil {
		if abort := m.waitIfBusy(ctx, m.busyFunc); abort {
			return nil, fmt.Errorf("aborted while waiting for clear channel")
		}
	}

	// Start connecting
	m.connectedState = connecting
	cmds, cancel := m.cmds.Subscribe("CONNECTED", "DISCONNECTED")
	defer cancel()
	if err := m.writeCmd(fmt.Sprintf("CONNECT %s %s", m.myCall, url.Target)); err != nil {
		return nil, err
	}

	// Handle context cancellation. The modem does not always accept DISCONNECT
	// while dialing, so we might end up returning a connection even after
	// DISCONNECT is sent.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			debugPrint("context cancellation - sending disconnect command...")
			m.writeCmd("DISCONNECT")
		case <-done:
		}
	}()

	switch cmd, ok := <-cmds; {
	case !ok:
		return nil, ErrModemClosed
	case strings.HasPrefix(cmd, "CONNECTED"):
		return m.newConn(url.Target), nil
	case ctx.Err() != nil:
		return nil, ctx.Err()
	default:
		// DISCONNECTED for some other reason. Most likely a timeout.
		return nil, errors.New("connect timeout")
	}
}

// Disconnect gracefully closes any active connection, blocking until the link
// is disconnected. If the modem is not connected, this is a no-op.
func (m *Modem) Disconnect() error {
	ack, cancel := m.cmds.Subscribe("DISCONNECTED")
	defer cancel()
	if m.connectedState == disconnected {
		return nil
	}
	if err := m.writeCmd("DISCONNECT"); err != nil {
		return err
	}
	<-ack
	return nil
}

// Abort disconnects the link immediately ("dirty disconnect").
func (m *Modem) Abort() error {
	err := m.writeCmd("ABORT")
	// The modem does not send a DISCONNECTED state change after ABORT if it's
	// already in the process of disconnecting, so we have to fake it.
	m.cmds.Publish("DISCONNECTED")
	m.handleDisconnected()
	return err
}

func (m *Modem) setBandwidth(bw string) error {
	if bw == "" {
		return nil
	}
	if !contains(bandwidths, bw) {
		return fmt.Errorf("bandwidth %s not supported", bw)
	}
	return m.writeCmd("BW" + bw)
}

func contains(c []string, s string) bool {
	for _, e := range c {
		if e == s {
			return true
		}
	}
	return false
}

// Busy returns true if the channel is not clear.
func (m *Modem) Busy() bool { return m.busy }

// SetPTT injects the PTTController (typically hooked to a transceiver) that
// should be keyed by the modem. If nil, PTT requests from the TNC are ignored
// (VOX may still work).
func (m *Modem) SetPTT(ptt transport.PTTController) { m.rig = ptt }

// waitIfBusy waits for signal from the BusyFunc if the channel is busy.
func (m *Modem) waitIfBusy(ctx context.Context, busyFunc BusyFunc) (abort bool) {
	if !m.Busy() {
		return false
	}

	// Cancel the context if/when the channel clears.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		defer cancel()
		for m.Busy() && ctx.Err() == nil {
			time.Sleep(300 * time.Millisecond)
		}
	}()

	return busyFunc(ctx)
}
