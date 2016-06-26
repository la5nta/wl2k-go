// Copyright 2016 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package fbb

import (
	"reflect"
	"testing"
	"time"
)

func TestParseDate(t *testing.T) {
	tests := []string{
		"2016/12/30 01:00", // The correct format according to winlink.org/b2f.
		"2016.12.30 01:00", // RMS Relay store-and-forward re-formats date headers in this undocumented layout.
		"2016-12-30 01:00", // Seen in a Radio Only message via RMS Relay-3.0.30.0.

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
