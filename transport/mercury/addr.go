// Copyright 2021 Jeremy Bush (N8JJA). All rights reserved.
// Copyright 2026 Rhizomatica. All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package mercury

const network = "mercury"

// Addr implements net.Addr for a station callsign.
type Addr struct{ string }

func (a Addr) Network() string { return network }
func (a Addr) String() string  { return a.string }
