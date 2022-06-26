package agwpe

import (
	"errors"
	"net"
	"sync"
)

var ErrListenerClosed = errors.New("listener closed")

type Listener struct {
	p *Port

	closeOnce sync.Once
	done      chan struct{}
}

func newListener(p *Port) *Listener { return &Listener{p: p, done: make(chan struct{})} }

func (ln *Listener) Accept() (net.Conn, error) {
	select {
	case conn, ok := <-ln.p.inboundConns:
		if !ok {
			return nil, ErrPortClosed
		}
		return conn, nil
	case <-ln.done:
		return nil, ErrListenerClosed
	}
}

func (ln *Listener) Addr() net.Addr { return addr{dest: ln.p.mycall} }

func (ln *Listener) Close() error {
	ln.closeOnce.Do(func() { close(ln.done) })
	return nil
}
