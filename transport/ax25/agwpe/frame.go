package agwpe

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

type callsign [10]byte

func callsignFromString(s string) callsign { var c callsign; copy(c[:], s); return c }

func (c callsign) String() string { return strFromBytes(c[:]) }

func strFromBytes(b []byte) string {
	idx := bytes.IndexByte(b[:], 0x00)
	if idx < 0 {
		return string(b)
	}
	return string(b[:idx])
}

// header represents the fixed-size AGWPE header
type header struct {
	Port     uint8    // AGWPE Port
	_        [3]byte  // Reserved
	DataKind kind     // The frame code
	_        byte     // Reserved
	PID      uint8    // Frame PID
	_        byte     // Reserved
	From     callsign // From callsign
	To       callsign // To callsign
	DataLen  uint32   // Data length
	_        uint32   // Reserved (User)
}

func (h *header) ReadFrom(r io.Reader) (int64, error) {
	return int64(binary.Size(h)), binary.Read(r, binary.LittleEndian, h)
}

func (h header) WriteTo(w io.Writer) (int64, error) {
	return int64(binary.Size(h)), binary.Write(w, binary.LittleEndian, h)
}

// frame represents the variable-size AGWPE frame
type frame struct {
	header
	Data []byte
}

func (f frame) String() string {
	return fmt.Sprintf("Port: %d. Kind: %c. From: %v. To: %v, Data: %q", f.Port, f.DataKind, f.From, f.To, f.Data)
}

func (f frame) WriteTo(w io.Writer) (int64, error) {
	f.DataLen = uint32(len(f.Data))
	n, err := f.header.WriteTo(w)
	if err != nil {
		return n, err
	}
	m, err := w.Write(f.Data)
	return n + int64(m), err
}

func (f *frame) ReadFrom(r io.Reader) (int64, error) {
	n, err := f.header.ReadFrom(r)
	if err != nil {
		return n, err
	}
	if cap(f.Data) < int(f.header.DataLen) {
		f.Data = make([]byte, int(f.header.DataLen))
	} else {
		f.Data = f.Data[:f.header.DataLen]
	}
	m, err := r.Read(f.Data)
	switch {
	case err != nil:
		return n + int64(m), err
	case m != len(f.Data):
		return n + int64(m), io.ErrUnexpectedEOF
	}
	return n + int64(m), err
}
