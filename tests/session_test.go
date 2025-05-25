package tests

import (
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/la5nta/wl2k-go/fbb"
	"github.com/la5nta/wl2k-go/mailbox"
	"github.com/la5nta/wl2k-go/transport/telnet"
)

type Station struct {
	Callsign string
	MBox     *mailbox.DirHandler
	path     string
}

func (s *Station) Cleanup() {
	os.Remove(s.path)
}

func (s *Station) ListenTelnet() (string, <-chan error, error) {
	errors := make(chan error, 10)

	ln, err := telnet.Listen("localhost:0")
	if err != nil {
		return "", nil, err
	}

	go func() {
		defer ln.Close()
		conn, err := ln.Accept()
		if err != nil {
			errors <- err
			return
		}
		defer conn.Close()

		conn.SetDeadline(time.Now().Add(time.Minute))
		session := fbb.NewSession(s.Callsign, conn.(*telnet.Conn).RemoteCall(), "", s.MBox)
		session.SetLogger(log.New(os.Stderr, s.Callsign+": ", 0))
		session.IsMaster(true)
		if _, err := session.Exchange(conn); err != nil {
			errors <- err
			return
		}
		close(errors)
	}()

	return ln.Addr().String(), errors, nil
}

func NewTempStation(callsign string) (*Station, error) {
	path, err := ioutil.TempDir("", callsign)
	if err != nil {
		return nil, err
	}

	mbox := mailbox.NewDirHandler(path, false)
	mbox.Prepare()

	return &Station{
		Callsign: callsign,
		MBox:     mbox,
		path:     path,
	}, nil
}

func TestMultiBlockAllDeferred(t *testing.T) {
	alice, _ := NewTempStation("N0DE1")
	defer alice.Cleanup()

	bob, _ := NewTempStation("N0DE2")
	defer bob.Cleanup()

	// Add 6 outbound messages
	msgs := NewRandomMessages(6, alice.Callsign, bob.Callsign)
	for _, msg := range msgs {
		alice.MBox.AddOut(msg)
	}

	// Fake msgs already delivered
	bob.MBox.ProcessInbound(msgs...)

	// Start alice as telnet listener
	addr, errors, err := alice.ListenTelnet()
	if err != nil {
		t.Fatalf("Unable to start listener: %s", err)
	}

	// Connect to alice from bob via telnet
	conn, err := telnet.Dial(addr, bob.Callsign, "")
	if err != nil {
		t.Fatalf("Unable to connect to listener: %s", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(time.Minute))
	s := fbb.NewSession(bob.Callsign, bob.Callsign, "", bob.MBox)
	s.SetLogger(log.New(os.Stderr, bob.Callsign+": ", 0))
	if _, err := s.Exchange(conn); err != nil {
		t.Fatalf("Exchange failed at connecting node: %s", err)
	}

	select {
	case err, ok := <-errors:
		if !ok {
			break // No error occurred
		}
		t.Fatalf("Exchange failed at listening node: %s", err)
	case <-time.After(time.Minute):
		t.Fatalf("Test timeout!")
	}

	if n := alice.MBox.OutboxCount(); n != 0 {
		t.Errorf("Unexpected QTC in %s's mailbox. Expected 0, got %d.", alice.Callsign, n)
	}
}

func NewRandomMessages(n int, from, to string) []*fbb.Message {
	msgs := make([]*fbb.Message, n)
	for i := 0; i < n; i++ {
		msgs[i] = NewRandomMessage(from, to)
	}
	return msgs
}

func NewRandomMessage(from, to string) *fbb.Message {
	msg := fbb.NewMessage(fbb.Private, from)

	msg.AddTo(to)
	msg.SetSubject(RandStringRunes(10))
	msg.SetBody(RandStringRunes(100))

	return msg
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}
