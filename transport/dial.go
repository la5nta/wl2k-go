// Copyright 2016 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package transport

import (
	"context"
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

// noCtxDialer wraps a Dialer to implement the ContextDialer interface.
type noCtxDialer struct{ Dialer }

func (d noCtxDialer) DialURLContext(_ context.Context, url *URL) (net.Conn, error) {
	return d.DialURL(url)
}

// DialURL calls the url.Scheme's Dialer.
//
// If the URL's scheme is not registered, ErrMissingDialer is returned.
func DialURL(url *URL) (net.Conn, error) {
	return DialURLContext(context.Background(), url)
}

// DialURLContext calls the url.Scheme's ContextDialer.
//
// If the URL's scheme is not registered, ErrMissingDialer is returned.
func DialURLContext(ctx context.Context, url *URL) (net.Conn, error) {
	dialers.mu.Lock()
	dialer, ok := dialers.m[url.Scheme]
	dialers.mu.Unlock()
	if !ok {
		return nil, ErrMissingDialer
	}
	return dialer.DialURLContext(ctx, url)
}

var dialers struct {
	mu sync.Mutex
	m  map[string]ContextDialer
}

// RegisterContextDialer registers a new scheme and it's ContextDialer.
//
// The list of registered dialers is used by DialURL and DialURLContext.
func RegisterContextDialer(scheme string, dialer ContextDialer) {
	dialers.mu.Lock()

	if dialers.m == nil {
		dialers.m = make(map[string]ContextDialer)
	}

	dialers.m[scheme] = dialer

	dialers.mu.Unlock()
}

// RigisterDialer registers a new scheme and it's Dialer.
//
// The list of registered dialers is used by DialURL and DialURLContext.
func RegisterDialer(scheme string, dialer Dialer) {
	d, ok := dialer.(ContextDialer)
	if !ok {
		d = noCtxDialer{dialer}
	}
	RegisterContextDialer(scheme, d)
}

// UnregisterDialer removes the given scheme's dialer from the list of dialers.
func UnregisterDialer(scheme string) {
	dialers.mu.Lock()
	delete(dialers.m, scheme)
	dialers.mu.Unlock()
}
