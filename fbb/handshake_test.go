// Copyright 2015 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package fbb

import (
	"fmt"
	"io"
	"reflect"
	"testing"
)

func TestParseFW(t *testing.T) {
	tests := map[string][]Address{
		";FW: LA5NTA":       []Address{AddressFromString("LA5NTA")},
		";FW: LE1OF":        []Address{AddressFromString("LE1OF")},
		";FW: LE1OF LA5NTA": []Address{AddressFromString("LE1OF"), AddressFromString("LA5NTA")},
		";FW: la4tta":       []Address{{Addr: "LA4TTA"}},
	}

	for input, expected := range tests {
		got, err := parseFW(input)
		if err != nil {
			t.Errorf("Got unexpected error while parsing '%s': %s", input, err)
		} else if !reflect.DeepEqual(got, expected) {
			t.Errorf("Expected %s, got %s", expected, got)
		}
	}
}

func TestIsLoginFailure(t *testing.T) {
	tests := map[error]bool{
		fmt.Errorf("[1] Secure login failed - account password does not match. - Disconnecting (88.90.2.192)"): true,
		io.EOF:              false,
		io.ErrUnexpectedEOF: false,
	}

	for err, expect := range tests {
		if got := IsLoginFailure(err); got != expect {
			t.Errorf("'%s' - Expected %t got %t", err.Error(), expect, got)
		}
	}
}
