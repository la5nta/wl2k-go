// Copyright 2015 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package hamlib

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"time"
)

const DefaultTCPAddr = "localhost:4532"

var ErrNotVFOMode = errors.New("rigctl is not running in VFO mode")

var ErrUnexpectedValue = fmt.Errorf("Unexpected value in response")

// TCPTimeout defines the timeout duration of dial, read and write operations.
var TCPTimeout = time.Second

// Rig represents a receiver or tranceiver.
//
// It holds the tcp connection to the service (rigctld).
type TCPRig struct {
	mu      sync.Mutex
	conn    *textproto.Conn
	tcpConn net.Conn
	addr    string
}

// VFO (Variable Frequency Oscillator) represents a tunable channel,
// from the radio operator's view.
//
// Also referred to as "BAND" (A-band/B-band) by some radio manufacturers.
type tcpVFO struct {
	r      *TCPRig
	prefix string
}

// OpenTCP opens a new TCPRig and returns a ready to use Rig.
//
// The connection to rigctld is not initiated until the connection is requred.
// To check for a valid connection, call Ping.
//
// Caller must remember to Close the Rig after use.
func OpenTCP(addr string) (*TCPRig, error) {
	r := &TCPRig{addr: addr}
	return r, nil
}

// Ping checks that a connection to rigctld is open and valid.
//
// If no connection is active, it will try to establish one.
func (r *TCPRig) Ping() error { _, err := r.cmd(`\get_info`, 1); return err } // Should return something for any rig.
//func (r *TCPRig) Ping() error { _, err := r.cmd(`dump_caps`, 1); return err } //DJC TODO: no prefix on dump_caps == doesn't work!

func (r *TCPRig) dial() (err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.conn != nil {
		r.conn.Close()
	}

	// Dial with 3 second timeout
	r.tcpConn, err = net.DialTimeout("tcp", r.addr, TCPTimeout)
	if err != nil {
		return err
	}

	r.conn = textproto.NewConn(r.tcpConn)

	return err
}

// Closes the connection to the Rig.
func (r *TCPRig) Close() error {
	if r.conn == nil {
		return nil
	}
	return r.conn.Close()
}

// Returns the Rig's active VFO (for control).
func (r *TCPRig) CurrentVFO() VFO { return &tcpVFO{r, ""} }

// Returns the Rig's VFO A (for control).
//
// ErrNotVFOMode is returned if rigctld is not in VFO mode.
func (r *TCPRig) VFOA() (VFO, error) {
	if ok, err := r.VFOMode(); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrNotVFOMode
	}

	return &tcpVFO{r, "VFOA"}, nil
}

// Returns the Rig's VFO B (for control).
//
// ErrNotVFOMode is returned if rigctld is not in VFO mode.
func (r *TCPRig) VFOB() (VFO, error) {
	if ok, err := r.VFOMode(); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrNotVFOMode
	}

	return &tcpVFO{r, "VFOB"}, nil
}

func (r *TCPRig) VFOMode() (bool, error) {
	resp, err := r.cmd(`\chk_vfo`, 1)
	if err != nil {
		return false, err
	}
	return resp[0] == "CHKVFO 1", nil
}

// Gets the dial frequency in Hz for this VFO.
func (v *tcpVFO) GetFreq() (int, error) {
	resp, err := v.cmd(`\get_freq`, 1)
	if err != nil {
		return -1, err
	}

	freq, err := strconv.Atoi(resp[0])
	if err != nil {
		return -1, err
	}

	return freq, nil
}

// Sets the dial frequency for this VFO.
func (v *tcpVFO) SetFreq(freq int) error {
	_, err := v.cmd(`\set_freq %d`, 0, freq)
	return err
}

func (v *tcpVFO) GetModeAsString() (rigmode, bandwidth string, err error) {
	// Query the VFO and return the modulation mode and bandwidth.
	// 'rigmode' is the mode as a string (defined by hamlib).
	// 'bandwidth' is the passband width in Hz.
	var modeBW []string
	modeBW, err = v.cmd(`\get_mode`, 2)
	if err != nil {
		return "", "", err
	}
	return modeBW[0], modeBW[1], nil
}

func (v *tcpVFO) SetModeAsString(rigmode, bandwidth string) (err error) {
	_, err = v.cmd(`\set_mode %s %s`, 0, rigmode, bandwidth)
	return err
}

// GetPTT returns the PTT state for this VFO.
func (v *tcpVFO) GetPTT() (bool, error) {
	resp, err := v.cmd("t", 1)
	if err != nil {
		return false, err
	}

	switch resp[0] {
	case "0":
		return false, nil
	case "1", "2", "3":
		return true, nil
	default:
		return false, ErrUnexpectedValue
	}
}

// Enable (or disable) PTT on this VFO.
func (v *tcpVFO) SetPTT(on bool) error {
	bInt := 0
	if on == true {
		bInt = 1

		// Experimental PTT STATE 3 (https://github.com/la5nta/pat/issues/184)
		if experimentalPTT3Enabled() {
			bInt = 3
		}
	}

	_, err := v.cmd(`\set_ptt %d`, 0, bInt)
	return err
}

func (v *tcpVFO) cmd(format string, nresults int, args ...interface{}) ([]string, error) {
	// Add VFO argument (if set)
	if v.prefix != "" {
		parts := strings.Split(format, " ")
		parts = append([]string{parts[0], v.prefix}, parts[1:]...)
		format = strings.Join(parts, " ")
	}
	return v.r.cmd(format, nresults, args...)
}

func (r *TCPRig) cmd(format string, nresults int, args ...interface{}) (resp []string, err error) {
	// Retry
	for i := 0; i < 3; i++ {
		if r.conn == nil {
			// Try re-dialing
			if err = r.dial(); err != nil {
				break
			}
		}

		resp, err = r.doCmd(format, nresults, args...)
		if err == nil {
			break
		}

		_, isNetError := err.(net.Error)
		if err == io.EOF || isNetError {
			r.conn = nil
		}
	}
	return resp, err
}

func (r *TCPRig) doCmd(format string, nresults int, args ...interface{}) (results []string, err error) {
	// Execute a hamlib command in 'string', expecting 'nresults' values returned, using 'args'
	// Returns a slice with the data in the order returned by the command, if any; if none then empty slice.

	r.tcpConn.SetDeadline(time.Now().Add(TCPTimeout))
	id, err := r.conn.Cmd(format, args...)
	r.tcpConn.SetDeadline(time.Time{})

	if err != nil {
		return nil, err
	}

	r.conn.StartResponse(id)
	defer r.conn.EndResponse(id)

	r.tcpConn.SetDeadline(time.Now().Add(TCPTimeout))

	// Using the hamlib regular protocol.
	// Set commands return no data but 'RPRT 0' for success.
	// 'RPRT -n' is an error, 'n' being a code.
	// Get commands return the data, one value per line, or
	// 'RPRT -n' signalling an error.
	var resp string

	if nresults == 0 { // i.e. a 'Set' command.
		resp, err = r.conn.ReadLine()
		// A set command returns 'RPRT 0' for success or 'RPRT -n' for failure code 'n'.
		if err == nil {
			if !strings.HasPrefix(resp, "RPRT 0") {
				c := fmt.Sprintf(format, args...)
				err = fmt.Errorf("Sent hamlib cmd \"%s\" but it returned error %s", c, resp)
			}
		}
		// Drop out of here with err!=nil if there was a problem.

	} else { // This is a Get command which will produce 'nresults' lines of output.
		for i := 0; i < nresults; i++ {
			resp, err = r.conn.ReadLine()
			if err != nil {
				break
			} else if strings.HasPrefix(resp, "RPRT") {
				// Some kind of failure. Get commands should not return RPRT 0
				err = fmt.Errorf("Hamlib given %s but returned %s", format, resp)
				break
			}

			results = append(results, resp)
		}
	}

	r.tcpConn.SetDeadline(time.Time{})

	if err != nil {
		return nil, err
	}

	if nresults > 0 && len(results) != nresults {
		return nil, fmt.Errorf("Hamlib command %s returned %d results; expected %d", format, len(results), nresults)
	}

	// ... and finally, all is good.
	return results, nil
}

// func toError(str string) error {
// 	if !strings.HasPrefix(str, "RPRT ") {
// 		return nil
// 	}

// 	parts := strings.SplitN(str, " ", 2)

// 	code, err := strconv.Atoi(parts[1])
// 	if err != nil {
// 		return err
// 	}

// 	switch code {
// 	case 0:
// 		return nil
// 	default:
// 		return fmt.Errorf("code %d", code)
// 	}
// }
