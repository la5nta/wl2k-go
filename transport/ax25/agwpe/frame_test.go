package agwpe

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"
)

func TestHeaderRoundtrip(t *testing.T) {
	tests := []header{
		{},
		{DataLen: 1024},
		{
			From:    callsign{'L', 'A', '5', 'N', 'T', 'A', '-', '1', 0x00, 0x00},
			To:      callsign{'L', 'A', '5', 'N', 'T', 'A', '-', '2', 0x00, 0x00},
			DataLen: 1024,
		},
	}

	for i, h := range tests {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			var buf bytes.Buffer
			if _, err := h.WriteTo(&buf); err != nil {
				t.Fatal(err)
			}
			var got header
			if _, err := got.ReadFrom(&buf); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(h, got) {
				t.Error("not equal", h, got)
			}
		})
	}
}

func TestFrameRoundtrip(t *testing.T) {
	tests := []frame{
		{},
		{
			header: header{DataLen: 1},
			Data:   []byte{0x10},
		},
	}

	for i, f := range tests {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			var buf bytes.Buffer
			if _, err := f.WriteTo(&buf); err != nil {
				t.Fatal(err)
			}
			var got frame
			if _, err := got.ReadFrom(&buf); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(f.header, got.header) {
				t.Error("header not equal")
			}
			if !reflect.DeepEqual(f.Data, got.Data) {
				t.Error("data not equal")
			}
		})
	}
}

func TestFrameDecode(t *testing.T) {
	raw := []byte{
		0x01, 0x00, 0x00, 0x00, 0x4D, 0x00, 0xCF, 0x00, 0x4C, 0x55, 0x37, 0x44, 0x49, 0x44, 0x2D, 0x34,
		0x00, 0x00, 0x4E, 0x4F, 0x44, 0x45, 0x53, 0x00, 0x00, 0x00, 0x00, 0x00, 0x07, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0xFF, 0x41, 0x42, 0x52, 0x4F, 0x57, 0x4E,
	}
	expect := frame{
		header: header{
			Port:     1,
			DataKind: 'M',
			PID:      207,
			From:     callsignFromString("LU7DID-4"),
			To:       callsignFromString("NODES"),
			DataLen:  7,
		},
		Data: []byte{0xFF, 0x41, 0x42, 0x52, 0x4F, 0x57, 0x4E},
	}
	got := frame{}
	if _, err := got.ReadFrom(bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(expect, got) {
		t.Error("got unexpected output")
	}
}
