// Copyright 2026 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package ardop

import "testing"

func TestListenerCloseIdempotent(t *testing.T) {
	ln := &listener{quit: make(chan struct{})}

	if err := ln.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-ln.quit:
	default:
		t.Error("quit channel was not closed")
	}
}
