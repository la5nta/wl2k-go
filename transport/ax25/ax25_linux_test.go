// Copyright 2026 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

//go:build libax25 && cgo
// +build libax25,cgo

package ax25

import (
	"os"
	"syscall"
	"testing"
)

func TestListenerCloseIdempotent(t *testing.T) {
	f, err := syscall.Open(os.DevNull, syscall.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}

	// The listener owns f; the first Close closes it.
	ln := &ax25Listener{sock: fd(f), close: make(chan struct{})}
	if err := ln.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-ln.close:
	default:
		t.Error("close channel was not closed")
	}
}
