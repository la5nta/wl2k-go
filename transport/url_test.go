// Copyright 2016 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package transport

import (
	"net/url"
	"reflect"
	"testing"
)

func TestParseURL(t *testing.T) {
	tests := map[string]URL{
		"ax25:///LA5NTA":                       {Scheme: "ax25", Target: "LA5NTA", Digis: []string{}, Params: url.Values{}},
		"ax25:///LA1B-10/LA5NTA":               {Scheme: "ax25", Target: "LA5NTA", Digis: []string{"LA1B-10"}, Params: url.Values{}},
		"ax25://axport/LA5NTA":                 {Scheme: "ax25", Host: "axport", Target: "LA5NTA", Digis: []string{}, Params: url.Values{}},
		"ax25://0/LA5NTA":                      {Scheme: "ax25", Host: "0", Target: "LA5NTA", Digis: []string{}, Params: url.Values{}},
		"serial-tnc:///LA5NTA?host=/dev/ttyS0": {Scheme: "serial-tnc", Host: "/dev/ttyS0", Target: "LA5NTA", Digis: []string{}, Params: url.Values{"host": []string{"/dev/ttyS0"}}},

		"telnet://LA5NTA:CMSTelnet@server.winlink.org:8772/wl2k": {
			Scheme: "telnet",
			Host:   "server.winlink.org:8772",
			Target: "wl2k",
			User:   url.UserPassword("LA5NTA", "CMSTelnet"),
			Digis:  []string{},
			Params: url.Values{},
		},
	}

	for str, expect := range tests {
		got, err := ParseURL(str)
		if err != nil {
			t.Errorf("'%s': Unexpected error (%s)", str, err)
			continue
		}

		if !reflect.DeepEqual(*got, expect) {
			t.Errorf("'%s':\n\tGot %#v\n\tExpect %#v", str, *got, expect)
		}
	}

	if _, err := ParseURL("ax25:///"); err == nil {
		t.Errorf("Expected error on no target")
	}
}
