package agwpe

import (
	"sync"
)

type framesFilter struct {
	kinds []kind
	port  *uint8
	call  callsign // to OR from
	to    callsign
}

type framesReq struct {
	framesFilter
	once bool
	done chan struct{}
	resp chan frame
}

func newFramesReq(bufSize int, filter framesFilter) framesReq {
	return framesReq{
		framesFilter: filter,
		done:         make(chan struct{}),
		resp:         make(chan frame, bufSize),
	}
}

func (r framesReq) Cancel() { close(r.done) }

func (f framesFilter) Want(frame frame) bool {
	switch {
	case f.port != nil && *f.port != frame.Port:
		return false
	case f.call != (callsign{}) && !(f.call == frame.From || f.call == frame.To):
		return false
	case f.to != (callsign{}) && !(f.to == frame.To):
		return false
	}
	if len(f.kinds) == 0 {
		return true
	}
	for _, k := range f.kinds {
		if frame.DataKind == k {
			return true
		}
	}
	return false
}

type demux struct {
	requests chan framesReq

	mu     sync.Mutex
	closed bool
	in     chan frame
}

func newDemux() *demux {
	d := demux{
		in:       make(chan frame, 1),
		requests: make(chan framesReq),
	}
	go d.run()
	return &d
}

func (d *demux) isClosed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closed
}

func (d *demux) Chain(filter framesFilter) *demux {
	if d.isClosed() {
		panic("demux closed")
	}
	next := newDemux()
	filtered, cancel := d.Frames(0, filter)
	go func() {
		defer cancel()
		defer next.Close()
		defer debugf("chain exited")
		for {
			f, ok := <-filtered
			if !ok {
				return
			}
			if !next.Enqueue(f) {
				return
			}
		}
	}()
	return next
}

func (d *demux) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	close(d.in)
	d.closed = true
	return nil
}

func (d *demux) Enqueue(f frame) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return false
	}
	select {
	case d.in <- f:
	default:
		debugf("port buffer full - dropping frame")
	}
	return true
}

func (d *demux) NextFrame(kinds ...kind) <-chan frame {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		c := make(chan frame)
		close(c)
		return c
	}
	req := newFramesReq(1, framesFilter{kinds: kinds})
	req.once = true
	d.requests <- req
	return req.resp
}

func (d *demux) Frames(bufSize int, filter framesFilter) (filtered <-chan frame, cancel func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, func() {}
	}
	req := newFramesReq(bufSize, filter)
	req.once = false
	d.requests <- req
	return req.resp, req.Cancel
}

func (d *demux) run() {
	defer debugf("demux exited")
	var clients []framesReq
	for {
		select {
		case c := <-d.requests:
			clients = append(clients, c)
		case f, ok := <-d.in:
			if !ok {
				debugf("demux closing (%d clients)...", len(clients))
				for _, c := range clients {
					close(c.resp)
				}
				clients = nil
				return
			}
			// Match against active clients
			for i := 0; i < len(clients); i++ {
				c := clients[i]
				if !c.Want(f) {
					continue
				}
				select {
				case c.resp <- f:
					if !c.once {
						continue
					}
				case <-c.done:
				}
				close(c.resp)
				clients = append(clients[:i], clients[i+1:]...)
				i--
			}
		}
	}
}
