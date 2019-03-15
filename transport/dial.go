// Copyright 2016 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package transport

import (
	"errors"
	"net"
	"sync"
)

var (
	ErrInvalidTarget     = errors.New("Invalid or missing target callsign")
	ErrDigisUnsupported  = errors.New("Digipeater path is not supported by this scheme")
	ErrMissingDialer     = errors.New("No dialer has been registered for this scheme")
	ErrUnsupportedScheme = errors.New("Unsupported URL scheme")
)

// DialURL calls the url.Scheme's Dialer.
//
// If the URL's scheme is not registered, ErrMissingDialer is returned.
func DialURL(url *URL) (net.Conn, error) {
	dialers.mu.Lock()
	dialer, ok := dialers.m[url.Scheme]
	dialers.mu.Unlock()

	if ok {
		return dialer.DialURL(url)
	}
	return nil, ErrMissingDialer
}

var dialers struct {
	mu sync.Mutex
	m  map[string]Dialer
}

// RegisterDialer registers a new scheme and it's Dialer.
//
// The list of registered dialers is used by DialURL.
func RegisterDialer(scheme string, dialer Dialer) {
	dialers.mu.Lock()

	if dialers.m == nil {
		dialers.m = make(map[string]Dialer)
	}

	dialers.m[scheme] = dialer

	dialers.mu.Unlock()
}

// UnregisterDialer removes the given scheme's dialer from the list of dialers.
func UnregisterDialer(scheme string) {
	dialers.mu.Lock()
	delete(dialers.m, scheme)
	dialers.mu.Unlock()
}
