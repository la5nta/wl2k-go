// Copyright 2026 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package ardop

import (
	"io"
	"sync"
	"testing"
)

type nopReadWriteCloser struct{}

func (nopReadWriteCloser) Read(p []byte) (n int, err error)  { return 0, io.EOF }
func (nopReadWriteCloser) Write(p []byte) (n int, err error) { return len(p), nil }
func (nopReadWriteCloser) Close() error                      { return nil }

func TestTNCCloseConcurrent(t *testing.T) {
	tnc := &TNC{
		in:      newBroadcaster(),
		out:     make(chan string),
		dataOut: make(chan []byte),
		ctrl:    nopReadWriteCloser{},
	}

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tnc.close()
		}()
	}
	wg.Wait()

	if !tnc.closed {
		t.Error("tnc.closed not set after close")
	}
}
