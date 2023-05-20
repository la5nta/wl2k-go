// Copyright 2015 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package ardop

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/la5nta/wl2k-go/transport"
)

type TNC struct {
	ctrl     io.ReadWriteCloser
	dataConn *net.TCPConn

	data *tncConn

	in      broadcaster
	out     chan<- string
	dataOut chan<- []byte
	dataIn  chan []byte

	busy bool

	state State
	heard map[string]time.Time

	selfClose bool

	ptt transport.PTTController

	// CRC checksum of frames and frame type prefixes is not used over TCPIP
	isTCP bool

	connected      bool
	listenerActive bool
	closed         bool

	beacon *beacon
}

// OpenTCP opens and initializes an ardop TNC over TCP.
func OpenTCP(addr string, mycall, gridSquare string) (*TNC, error) {
	ctrlConn, err := net.Dial(`tcp`, addr)
	if err != nil {
		return nil, err
	}

	dataAddr := string(append([]byte(addr[:len(addr)-1]), addr[len(addr)-1]+1)) // Oh no he didn't!
	raddr, _ := net.ResolveTCPAddr("tcp", dataAddr)
	dataConn, err := net.DialTCP(`tcp`, nil, raddr)
	if err != nil {
		return nil, err
	}

	tnc := newTNC(ctrlConn, dataConn)
	tnc.isTCP = true

	return tnc, open(tnc, mycall, gridSquare)
}

func newTNC(ctrl io.ReadWriteCloser, dataConn *net.TCPConn) *TNC {
	return &TNC{
		in:       newBroadcaster(),
		dataIn:   make(chan []byte, 4096),
		ctrl:     ctrl,
		dataConn: dataConn,
		heard:    make(map[string]time.Time),
	}
}

// Open opens and initializes an ardop TNC.
func Open(ctrl io.ReadWriteCloser, mycall, gridSquare string) (*TNC, error) {
	tnc := newTNC(ctrl, nil)
	return tnc, open(tnc, mycall, gridSquare)
}

func open(tnc *TNC, mycall, gridSquare string) error {
	if err := tnc.runControlLoop(); err == io.EOF {
		return ErrBusy
	} else if err != nil {
		return err
	}

	runtime.SetFinalizer(tnc, (*TNC).Close)

	if err := tnc.init(); err == io.EOF {
		return ErrBusy
	} else if err != nil {
		return fmt.Errorf("Failed to initialize TNC: %s", err)
	}

	if err := tnc.SetMycall(mycall); err != nil {
		return fmt.Errorf("Set my call failed: %s", err)
	}

	if err := tnc.SetGridSquare(gridSquare); err != nil {
		return fmt.Errorf("Set grid square failed: %s", err)
	}

	tnc.beacon = initBeacon(tnc)

	return nil
}

// Set the PTT that should be controlled by the TNC.
//
// If nil, the PTT request from the TNC is ignored.
func (tnc *TNC) SetPTT(ptt transport.PTTController) {
	tnc.ptt = ptt
}

func (tnc *TNC) init() (err error) {
	if err = tnc.set(cmdInitialize, nil); err != nil {
		return err
	}

	tnc.state, err = tnc.getState()
	if err != nil {
		return err
	}
	if tnc.state == Offline {
		if err = tnc.SetCodec(true); err != nil {
			return fmt.Errorf("Enable codec failed: %s", err)
		}
	}

	if err = tnc.set(cmdProtocolMode, ModeARQ); err != nil {
		return fmt.Errorf("Set protocol mode ARQ failed: %s", err)
	}

	if err = tnc.SetARQTimeout(DefaultARQTimeout); err != nil {
		return fmt.Errorf("Set ARQ timeout failed: %s", err)
	}

	// Not yet implemented by TNC
	/*if err = tnc.SetAutoBreak(true); err != nil {
		return fmt.Errorf("Enable autobreak failed: %s", err)
	}*/

	// The TNC should only answer inbound ARQ connect requests when
	// requested by the user.
	if err = tnc.SetListenEnabled(false); err != nil {
		return fmt.Errorf("Disable listen failed: %s", err)
	}

	// FSKONLY experiment
	if t, _ := strconv.ParseBool(os.Getenv("ARDOP_FSKONLY_EXPERIMENT")); t {
		if err = tnc.setFSKOnly(true); err != nil {
			return fmt.Errorf("Set FSK only failed: %s", err)
		}
		log.Println("Experimental FSKONLY mode enabled")
	}
	return nil
}

func decodeTNCStream(fType byte, rd *bufio.Reader, isTCP bool, frames chan<- frame, errors chan<- error) {
	for {
		frame, err := readFrameOfType(fType, rd, isTCP)
		if err != nil {
			errors <- err
		} else {
			frames <- frame
		}

		if err == io.EOF {
			break
		}
	}
}

func (tnc *TNC) runControlLoop() error {
	rd := bufio.NewReader(tnc.ctrl)

	// Multiplex the possible TNC->HOST streams (TCP needs two streams) into a single channel of frames
	frames := make(chan frame)
	errors := make(chan error)

	if tnc.isTCP {
		go decodeTNCStream('c', rd, tnc.isTCP, frames, errors)
		go decodeTNCStream('d', bufio.NewReader(tnc.dataConn), tnc.isTCP, frames, errors)
	} else {
		go decodeTNCStream('*', rd, false, frames, errors)
	}

	go func() {
		for { // Handle incoming TNC data
			var frame frame
			var err error
			select {
			case frame = <-frames:
			case err = <-errors:
			}

			if _, ok := err.(*net.OpError); err == io.EOF || ok {
				break
			} else if err != nil {
				if debugEnabled() {
					log.Printf("Error reading frame: %s", err)
				}
				continue
			}

			if debugEnabled() {
				log.Println("frame", frame)
			}

			if d, ok := frame.(dFrame); ok {
				switch {
				case d.ARQFrame():
					if !tnc.connected {
						// ARDOPc is sending non-ARQ data as ARQ frames when not connected
						continue
					}
					select {
					case tnc.dataIn <- d.data:
					case <-time.After(time.Minute):
						go tnc.Disconnect() // Buffer full and timeout
					}
				case d.IDFrame():
					call, _, err := parseIDFrame(d)
					if err == nil {
						tnc.heard[call] = time.Now()
					} else if debugEnabled() {
						log.Println(err)
					}
				}
			}

			line, ok := frame.(cmdFrame)
			if !ok {
				continue
			}

			msg := line.Parsed()
			switch msg.cmd {
			case cmdPTT:
				if tnc.ptt != nil {
					tnc.ptt.SetPTT(msg.Bool())
				}
			case cmdDisconnected:
				tnc.state = Disconnected
				tnc.eof()
			case cmdBuffer:
				tnc.data.updateBuffer(msg.value.(int))
			case cmdNewState:
				tnc.state = msg.State()

				// Close ongoing connections if the new state is Disconnected
				if msg.State() == Disconnected {
					tnc.eof()
				}
			case cmdBusy:
				tnc.busy = msg.value.(bool)
			}

			if debugEnabled() {
				log.Printf("<-- %s\t[%#v]", line, msg)
			}
			tnc.in.Send(msg)
		}

		tnc.close()
	}()

	out := make(chan string)
	dataOut := make(chan []byte)

	tnc.out = out
	tnc.dataOut = dataOut

	go func() {
		for {
			select {
			case str, ok := <-out:
				if !ok {
					return
				}

				if debugEnabled() {
					log.Println("-->", str)
				}

				if err := writeCtrlFrame(tnc.isTCP, tnc.ctrl, str); err != nil {
					if debugEnabled() {
						log.Println(err)
					}
					return // The TNC connection was closed (most likely).
				}
			case data, ok := <-dataOut:
				if !ok {
					return
				}

				var err error
				if tnc.dataConn != nil {
					_, err = tnc.dataConn.Write(data)
				} else {
					_, err = tnc.ctrl.Write(data)
				}

				if err != nil {
					panic(err) // FIXME
				}
			}
		}
	}()
	return nil
}

func (tnc *TNC) eof() {
	if tnc.data != nil {
		close(tnc.dataIn)       // Signals EOF to pending reads
		tnc.data.signalClosed() // Signals EOF to pending writes
		tnc.connected = false   // connect() is responsible for setting it to true
		tnc.dataIn = make(chan []byte, 4096)
		tnc.data = nil
	}
}

// Ping checks the TNC connection for errors
func (tnc *TNC) Ping() error {
	if tnc.closed {
		return ErrTNCClosed
	}

	_, err := tnc.getString(cmdVersion)
	return err
}

// Closes the connection to the TNC (and any on-going connections).
func (tnc *TNC) Close() error {
	if tnc.closed {
		return nil
	}

	if err := tnc.SetListenEnabled(false); err != nil {
		return err
	}

	if err := tnc.Disconnect(); err != nil { // Noop if idle
		return err
	}

	tnc.close()
	return nil
}

func (tnc *TNC) close() {
	if tnc.closed {
		return
	}
	tnc.closed = true // bug(martinhpedersen): Data race in tnc.Close can cause panic on duplicate calls

	tnc.beacon.Close()
	tnc.eof()

	tnc.ctrl.Close()

	tnc.in.Close() // TODO: This may panic due to the race mentioned above. Consider using a mutex to guard tnc.closed.
	close(tnc.out)
	close(tnc.dataOut)

	// no need for a finalizer anymore
	runtime.SetFinalizer(tnc, nil)
}

// Returns true if channel is not clear
func (tnc *TNC) Busy() bool {
	return tnc.busy
}

// Version returns the software version of the TNC
func (tnc *TNC) Version() (string, error) {
	return tnc.getString(cmdVersion)
}

// Returns the current state of the TNC
func (tnc *TNC) State() State {
	return tnc.state
}

// Returns the grid square as reported by the TNC
func (tnc *TNC) GridSquare() (string, error) {
	return tnc.getString(cmdGridSquare)
}

// Returns mycall as reported by the TNC
func (tnc *TNC) MyCall() (string, error) {
	return tnc.getString(cmdMyCall)
}

// Autobreak returns wether or not automatic link turnover is enabled.
func (tnc *TNC) AutoBreak() (bool, error) {
	return tnc.getBool(cmdAutoBreak)
}

// SetAutoBreak Disables/enables automatic link turnover.
func (tnc *TNC) SetAutoBreak(on bool) error {
	return tnc.set(cmdAutoBreak, on)
}

// Sets the ARQ bandwidth
func (tnc *TNC) SetARQBandwidth(bw Bandwidth) error {
	return tnc.set(cmdARQBW, bw)
}

func (tnc *TNC) ARQBandwidth() (Bandwidth, error) {
	str, err := tnc.getString(cmdARQBW)
	if err != nil {
		return Bandwidth{}, err
	}
	bw, err := BandwidthFromString(str)
	if err != nil {
		return Bandwidth{}, fmt.Errorf("invalid ARQBW response: %w", err)
	}
	return bw, nil
}

// Sets the ARQ timeout
func (tnc *TNC) SetARQTimeout(d time.Duration) error {
	return tnc.set(cmdARQTimeout, int(d/time.Second))
}

// Gets the ARQ timeout
func (tnc *TNC) ARQTimeout() (time.Duration, error) {
	seconds, err := tnc.getInt(cmdARQTimeout)
	return time.Duration(seconds) * time.Second, err
}

// Sets the grid square
func (tnc *TNC) SetGridSquare(gs string) error {
	return tnc.set(cmdGridSquare, gs)
}

// SetMycall sets the provided callsign as the main callsign for the TNC
func (tnc *TNC) SetMycall(mycall string) error {
	return tnc.set(cmdMyCall, mycall)
}

// SetCWID sets wether or not to send FSK CW ID after an ID frame.
func (tnc *TNC) SetCWID(enabled bool) error {
	return tnc.set(cmdCWID, enabled)
}

// CWID reports wether or not the TNC will send FSK CW ID after an ID frame.
func (tnc *TNC) CWID() (bool, error) {
	return tnc.getBool(cmdCWID)
}

// SendID will send an ID frame
//
// If CWID is enabled the ID frame will be followed by a FSK CW ID.
func (tnc *TNC) SendID() error {
	return tnc.set(cmdSendID, nil)
}

type beacon struct {
	reset chan time.Duration
	close chan struct{}
}

func (b *beacon) Reset(d time.Duration) { b.reset <- d }

func (b *beacon) Close() {
	if b == nil {
		return
	}
	select {
	case b.close <- struct{}{}:
	default:
	}
}

func initBeacon(tnc *TNC) *beacon {
	b := &beacon{reset: make(chan time.Duration, 1), close: make(chan struct{}, 1)}
	go func() {
		t := time.NewTimer(10)
		t.Stop()
		var d time.Duration
		for {
			select {
			case <-b.close:
				t.Stop()
				return
			case d = <-b.reset:
				t.Stop()
			case <-t.C:
				if tnc.Idle() {
					tnc.SendID()
				}
			}
			if d > 0 {
				t.Reset(d)
			}
		}
	}()
	return b
}

// BeaconEvery starts a goroutine that sends an ID frame (SendID) at the regular interval d
//
// The gorutine will be closed on Close() or if d equals 0.
func (tnc *TNC) BeaconEvery(d time.Duration) error { tnc.beacon.Reset(d); return nil }

// Sets the auxiliary call signs that the TNC should answer to on incoming connections.
func (tnc *TNC) SetAuxiliaryCalls(calls []string) (err error) {
	return tnc.set(cmdMyAux, strings.Join(calls, ", "))
}

// Enable/disable sound card and other resources
//
// This is done automatically on Open(), users should
// normally don't do this.
func (tnc *TNC) SetCodec(state bool) error {
	return tnc.set(cmdCodec, fmt.Sprintf("%t", state))
}

// ListenState() returns a StateReceiver which can be used to get notification when the TNC state changes.
func (tnc *TNC) ListenEnabled() StateReceiver {
	return tnc.in.ListenState()
}

// Heard returns all stations heard by the TNC since it was opened.
//
// The returned map is a map from callsign to last time the station was heard.
func (tnc *TNC) Heard() map[string]time.Time { return tnc.heard }

// Enable/disable TNC response to an ARQ connect request.
//
// This is disabled automatically on Open(), and enabled
// when needed. Users should normally don't do this.
func (tnc *TNC) SetListenEnabled(listen bool) error {
	return tnc.set(cmdListen, fmt.Sprintf("%t", listen))
}

// Enable/disable the FSKONLY mode.
//
// When enabled, the TNC will only use FSK modulation for ARQ connections.
func (tnc *TNC) setFSKOnly(t bool) error {
	return tnc.set(cmdFSKOnly, fmt.Sprintf("%t", t))
}

// Disconnect gracefully disconnects the active connection or cancels an ongoing connect.
//
// The method will block until the TNC is disconnected.
//
// If the TNC is not connecting/connected, Disconnect is
// a noop.
func (tnc *TNC) Disconnect() error {
	if tnc.Idle() {
		return nil
	}

	tnc.eof()

	r := tnc.in.Listen()
	defer r.Close()

	tnc.out <- fmt.Sprintf("%s", cmdDisconnect)
	for msg := range r.Msgs() {
		if msg.cmd == cmdDisconnected {
			return nil
		}
		if tnc.Idle() {
			return nil
		}
	}
	return ErrTNCClosed
}

// Idle returns true if the TNC is not in a connecting or connected state.
func (tnc *TNC) Idle() bool {
	return tnc.state == Disconnected || tnc.state == Offline
}

// Abort immediately aborts an ARQ Connection or a FEC Send session.
func (tnc *TNC) Abort() error {
	return tnc.set(cmdAbort, nil)
}

func (tnc *TNC) getState() (State, error) {
	v, err := tnc.get(cmdState)
	if err != nil {
		return Offline, nil
	}
	return v.(State), nil
}

// Sends a connect command to the TNC. Users should call Dial().
func (tnc *TNC) arqCall(targetcall string, repeat int) error {
	if !tnc.Idle() {
		return ErrConnectInProgress
	}

	r := tnc.in.Listen()
	defer r.Close()

	tnc.out <- fmt.Sprintf("%s %s %d", cmdARQCall, targetcall, repeat)
	for msg := range r.Msgs() {
		switch msg.cmd {
		case cmdFault:
			return fmt.Errorf(msg.String())
		case cmdNewState:
			if tnc.state == Disconnected {
				return ErrConnectTimeout
			}
		case cmdConnected: // TODO: Probably not what we should look for
			tnc.connected = true
			return nil
		}
	}
	return ErrTNCClosed
}

func (tnc *TNC) set(cmd command, param interface{}) (err error) {
	if tnc.closed {
		return ErrTNCClosed
	}

	r := tnc.in.Listen()
	defer r.Close()

	if param != nil {
		tnc.out <- fmt.Sprintf("%s %v", cmd, param)
	} else {
		tnc.out <- string(cmd)
	}

	for msg := range r.Msgs() {
		if msg.cmd == cmd {
			return
		} else if msg.cmd == cmdFault {
			return errors.New(msg.String())
		}
	}
	return ErrTNCClosed
}

func (tnc *TNC) getString(cmd command) (string, error) {
	v, err := tnc.get(cmd)
	if err != nil {
		return "", nil
	}
	return v.(string), nil
}

func (tnc *TNC) getBool(cmd command) (bool, error) {
	v, err := tnc.get(cmd)
	if err != nil {
		return false, nil
	}
	return v.(bool), nil
}

func (tnc *TNC) getInt(cmd command) (int, error) {
	v, err := tnc.get(cmd)
	if err != nil {
		return 0, err
	}
	return v.(int), nil
}

func (tnc *TNC) get(cmd command) (interface{}, error) {
	if tnc.closed {
		return nil, ErrTNCClosed
	}

	r := tnc.in.Listen()
	defer r.Close()

	tnc.out <- string(cmd)
	for msg := range r.Msgs() {
		switch msg.cmd {
		case cmd:
			return msg.value, nil
		case cmdFault:
			return nil, errors.New(msg.String())
		}
	}
	return nil, ErrTNCClosed
}
