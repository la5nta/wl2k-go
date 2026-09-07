// Copyright 2021 Jeremy Bush (N8JJA). All rights reserved.
// Copyright 2026 Rhizomatica. All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package mercury

import (
	"strings"
	"sync"
)

// pubSub is a simple fan-out for the asynchronous control-port lines received
// from the TNC. Subscribers may optionally filter by line prefix.
type pubSub struct {
	in        chan string
	req       chan subscriber
	closeOnce *sync.Once
}

type subscriber struct {
	c        chan<- string
	quit     <-chan struct{}
	prefixes []string
}

func (s subscriber) wants(v string) bool {
	if s.prefixes == nil {
		return true
	}
	for _, prefix := range s.prefixes {
		if strings.HasPrefix(v, prefix) {
			return true
		}
	}
	return false
}

func (pb pubSub) Close() { pb.closeOnce.Do(func() { close(pb.in) }) }

func newPubSub() pubSub {
	pb := pubSub{
		in:        make(chan string),
		req:       make(chan subscriber),
		closeOnce: new(sync.Once),
	}
	go func() {
		var subscribers []subscriber
		defer func() {
			// Signal all subscribers that the publisher is done.
			for _, r := range subscribers {
				close(r.c)
			}
		}()
		for {
			select {
			case v, ok := <-pb.in:
				if !ok {
					return
				}
				for i := 0; i < len(subscribers); i++ {
					s := subscribers[i]
					if !s.wants(v) {
						continue
					}
					select {
					case <-s.quit:
						subscribers = append(subscribers[:i], subscribers[i+1:]...)
						i--
					case s.c <- v:
					}
				}
			case req, ok := <-pb.req:
				if !ok {
					return
				}
				subscribers = append(subscribers, req)
			}
		}
	}()
	return pb
}

func (in pubSub) Publish(v string) { in.in <- v }

func (in pubSub) Subscribe(prefix ...string) (<-chan string, func()) {
	c := make(chan string, 1)
	q := make(chan struct{}, 1)
	in.req <- subscriber{c, q, prefix}
	return c, func() {
		select {
		case q <- struct{}{}:
		default:
		}
	}
}
