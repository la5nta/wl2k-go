package agwpe

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"time"
)

var (
	ErrTNCClosed  = errors.New("TNC closed")
	ErrPortClosed = errors.New("port closed")
)

type TNC struct {
	conn  net.Conn
	demux *demux
}

func newTNC(conn net.Conn) *TNC {
	t := &TNC{
		conn:  conn,
		demux: newDemux(),
	}
	go t.run()
	return t
}

func (t *TNC) run() {
	defer debugf("TNC run() exited")
	defer t.Close()
	for {
		var f frame
		if err := t.read(&f); err != nil {
			debugf("read failed: %v", err)
			return
		}
		if !t.demux.Enqueue(f) {
			return
		}
	}
}

func OpenTCP(addr string) (*TNC, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return newTNC(conn), nil
}

func (t *TNC) Ping() error { _, err := t.Version(); return err }

func (t *TNC) Version() (string, error) {
	resp := t.demux.NextFrame(kindVersionNumber)
	t.write(versionNumberFrame())
	select {
	case <-time.After(3 * time.Second):
		return "", fmt.Errorf("response timeout")
	case f, ok := <-resp:
		if !ok {
			return "", ErrTNCClosed
		}
		if len(f.Data) != 8 {
			return "", fmt.Errorf("'%c' frame with invalid data length", f.DataKind)
		}
		var v struct{ Major, _, Minor, _ uint16 }
		binary.Read(bytes.NewReader(f.Data), binary.LittleEndian, &v)
		return fmt.Sprintf("%d.%d", v.Major, v.Minor), nil
	}
}

func (t *TNC) Close() error {
	t.demux.Close()
	return t.conn.Close()
}

func (t *TNC) RegisterPort(port int, mycall string) (*Port, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p := newPort(t, uint8(port), mycall)
	if err := p.register(ctx); err != nil {
		t.Close()
		return nil, err
	}
	return p, nil
}

func (t *TNC) write(f frame) error {
	_, err := f.WriteTo(t.conn)
	if err == nil {
		debugf("-> %v", f)
	}
	return err
}

func (t *TNC) read(f *frame) error {
	_, err := f.ReadFrom(t.conn)
	if err == nil {
		debugf("<- %v", *f)
	}
	return err
}

func debugf(s string, v ...interface{}) {
	if t, _ := strconv.ParseBool(os.Getenv("AGWPE_DEBUG")); !t {
		return
	}
	log.Printf(s, v...)
}
