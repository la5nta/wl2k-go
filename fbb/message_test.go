// Copyright 2016 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package fbb

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode"
)

func TestReadMessageWithWhitespaceBeforeHeader(t *testing.T) {
	m1 := NewMessage(Private, "one-long-mid")
	m1.AddTo("N0CALL")
	m1.SetFrom("LA5NTA")
	m1.SetBody("Hello world")

	// Write the message with leading whitespace garbage
	var buf bytes.Buffer
	buf.WriteString("\r\n\r\n\t ")
	if err := m1.Write(&buf); err != nil {
		t.Fatal(err)
	}

	// Read the message and verify that we decoded the header successfully.
	m2 := &Message{}
	if err := m2.ReadFrom(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m1.Header, m2.Header) {
		t.Error("parsed message header differs", m1, m2)
	}
}

func TestEmptyMessageReadError(t *testing.T) {
	if err := (&Message{}).ReadFrom(strings.NewReader("")); err == nil {
		t.Errorf("Reading empty message did not error")
	}
	if err := (&Message{}).ReadFrom(strings.NewReader("\r\n\r\nfoobar")); err == nil {
		t.Errorf("Reading headerless message did not error")
	}
}

func TestParseDate(t *testing.T) {
	tests := []string{
		"2016/12/30 01:00", // The correct format according to winlink.org/b2f.
		"2016.12.30 01:00", // RMS Relay store-and-forward re-formats date headers in this undocumented layout.
		"2016-12-30 01:00", // Seen in a Radio Only message via RMS Relay-3.0.30.0.
		"20161230010000",   // Format known to be produced by some versions of BPQ Mail.

		// Extended format support to ensure we support common email formats.
		"Fri, 30 Dec 2016 01:00:00 -0000", // RFC 5322, Appendix A.1.1.
		"Fri, 30 Dec 2016 01:00:00 GMT",   // RFC 5322, Appendix A.6.2. Obsolete date.
	}
	expect := time.Date(2016, time.December, 30, 01, 00, 00, 00, time.UTC).Local()

	for _, str := range tests {
		got, _ := ParseDate(str)
		if !got.Equal(expect) {
			t.Errorf("Unexpected Time when parsing `%s`: %s", str, got)
		}
	}
}

func TestAddressFromString(t *testing.T) {
	tests := map[string]Address{
		"LA5NTA":             {Proto: "", Addr: "LA5NTA"},
		"la5nta":             {Proto: "", Addr: "LA5NTA"},
		"LA5NTA@winlink.org": {Proto: "", Addr: "LA5NTA"},
		"LA5NTA@WINLINK.org": {Proto: "", Addr: "LA5NTA"},
		"la5nta@WINLINK.org": {Proto: "", Addr: "LA5NTA"},

		"foo@bar.baz": {Proto: "SMTP", Addr: "foo@bar.baz"},
	}

	for str, expect := range tests {
		got := AddressFromString(str)
		if !reflect.DeepEqual(expect, got) {
			t.Errorf("'%s' got %#v expected %#v", str, got, expect)
		}
	}
}

func TestEncodeNonASCIIFileNames(t *testing.T) {
	msg := NewMessage(Private, "NOCALL")
	msg.AddFile(NewFile("æøå.txt", []byte{}))

	if h := msg.Header.Get("File"); IsIllegalHeader(h) {
		t.Error("Non-ascii character in encoded File header")
	}
}

func TestDecodeNonASCIIFileNames(t *testing.T) {
	msg := NewMessage(Private, "NOCALL")
	msg.AddFile(NewFile("æøå.txt", []byte{}))

	samples := []string{
		msg.Header["File"][0], // Word encoded (round trip)
		"0 æøå.txt",           // UTF8
		"0 \xE6\xF8\xE5.txt",  // Latin1
	}

	for i, v := range samples {
		msg.Header["File"][0] = v

		var buf bytes.Buffer
		msg.Write(&buf)

		decoded := new(Message)
		decoded.ReadFrom(&buf)
		if msg.Files()[0].Name() != "æøå.txt" {
			t.Errorf("Sample %d failed", i)
		}
	}
}

func TestEmptyAttachment(t *testing.T) {
	msg := NewMessage(Private, "N0CALL")
	msg.AddFile(NewFile("foo.txt", nil))
	var buf bytes.Buffer
	if err := msg.Write(&buf); err != nil {
		t.Fatalf("Error writing message: %v", err)
	}
	if !strings.Contains(buf.String(), "File: 0 foo.txt") {
		t.Error("Expected File header")
	}
	decoded := new(Message)
	if err := decoded.ReadFrom(&buf); err != nil {
		t.Fatalf("Error while decoding produced message: %v", err)
	}
	if n := len(msg.Files()); n != 1 {
		t.Fatalf("Expected one attachment after roundtrip, found %d", n)
	}
	f := msg.Files()[0]
	if f.Size() != 0 {
		t.Errorf("Expected size 0 after roundtrip, found %d", f.Size())
	}
	if f.Name() != "foo.txt" {
		t.Errorf("Got unexpected attachment name after roundtrip: %s", f.Name())
	}
	body, err := msg.Body()
	if err != nil {
		t.Errorf("Got error reading body: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("Expected no body, got length %d", len(body))
	}
}

func IsIllegalHeader(str string) bool {
	for _, c := range str {
		if !IsGraphicASCII(c) {
			return true
		}
	}
	return false
}

func IsGraphicASCII(c rune) bool {
	return c <= unicode.MaxASCII && unicode.IsGraphic(c)
}
