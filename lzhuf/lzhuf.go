// Copyright 2015 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

// Package lzhuf implements the lzhuf compression used by the binary FBB protocols B, B1 and B2.
//
// The compression is LZHUF with a CRC16 checksum of the compressed data prepended (B2F option).
package lzhuf

// #cgo CFLAGS: -DLZHUF=1 -DB2F=1
// #include "lzhuf.h"
import "C"

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"syscall"
)

var ErrChecksum = errors.New("lzhuf: invalid checksum")

//TODO:
// * Modify lzhuf.c's Encode() so we don't have to use temp files.
// * Handle go errors.
func encDec(data []byte, encode bool, crc16 bool) ([]byte, error) {
	// Create temp files
	outf, _ := ioutil.TempFile("", "lzhuf")
	inf, _ := ioutil.TempFile("", "lzhuf")
	defer func() {
		outf.Close()
		os.Remove(outf.Name())
		os.Remove(inf.Name())
	}()

	// Copy data to in file
	io.Copy(inf, bytes.NewBuffer(data))
	inf.Sync()
	inf.Close()

	var bFlag C.int
	if crc16 {
		bFlag = 1
	} else {
		bFlag = 0
	}

	// Encode/Decode the inf to outf
	lzs := C.AllocStruct()
	var retval C.int
	if encode {
		retval = C.Encode(0, C.CString(inf.Name()), C.CString(outf.Name()), lzs, bFlag)
	} else {
		retval = C.Decode(0, C.CString(inf.Name()), C.CString(outf.Name()), lzs, bFlag)
	}
	C.FreeStruct(lzs)

	if retval == -1 {
		return nil, ErrChecksum
	} else if retval != 0 {
		return nil, syscall.Errno(uintptr(retval))
	}

	// Read the compressed/decompressed data from outf
	b, _ := ioutil.ReadAll(outf)

	return b, nil
}

// A Reader is an io.Reader that can be read to retrieve
// uncompressed data from a lzhuf-compressed file.
//
// Lzhuf files store a length and optionally a checksum of the uncompressed data.
// The Reader will return a ErrChecksum when Read
// reaches the end of the uncompressed data if it does not
// have the expected length or checksum.  Clients should treat data
// returned by Read as tentative until they receive the io.EOF
// marking the end of the data.
type Reader struct {
	r            io.Reader
	crc16        bool
	uncompressed *bytes.Reader
	err          error
}

// NewB2Writer creates a new Reader expecting the extended FBB B2 format used by Winlink.
//
// It is the caller's responsibility to call Close on the Reader when done.
func NewB2Reader(r io.Reader) *Reader { return NewReader(r, true) }

// NewReader creates a new Reader reading the given reader.
//
// If crc16 is true, the Reader will expect and verify a checksum of the compressed data (as per FBB B2).
//
// It is the caller's responsibility to call Close on the Reader when done.
func NewReader(r io.Reader, crc16 bool) *Reader { return &Reader{r: r, crc16: crc16} }

// Read reads uncompressed data into p. It returns the number of bytes read into p.
//
// At EOF, count is 0 and err is io.EOF (unless len(p) is zero).
func (z *Reader) Read(p []byte) (int, error) {
	if z.uncompressed == nil {
		var buf bytes.Buffer

		if _, err := io.Copy(&buf, z.r); err != nil {
			return 0, err
		}

		var data []byte
		data, z.err = encDec(buf.Bytes(), false, z.crc16)
		z.uncompressed = bytes.NewReader(data)
	}

	if z.err != nil {
		return 0, z.err
	}

	return z.uncompressed.Read(p)
}

// Close closes the Reader. It does not close the underlying io.Reader.
func (z *Reader) Close() error {
	// Future implementation need to check that we reached io.EOF or encountered
	// any other error while reading.
	return z.err
}

// A Writer is an io.WriteCloser.
// Writes to a Writer are compressed and writter to w.
type Writer struct {
	w     io.Writer
	crc16 bool
	buf   *bytes.Buffer
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
	return &Writer{
		w:     w,
		crc16: crc16,
		buf:   new(bytes.Buffer),
	}
}

// Write writes a compressed form of p to the underlying io.Writer. The
// compressed bytes are not necessarily flushed until the Writer is closed.
func (z *Writer) Write(p []byte) (int, error) { return z.buf.Write(p) }

// Close closes the Writer, flushing any unwritten data to the underlying
// io.Writer, but does not close the underlying io.Writer.
func (z *Writer) Close() error {
	data, err := encDec(z.buf.Bytes(), true, z.crc16)
	if err != nil {
		return err
	}

	_, err = z.w.Write(data)

	return err
}
