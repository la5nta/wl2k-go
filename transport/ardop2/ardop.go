// Copyright 2015 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

// Package ardop provides means of establishing a connection to a remote node using ARDOP TNC
package ardop2

import (
	"errors"
	"os"
	"strings"
	"time"
)

const (
	DefaultAddr       = "localhost:8515" // The default address Ardop TNC listens on
	DefaultARQTimeout = 90 * time.Second // The default ARQ session idle timout
)

const (
	ModeARQ = "ARQ" // ARQ mode
	ModeFEC = "FEC" // FEC mode
)

// TNC states
const (
	//go:generate stringer -type=State .
	Unknown      State = iota
	Offline            // Sound card disabled and all sound card resources are released
	Disconnected       // The session is disconnected, the sound card remains active
	ISS                // Information Sending Station (Sending Data)
	IRS                // Information Receiving Station (Receiving data)
	Quiet              // ??
	FECSend            // ??
	FECReceive         // Receiving FEC (unproto) data
)

var (
	ErrBusy                 = errors.New("TNC control port is busy.")
	ErrConnectInProgress    = errors.New("A connect is in progress.")
	ErrFlushTimeout         = errors.New("Flush timeout.")
	ErrActiveListenerExists = errors.New("An active listener is already registered with this TNC.")
	ErrDisconnectTimeout    = errors.New("Disconnect timeout: aborted connection.")
	ErrConnectTimeout       = errors.New("Connect timeout")
	ErrRejectedBandwidth    = errors.New("Connection rejected by peer: incompatible bandwidth")
	ErrRejectedBusy         = errors.New("Connection rejected: channel busy")
	ErrChecksumMismatch     = errors.New("Control protocol checksum mismatch")
	ErrTNCClosed            = errors.New("TNC closed")
)

type State uint8

var stateMap = map[string]State{
	"":        Unknown,
	"OFFLINE": Offline,
	"DISC":    Disconnected,
	"ISS":     ISS,
	"IRS":     IRS,
	"QUIET":   Quiet,
	"FECRcv":  FECReceive,
	"FECSend": FECSend,
}

func strToState(str string) (State, bool) {
	state, ok := stateMap[strings.ToUpper(str)]
	return state, ok
}

func debugEnabled() bool {
	return os.Getenv("ardop_debug") != ""
}
