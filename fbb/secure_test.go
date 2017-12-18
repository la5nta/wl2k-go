// Copyright 2015 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package fbb

import "testing"

func TestSecureLoginResponse(t *testing.T) {
	type test struct{ challenge, password, expect string }

	tests := []test{
		{challenge: "23753528", password: "FOOBAR", expect: "72768415"},
		{challenge: "23753528", password: "FooBar", expect: "95074758"},
	}

	for i, v := range tests {
		if got := secureLoginResponse(v.challenge, v.password); got != v.expect {
			t.Errorf("%d: Got unexpected login response, expected '%s' got '%s'.", i, v.expect, got)
		}
	}
}

func BenchmarkSecureLoginResponse(b *testing.B) {
	for i := 0; i < b.N; i++ {
		secureLoginResponse("23753528", "foobar")
	}
}
