// Copyright 2015 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package fbb

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"strconv"
	"strings"
	"time"

	"github.com/la5nta/wl2k-go/transport"
)

var ErrOffsetLimitExceeded error = errors.New("Protocol does not support offset larger than 6 digits")

const (
	ProtocolOffsetSizeLimit = 999999
	MaxBlockSize            = 5

	// Paclink-unix uses 250, protocol maximum is 255, but we use 125 to allow use of AX.25 links with a paclen of 128.
	// TODO: Consider setting this dynamically.
	MaxMsgLength = 125
)

const (
	cmdPrefix = 'F'
	cmdPrompt = '>'

	cmdNoMoreMessages = 'F'
	cmdQuit           = 'Q'
	cmdPropAnswer     = 'S'

	cmdPropA = 'A'
	cmdPropB = 'B'
	cmdPropC = 'C' // Wl2k extended B2 message

	cmdPropD = 'D' // Gzip compressed B2 message (GZIP_EXPERIMENT)
)

const (
	_CHRNUL byte = 0
	_CHRSOH      = 1
	_CHRSTX      = 2
	_CHREOT      = 4
)

func (s *Session) handleOutbound(rw io.ReadWriter) (quitSent bool, err error) {
	outbound := s.outbound()
	var sent map[string]bool

	// Send outbound messages
	if len(outbound) > 0 {
		sent, err = s.sendOutbound(rw, outbound)
		if err != nil {
			return
		}
	}

	// Report rejected now, they can safely be omitted even if an error occures
	for mid, rej := range sent {
		if rej {
			s.h.SetSent(mid, rej)
			delete(sent, mid)
		}
	}

	// Handle session turnover (regardless of number of accepted messages)
	switch {
	case len(outbound) > 0:
		// Turnover is implied
	case s.remoteNoMsgs && len(sent) == 0:
		s.pLog.Print(">FQ")
		fmt.Fprint(rw, "FQ\r")
		quitSent = true
		return // No need to check for remote error since we did not send any messages
	default:
		s.pLog.Print(">FF")
		fmt.Fprint(rw, "FF\r")
	}

	// Error reporting from remote is not defined by the protocol,
	// but usually indicated by sending a line prefixed with '***'.
	// The only valid bytes (according to protocol) after a session
	// turnover is 'F' or ';', so we use those to confirm the block
	// was successfully received.
	var p []byte
	if p, err = s.rd.Peek(1); err != nil {
		return
	} else if p[0] != 'F' && p[0] != ';' {
		var line string
		line, err = s.nextLine()
		if err != nil {
			return
		}
		err = fmt.Errorf("Unexpected response: '%s'", line)
		return
	}

	// Report successfully sent messages
	for mid, rej := range sent {
		s.h.SetSent(mid, rej)
		if !rej {
			s.trafficStats.Sent = append(s.trafficStats.Sent, mid)
		}
	}
	return
}

func (s *Session) sendOutbound(rw io.ReadWriter, outbound []*Proposal) (sent map[string]bool, err error) {
	sent = make(map[string]bool) // Use this to keep track of sent (rejected or not) mids.
	var checksum int64

	if len(outbound) > MaxBlockSize {
		outbound = outbound[0:MaxBlockSize]
	}

	for _, prop := range outbound {
		sp := fmt.Sprintf("F%c %s %s %d %d %d",
			prop.code,           // Proposal code
			prop.msgType,        // Message type (1 or 2 alphanumeric)
			prop.mid,            // Max 12 characters
			prop.size,           // Uncompressed size of message
			prop.compressedSize, // Compressed size of message
			0)                   // ?

		s.pLog.Printf(">%s", sp)
		fmt.Fprintf(rw, "%s\r", sp)
		for _, c := range sp {
			checksum += int64(c)
		}
		checksum += int64('\r')
	}
	checksum = (-checksum) & 0xff

	s.pLog.Printf(">F> %02X", checksum)
	fmt.Fprintf(rw, "F> %02X\r", checksum)

	var reply string
	for reply == "" {
		line, err := s.nextLine()
		switch {
		case err != nil:
			return sent, err
		case strings.HasPrefix(line, "FS "):
			reply = line // The expected proposal answer
		case strings.HasPrefix(line, ";"):
			continue // Ignore comment
		default:
			return sent, fmt.Errorf("Expected proposal answer from remote. Got: %q", reply)
		}
	}

	if err = parseProposalAnswer(reply, outbound, s.log); err != nil {
		return sent, fmt.Errorf("Unable to parse proposal answer: %w", err)
	}

	if len(outbound) == 0 {
		return
	}

	if r, ok := rw.(transport.Robust); ok && s.robustMode == RobustAuto {
		r.SetRobust(false)
		defer r.SetRobust(true)
	}

	for _, prop := range outbound {
		switch prop.answer {
		case Defer:
			s.h.SetDeferred(prop.mid)
		case Reject:
			sent[prop.mid] = true
		case Accept:
			if err = s.writeCompressed(rw, prop); err != nil {
				return
			}
			sent[prop.mid] = false
		}
	}
	return
}

func (s *Session) handleInbound(rw io.ReadWriter) (quitReceived bool, err error) {
	var ourChecksum int64
	proposals := make([]*Proposal, 0)
	var nAccepted int

Loop:
	for {
		var line string
		line, err = s.nextLine()
		if err != nil {
			return
		}

		// Store pending message details (winlink extension)
		if strings.HasPrefix(line, ";PM") {
			if pm, err := parsePM(line); err == nil {
				s.pendingMessages[pm.MID] = pm
			}
			continue
		}

		// Ignore comments and empty lines
		if line == "" || line[0] == ';' {
			continue
		}

		// The line should be prefixed F? (? is the command character)
		if len(line) < 2 || line[0] != 'F' {
			return false, fmt.Errorf("Got unexpected protocol line: '%s'", line)
		}

		switch line[:2] {
		case "FA", "FB", "FC", "FD": // Proposals
			for _, c := range line {
				ourChecksum += int64(c)
			}
			ourChecksum += int64('\r')

			prop := new(Proposal)
			if err = parseProposal(line, prop); err != nil {
				err = fmt.Errorf("Unable to parse proposal: %w", err)
				return
			}
			if pm, ok := s.pendingMessages[prop.mid]; ok {
				prop.pendingMessage = &pm
			}
			proposals = append(proposals, prop)

		case "FF": // No more messages
			s.remoteNoMsgs = true
			break Loop

		case "FQ": // Quit
			quitReceived = true
			break Loop

		case "F>": // Prompt (end of proposal block)
			// Verify checksum
			ourChecksum = (-ourChecksum) & 0xff
			their, _ := strconv.ParseInt(line[3:], 16, 64)
			if their != ourChecksum {
				err = errors.New(fmt.Sprintf(`Checksum error (%d-%d)`, ourChecksum, their))
				return
			}

			// If we didn't get any proposals, return
			if len(proposals) == 0 {
				s.remoteNoMsgs = true
				return
			}

			s.remoteNoMsgs = false

			// Answer proposal
			s.log.Printf(`%d proposal(s) received`, len(proposals))
			_, err = s.writeProposalsAnswer(rw, proposals)
			if err != nil {
				return quitReceived, err
			}

			break Loop // Session turn over is implied (regardless of number of accepted messages)
		default: //TODO: Ignore?
			return false, fmt.Errorf("Unknown protocol command %c", line[1])
		}
	}

	if quitReceived && nAccepted > 0 {
		return true, errors.New("Got quit command when inbound proposals were pending")
	}

	// Fetch and decompress accepted
	for _, prop := range proposals {
		if prop.answer != Accept {
			continue
		}

		var msg *Message
		if err = s.readCompressed(rw, prop); err != nil {
			return
		} else if msg, err = prop.Message(); err != nil {
			return
		}

		if err = s.h.ProcessInbound(msg); err != nil {
			return
		}
		s.trafficStats.Received = append(s.trafficStats.Received, prop.MID())
	}

	return
}

// The B2F protocol does not support offsets larger than 6 digits, the author of the protocol
// seems to have thrown away the idea of supporting transfer of fragmented messages.
//
// If we ever want to support requests of message with offset, we must guard against asking for
// offsets > 999999. RMS Express does not do this (in Winmor P2P anyway), we must avoid that pitfall.
func (s *Session) writeProposalsAnswer(rw io.ReadWriter, proposals []*Proposal) (nAccepted int, err error) {

	seen := make(map[string]bool)

	var unanswered []int
	for idx, prop := range proposals {
		if seen[prop.MID()] {
			// Radio Only gateways will sometimes send multiple proposals for the same MID in the same batch.
			// Instead of rejecting them right away, let's defer the dups until we know we have sucessfully received at least one of the copies.
			s.log.Printf("Defering duplicate message %s", prop.MID())
			prop.answer = Defer
		} else if prop.code != Wl2kProposal && prop.code != GzipProposal {
			s.log.Printf("Defering %s (unsupported format)", prop.MID())
			prop.answer = Defer
		} else if s.h == nil {
			s.log.Printf("Defering %s (missing handler)", prop.MID())
			prop.answer = Defer
		} else {
			unanswered = append(unanswered, idx)
		}

		seen[prop.MID()] = true
	}

	// Now call the inbound handler to get answers for the remaining in this block.
	if batched, ok := s.h.(BatchedInboundHandler); ok {
		check := make([]Proposal, 0, len(unanswered))
		for _, idx := range unanswered {
			check = append(check, *proposals[idx])
		}
		answers := batched.GetInboundAnswers(check)
		for answerIdx, unansweredIdx := range unanswered {
			p := proposals[unansweredIdx]
			p.answer = answers[answerIdx]
		}
	} else {
		for _, idx := range unanswered {
			p := proposals[idx]
			p.answer = s.h.GetInboundAnswer(*p)
		}
	}

	answers := make([]byte, len(proposals))
	for i, prop := range proposals {
		answers[i] = byte(prop.answer)
		if prop.answer == Accept {
			s.log.Printf("Accepting %s", prop.MID())
			nAccepted++
		}
	}
	s.pLog.Printf(">FS %s", answers)
	_, err = fmt.Fprintf(rw, "FS %s\r", answers)
	return
}

// Parses the proposal answer (str) and updates the proposals given (in that order)
func parseProposalAnswer(str string, props []*Proposal, l *log.Logger) error {
	str = strings.TrimPrefix(str, "FS ")

	var c byte
	for i := 0; len(str) > 0; i++ {
		if i >= len(props) {
			return errors.New("Got answer for more proposals than expected")
		}

		prop := props[i]
		c, str = str[0], str[1:]

		switch c {
		case 'Y', 'y', '+':
			if l != nil {
				l.Printf("Remote accepted %s", prop.MID())
			}
			prop.answer = Accept
		case 'N', 'n', 'R', 'r', '-':
			if l != nil {
				l.Printf("Remote already received %s", prop.MID())
			}
			prop.answer = Reject
		case 'L', 'l', '=', 'H', 'h':
			if l != nil {
				l.Printf("Remote defered %s", prop.MID())
			}
			prop.answer = Defer
		case 'A', 'a', '!':
			idx := strings.LastIndexAny(str, "0123456789")
			if idx < 0 {
				return errors.New("Got offset request without offset index")
			}
			prop.answer = Accept // Offset is not implemented as a ProposalAnswer
			prop.offset, _ = strconv.Atoi(str[:idx+1])
			str = str[idx+1:]

			if prop.offset > ProtocolOffsetSizeLimit { // RMS Express does this (in Winmor P2P for sure)
				prop.offset = 0
				if l != nil {
					l.Printf(
						"Remote requested %s at offset %d which exceeds the binary protocol offset limit. Ignoring offset.",
						prop.MID(), prop.offset,
					)
				}
			} else if l != nil {
				l.Printf("Remote accepted %s at offset %d", prop.MID(), prop.offset)
			}
		default:
			return fmt.Errorf("Invalid character (%c) in proposal answer line", c)
		}
	}
	return nil
}

func (s *Session) writeCompressed(rw io.ReadWriter, p *Proposal) (err error) {
	s.log.Printf("Transmitting [%s] [offset %d]", p.title, p.offset)

	if p.code == GzipProposal {
		s.log.Println("GZIP_EXPERIMENT:", "Transmitting gzip compressed message.")
	}

	writer := bufio.NewWriter(rw)

	var (
		title    = mime.QEncoding.Encode("utf-8", p.title) // Word-encode the title since this field must be ASCII-only
		offset   = fmt.Sprintf("%d", p.offset)
		length   = len(title) + len(offset) + 2
		checksum int64
	)

	writer.Write([]byte{_CHRSOH, byte(length)})
	writer.WriteString(title) // Max 80 bytes, min 1 byte
	writer.WriteByte(_CHRNUL)
	writer.WriteString(offset) // Max 6 bytes, min 1 byte. Highest supported offset is 1MB-1B.
	writer.WriteByte(_CHRNUL)
	writer.Flush()

	if p.compressedSize < 6 { // lzhuf's smallest valid length (empty)
		return errors.New(`Invalid compressed data`)
	}

	buffer := bytes.NewBuffer(p.compressedData[p.offset:])

	// Update Status of message transfer every 250ms
	statusTicker := time.NewTicker(250 * time.Millisecond)
	statusDone := make(chan struct{})
	go func() {
		for {
			select {
			case <-statusTicker.C:
				if s.statusUpdater == nil || buffer == nil {
					continue
				}

				// Take into account that the modem has an internal tx buffer (if possible).
				var txBufLen int
				if b, ok := rw.(transport.TxBuffer); ok {
					txBufLen = b.TxBufferLen()
				}

				transferred := p.compressedSize - buffer.Len() - txBufLen
				if transferred < 0 {
					transferred = 0
				}

				if s.statusUpdater != nil {
					s.statusUpdater.UpdateStatus(Status{
						Sending:          p,
						BytesTransferred: transferred,
						BytesTotal:       p.compressedSize,
					})
				}
			case <-statusDone:
				if s.statusUpdater != nil {
					s.statusUpdater.UpdateStatus(Status{
						Sending:          p,
						BytesTransferred: p.compressedSize - buffer.Len(),
						BytesTotal:       p.compressedSize,
						Done:             true,
					})
				}
				return
			}
		}
	}()
	defer func() { close(statusDone) }()

	// Data (in chunks of max 250)
	for buffer.Len() > 0 {
		msgLen := MaxMsgLength
		if buffer.Len() < MaxMsgLength {
			msgLen = buffer.Len()
		}

		if _, err = writer.Write([]byte{_CHRSTX, byte(msgLen)}); err != nil {
			return err
		}

		for i := 0; i < msgLen; i++ {
			c, _ := buffer.ReadByte()
			if err := writer.WriteByte(c); err != nil {
				return err
			}
			checksum += int64(c)
		}

		if err = writer.Flush(); err != nil {
			return err
		}
	}

	// Checksum
	checksum = -checksum & 0xff
	_, err = writer.Write([]byte{_CHREOT, byte(checksum)})
	err = writer.Flush()

	// Flush connection buffers.
	// This enables us to block until the whole message has been transmitted over the air.
	if f, ok := rw.(transport.Flusher); ok {
		err = f.Flush()
	}

	statusTicker.Stop()

	return err
}

func (s *Session) readCompressed(rw io.ReadWriter, p *Proposal) (err error) {
	var (
		ourChecksum int
		buf         bytes.Buffer
	)

	var c byte
	if c, err = s.rd.ReadByte(); err != nil {
		return
	}
	switch c {
	case _CHRSOH:
		// what we expected...
	case '*':
		line, _ := s.nextLine()
		return errors.New(fmt.Sprintf(`Got error from CMS: %s`, line))
	default:
		return errors.New(fmt.Sprintf(`First byte not as expected, got %d`, int(c)))
	}

	if c, err = s.rd.ReadByte(); err != nil {
		return
	}
	headerLength := int(c)

	// Read proposal title.
	title, err := s.rd.ReadString(_CHRNUL)
	if err != nil {
		return fmt.Errorf("Unable to parse title: %w", err)
	}
	title = title[:len(title)-1] // Remove _CHRNUL

	// The proposal title should be ASCII-only according to the protocol specification. Since RMS Express and CMS puts
	// the raw subject header here, we need to handle this by decoding it the same way as the subject header.
	p.title, _ = new(WordDecoder).DecodeHeader(title)

	// Read offset part
	var offsetStr string
	if offsetStr, err = s.rd.ReadString(_CHRNUL); err != nil {
		return fmt.Errorf("Unable to parse offset: %w", err)
	} else {
		offsetStr = offsetStr[:len(offsetStr)-1]
	}

	// Check overall length of header
	actualHeaderLength := (len(title) + len(offsetStr)) + 2
	if headerLength != actualHeaderLength {
		return errors.New(fmt.Sprintf(`Header length mismatch: expected %d, got %d`, headerLength, actualHeaderLength))
	}

	// Parse offset as integer (and do some sanity checks)
	offset, err := strconv.Atoi(offsetStr)
	switch {
	case err != nil:
		return fmt.Errorf("Offset header not parseable as integer: %w", err)
	case offset != p.offset:
		return fmt.Errorf(`Expected offset %d, got %d`, p.offset, offset)
	}

	s.log.Printf("Receiving [%s] [offset %d]", p.title, p.offset)

	if p.code == GzipProposal {
		s.log.Println("GZIP_EXPERIMENT:", "Receiving gzip compressed message.")
	}

	statusUpdate := make(chan struct{})
	go func() {
		for {
			_, ok := <-statusUpdate
			if s.statusUpdater != nil {
				s.statusUpdater.UpdateStatus(Status{
					Receiving:        p,
					BytesTransferred: buf.Len(),
					BytesTotal:       p.compressedSize,
					Done:             !ok,
				})
			}
			if !ok {
				return
			}
		}
	}()
	defer func() { close(statusUpdate) }()
	updateStatus := func() {
		select {
		case statusUpdate <- struct{}{}:
		default:
		}
	}

	for {
		updateStatus()
		c, err = s.rd.ReadByte()
		if err != nil {
			return err
		}

		switch c {
		case _CHRSTX:
			c, _ := s.rd.ReadByte()
			length := int(c)
			if length == 0 {
				length = 256
			}
			for i := 0; i < length; i++ {
				c, err = s.rd.ReadByte()
				if err != nil {
					return
				}
				buf.WriteByte(c)
				ourChecksum = (ourChecksum + int(c)) % 256
				if i%10 == 0 {
					updateStatus()
				}
			}
		case _CHREOT:
			c, _ = s.rd.ReadByte()
			ourChecksum = (ourChecksum + int(c)) % 256
			if ourChecksum != 0 {
				return errors.New(`Bad checksum`)
			} else if p.compressedSize != buf.Len() {
				return errors.New(`Length mismatch after EOT`)
			} else {
				p.compressedData = buf.Bytes()
			}
			return
		default:
			return errors.New(`Unexpected byte in compressed stream: ` + string(c))
		}
	}
}
