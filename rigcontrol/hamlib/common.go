// Copyright 2015 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

// Package hamlib provides bindings for a _subset_ of hamlib.
// It provides both native cgo bindings and a rigctld client.
//
// Use build tag "libhamlib" to build with native C library support.
package hamlib

import "fmt"

// RigModel is the hamlib ID identifying a spesific tranceiver model.
type RigModel int

// Rig represents a receiver or tranceiver.
//
// It holds the data connection to the device.
type Rig interface {
	// Closes the connection to the Rig.
	Close() error

	// Returns the Rig's active VFO (for control).
	CurrentVFO() VFO

	// Returns the Rig's A-VFO (for control).
	VFOA() (VFO, error)

	// Returns the Rig's B-VFO (for control).
	VFOB() (VFO, error)
}

// VFO (Variable Frequency Oscillator) represents a tunable channel, from the radio operator's view.
//
// Also referred to as "BAND" (A-band/B-band) by some radio manufacturers.
type VFO interface {
	// Gets the dial frequency for this VFO.
	GetFreq() (int, error)

	// Sets the dial frequency for this VFO.
	SetFreq(f int) error

	// GetPTT returns the PTT state for this VFO.
	GetPTT() (bool, error)

	// Enable (or disable) PTT on this VFO.
	SetPTT(on bool) error

	// Set VFO modulation mode to 'm' using passband width 'pbw' in Hz, or rig default if 'pbw' is zero.
	SetMode(m Mode, pbw int) error

	// Get the modulation mode and passband width in Hz that this VFO is set to.
	GetMode() (m Mode, pwb int, err error)
}

// ModeToString converts a enum mode as returned from hamlib into a string.
func ModeToString(m Mode) string {
	return ModeString[m] // Yes, ignoring unlikely possible error.
}

// StringToMode converts a string representation of a rig mode into a hamlib enum mode
// returning a non-nil error if it can't do that.
func StringToMode(s string) (m Mode, err error) {
	for mkey := range ModeString {
		val := ModeString[mkey]
		if val == s {
			return mkey, nil
		}
	}
	return 0, fmt.Errorf("Invalid rig mode '%s'", s)
}

func Open(network, address string) (Rig, error) {
	switch network {
	case "tcp":
		return OpenTCP(address)
	case "serial":
		return OpenSerialURI(address)
	default:
		return nil, fmt.Errorf("Unknown network")
	}
}
