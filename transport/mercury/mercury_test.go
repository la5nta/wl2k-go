// Copyright 2021 Jeremy Bush (N8JJA). All rights reserved.
// Copyright 2026 Rhizomatica. All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package mercury

import (
	"net"
	"testing"

	"github.com/la5nta/wl2k-go/transport"
)

func TestInterfaces(t *testing.T) {
	// Ensure Modem implements the necessary transport interfaces
	// (https://github.com/la5nta/pat/wiki/Adding-transports).
	var _ transport.Dialer = (*Modem)(nil)
	var _ transport.ContextDialer = (*Modem)(nil)
	var _ net.Conn = (*conn)(nil)

	// Optional interfaces with extended functionality.
	var _ net.Listener = (*listener)(nil)
	var _ transport.BusyChannelChecker = (*Modem)(nil)
	var _ transport.Flusher = (*conn)(nil)
	var _ transport.TxBuffer = (*conn)(nil)
}

func TestBandwidths(t *testing.T) {
	bw := Bandwidths()
	if !contains(bw, "500") || !contains(bw, "2300") || !contains(bw, "2750") {
		t.Fatalf("unexpected bandwidths: %v", bw)
	}
}

func TestConfigDefaults(t *testing.T) {
	c := ModemConfig{}.withDefaults()
	if c.Host != "localhost" || c.CmdPort != 8300 || c.DataPort != 8301 {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if c := (ModemConfig{CmdPort: 9000}).withDefaults(); c.DataPort != 9001 {
		t.Fatalf("DataPort should follow CmdPort: %+v", c)
	}
}
