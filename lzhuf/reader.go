// Copyright 2016 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package lzhuf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

// ErrChecksum indicates a checksum or file size mismatch on decode.
var ErrChecksum = errors.New("lzhuf: invalid checksum")

// A Reader is an io.Reader that can be read to retrieve
// uncompressed data from a lzhuf-compressed file.
//
// Lzhuf files store a length and optionally a checksum of the uncompressed data.
// The Reader will return io.ErrUnexpectedEOF when Read reaches the end of the
// uncompressed data. The checksum is verified on Close.
//
// Clients should treat data returned by Read as tentative until they receive the io.EOF
// marking the end of the data. Data consistency should then be verified by calling Close.
type Reader struct {
	r   bitReader
	z   *lzhuf
	err error

	crc16 bool
	crcw  *crcWriter

	header struct {
		crc  crc16 // 2 bytes (only in B2 mode)
		size int32 // 4 bytes
	}

	state struct {
		pos int32
		r   int
		buf bytes.Buffer // Buffer to hold decoded but not yet Read
	}
}

// NewB2Reader creates a new Reader expecting the extended FBB B2 format used by Winlink.
//
// It is the caller's responsibility to call Close on the Reader when done.
func NewB2Reader(r io.Reader) (*Reader, error) { return NewReader(r, true) }

// NewReader creates a new Reader reading the given reader.
//
// If crc16 is true, the Reader will expect and verify a checksum of the compressed data (as per FBB B2).
//
// It is the caller's responsibility to call Close on the Reader when done.
func NewReader(r io.Reader, crc16 bool) (*Reader, error) {
	d := &Reader{z: newLZHUFF(), crc16: crc16, crcw: newCRCWriter()}
	d.state.r = _N - _R
	for i := 0; i < _N-_F; i++ {
		d.z.textBuf[i] = ' '
	}

	if d.crc16 {
		err := binary.Read(r, binary.LittleEndian, &d.header.crc)
		if err != nil {
			return nil, err
		}
	}

	// Copy every byte read into our CRC writer (for checksum)
	r = io.TeeReader(r, d.crcw)
	d.r = newBitReader(r)

	return d, binary.Read(r, binary.LittleEndian, &d.header.size)
}

// Close closes the Reader. It does not close the underlying io.Reader.
//
// If an error was encountered during Read, the error will be returned.
// ErrChecksum is returned if the filesize header does not match the
// number of bytes read, or a crc16 checksum (B2 format) was expected
// but did not match.
//
// If no error is returned, the file has been successfully decompressed.
func (d *Reader) Close() error {
	switch {
	case d.err != nil:
		return d.err
	case d.r.Err() != nil:
		return d.r.Err()
	case d.crc16 && d.header.crc != d.crcw.Sum():
		return ErrChecksum
	case d.header.size != d.state.pos-int32(d.state.buf.Len()):
		return ErrChecksum
	default:
		return nil
	}
}

// Read reads uncompressed data into p. It returns the number of bytes read into p.
//
// At EOF, count is 0 and err is io.EOF (unless len(p) is zero).
func (d *Reader) Read(p []byte) (n int, err error) {
	switch {
	case d.r.Err() == io.EOF && d.state.pos < d.header.size:
		d.err = io.ErrUnexpectedEOF
	case d.r.Err() != nil:
		d.err = d.r.Err()
	case d.state.pos == d.header.size && d.state.buf.Len() == 0:
		return 0, io.EOF
	}

	if d.err != nil {
		return 0, d.err
	}

	n, err = d.state.buf.Read(p)

	var i, j, k, c int
	for n < len(p) && d.r.Err() == nil && d.state.pos < d.header.size {
		c = int(d.decodeChar())

		if c < 256 {
			p[n] = byte(c)
			n++
			d.z.textBuf[d.state.r] = byte(c)
			d.advanceState()
			continue
		}

		i = (d.state.r - d.decodePosition() - 1) & (_N - 1)
		j = c - 255 + _Threshold
		for k = 0; k < j; k++ {
			c = int(d.z.textBuf[(i+k)&(_N-1)])
			if n < len(p) {
				p[n] = byte(c)
				n++
			} else {
				d.state.buf.WriteByte(byte(c))
			}
			d.z.textBuf[d.state.r] = byte(c)
			d.advanceState()
		}
	}

	return n, nil
}

func (d *Reader) advanceState() {
	d.state.r++
	d.state.r &= (_N - 1)
	d.state.pos++
}

func (d *Reader) decodeChar() (c uint) {
	c = uint(d.z.son[_R])

	// Travel from root to leaf,
	// choosing the smaller child node (son[]) if the read bit is 0,
	// the bigger (son[]+1} if 1
	for c < _T {
		c += uint(d.getBit())
		c = uint(d.z.son[c])
	}
	c -= _T
	d.z.update(int(c))
	return c
}

func (d *Reader) decodePosition() int {
	var i, j, c uint

	// Recover upper 6 bits from table
	i = uint(d.getByte())
	c = uint(dCode[i]) << 6
	j = uint(dLen[i])

	// Read lower 6 bits verbatim
	for j -= 2; j > 0; j-- {
		i = (i << 1) + uint(d.getBit())
	}
	return int(c | (i & 0x3f))
}

func (d *Reader) getBit() (c int)  { return d.r.ReadBits(1) }
func (d *Reader) getByte() (c int) { return d.r.ReadBits(8) }
