// Copyright 2016 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package lzhuf

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
)

// A Writer is an io.WriteCloser.
// Writes to a Writer are compressed and writter to w.
type Writer struct {
	w   *bufio.Writer
	z   *lzhuf
	err error

	crc16 bool

	buf             *bytes.Buffer // Encode data here and then write header and copy buf to actual writer
	putbuf          uint
	putlen          uint8
	len, r, s       int
	lastMatchLength int
	preFilled       bool
	fileSize        int32
}

// NewB2Writer returns a new Writer with the extended FBB B2 format used by Winlink.
//
// It is the caller's responsibility to call Close on the WriteCloser when done.
// Writes may be buffered and not flushed until Close.
func NewB2Writer(w io.Writer) *Writer { return NewWriter(w, true) }

// NewWriter returns a new Writer. Writes to the returned writer are compressed and written to w.
//
// If crc16 is true, the header will be prepended with a checksum of the compressed data (as per FBB B2).
//
// It is the caller's responsibility to call Close on the WriteCloser when done.
// Writes may be buffered and not flushed until Close.
func NewWriter(w io.Writer, crc16 bool) *Writer {
	wr := &Writer{w: bufio.NewWriter(w), buf: new(bytes.Buffer), crc16: crc16}

	wr.z = newLZHUFF()
	wr.z.InitTree()

	wr.r = _N - _F
	for i := 0; i < wr.r; i++ {
		wr.z.textBuf[i] = ' '
	}

	return wr
}

// Write writes a compressed form of p to the underlying io.Writer. The
// compressed bytes are not necessarily flushed until the Writer is closed.
func (w *Writer) Write(p []byte) (n int, err error) {
	if w.err != nil {
		return 0, err
	}

	for !w.preFilled && n < len(p) { // Pre-fill lookahead buffer
		w.z.textBuf[w.r+w.len] = p[n]
		n++
		w.fileSize++
		w.len++
		w.z.InsertNode(w.r - w.len)

		w.lastMatchLength = 1
		w.preFilled = w.len == _F
	}

	for n < len(p) {
		w.advance(&p[n])
		n++
		w.fileSize++
	}

	return n, nil
}

// Close closes the Writer, flushing any unwritten data to the underlying
// io.Writer, but does not close the underlying io.Writer.
func (w *Writer) Close() error {
	if w.err != nil {
		return w.err
	}

	// Write remaining data from the lookahead buffer
	for w.len > 0 {
		w.advance(nil)
	}
	w.encode()
	w.encodeEnd()

	var lengthBytes bytes.Buffer
	binary.Write(&lengthBytes, binary.LittleEndian, w.fileSize)

	// Write checksum (2 bytes)
	if w.crc16 {
		sum := crc(append(lengthBytes.Bytes(), w.buf.Bytes()...))
		if err := binary.Write(w.w, binary.LittleEndian, sum); err != nil {
			return err
		}
	}

	// Write filesize (4 bytes)
	if _, err := io.Copy(w.w, &lengthBytes); err != nil {
		return err
	}

	// Write compressed data
	if _, err := io.Copy(w.w, w.buf); err != nil {
		return err
	}

	return w.w.Flush()
}

func (w *Writer) advance(c *byte) {
	if c != nil {
		// Add to lookahead buffer
		w.z.textBuf[w.s] = *c
		if w.s < _F-1 {
			w.z.textBuf[w.s+_N] = *c
		}
		w.len++
	}

	// Process one byte from lookahead buffer
	w.z.InsertNode(w.r)
	w.lastMatchLength--
	if w.lastMatchLength == 0 {
		w.encode()
	}
	w.z.DeleteNode(w.s)
	w.s = (w.s + 1) & (_N - 1)
	w.r = (w.r + 1) & (_N - 1)
	w.len--
}

func (w *Writer) encode() {
	if w.len == 0 {
		return
	}

	// Encode from lookahead buffer
	if w.z.matchLength > w.len {
		w.z.matchLength = w.len
	}
	if w.z.matchLength <= _THRESHOLD {
		w.z.matchLength = 1
		w.encodeChar(uint(w.z.textBuf[w.r]))
	} else {
		w.encodeChar(uint(255 - _THRESHOLD + w.z.matchLength))
		w.encodePosition(uint(w.z.matchPosition))
	}

	w.lastMatchLength = w.z.matchLength
}

func (w *Writer) encodeEnd() {
	if w.putlen == 0 {
		return
	}
	w.err = w.buf.WriteByte(byte(w.putbuf >> 8))
}

func (w *Writer) encodeChar(c uint) {
	// travel from leaf to root
	i, j := uint(0), int(0)
	k := w.z.prnt[c+_T]
	for {
		i >>= 1
		j++

		// if node's address is odd-numbered, choose bigger brother node
		if k&1 != 0 {
			i += 0x8000
		}

		if k = w.z.prnt[k]; k == _R {
			break
		}
	}
	w.putCode(j, i)
	w.z.update(int(c))
}

func (w *Writer) encodePosition(c uint) {
	var i uint

	// output upper 6 bits by table lookup
	i = c >> 6
	w.putCode(int(p_len[i]), uint(p_code[i])<<8)

	// output lower 6 bits verbatim
	w.putCode(6, (c&0x3f)<<10)
}

// Output c bits of code
func (w *Writer) putCode(l int, c uint) {
	if w.err != nil {
		return
	}

	w.putbuf |= c >> w.putlen
	w.putlen += uint8(l)

	if w.putlen < 8 {
		return
	}

	w.err = w.buf.WriteByte(byte(w.putbuf >> 8))
	w.putlen -= 8

	if w.putlen >= 8 {
		w.err = w.buf.WriteByte(byte(w.putbuf))

		w.putlen -= 8
		w.putbuf = c << uint(l-int(w.putlen))
	} else {
		w.putbuf <<= 8
	}
}
