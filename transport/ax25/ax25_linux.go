// Copyright 2015 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

//go:build libax25 && cgo
// +build libax25,cgo

package ax25

/*
#include <sys/socket.h>
#include <netax25/ax25.h>
#include <netax25/axlib.h>
#include <netax25/axconfig.h>
#include <fcntl.h>
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"time"
	"unsafe"
)

type ax25Addr C.struct_full_sockaddr_ax25

var numAXPorts int

// bug(martinhpedersen): The AX.25 stack does not support SOCK_STREAM, so any write to the connection
// that is larger than maximum packet length will fail. The b2f impl. requires 125 bytes long packets.
var ErrMessageTooLong = errors.New("Write: Message too long. Consider increasing maximum packet length to >= 125.")
var ErrPortNotExist = errors.New("No such AX port found")

type fd uintptr

type ax25Listener struct {
	sock      fd
	localAddr AX25Addr
	close     chan struct{}
}

func portExists(port string) bool { return C.ax25_config_get_dev(C.CString(port)) != nil }

func loadPorts() (int, error) {
	if numAXPorts > 0 {
		return numAXPorts, nil
	}

	n, err := C.ax25_config_load_ports()
	if err != nil {
		return int(n), err
	} else if n == 0 {
		return 0, fmt.Errorf("No AX.25 ports configured")
	}

	numAXPorts = int(n)
	return numAXPorts, err
}

func checkPort(axPort string) error {
	if axPort == "" {
		return errors.New("Invalid empty axport")
	}
	if _, err := loadPorts(); err != nil {
		return err
	}
	if !portExists(axPort) {
		return ErrPortNotExist
	}
	return nil
}

// Addr returns the listener's network address, an AX25Addr.
func (ln ax25Listener) Addr() net.Addr { return ln.localAddr }

// Close stops listening on the AX.25 port. Already Accepted connections are not closed.
func (ln ax25Listener) Close() error { close(ln.close); return ln.sock.close() }

// Accept waits for the next call and returns a generic Conn.
//
// See net.Listener for more information.
func (ln ax25Listener) Accept() (net.Conn, error) {
	err := ln.sock.waitRead(ln.close)
	if err != nil {
		return nil, err
	}

	nfd, addr, err := ln.sock.accept()
	if err != nil {
		return nil, err
	}

	conn := &Conn{
		localAddr:       ln.localAddr,
		remoteAddr:      AX25Addr{addr},
		ReadWriteCloser: os.NewFile(uintptr(nfd), ""),
	}

	return conn, nil
}

// ListenAX25 announces on the local port axPort using mycall as the local address.
//
// An error will be returned if axPort is empty.
func ListenAX25(axPort, mycall string) (net.Listener, error) {
	if err := checkPort(axPort); err != nil {
		return nil, err
	}

	// Setup local address (via callsign of supplied axPort)
	localAddr := newAX25Addr(mycall)
	if err := localAddr.setPort(axPort); err != nil {
		return nil, err
	}

	// Create file descriptor
	var socket fd
	if f, err := syscall.Socket(syscall.AF_AX25, syscall.SOCK_SEQPACKET, 0); err != nil {
		return nil, err
	} else {
		socket = fd(f)
	}

	if err := socket.bind(localAddr); err != nil {
		return nil, err
	}
	if err := syscall.Listen(int(socket), syscall.SOMAXCONN); err != nil {
		return nil, err
	}

	return ax25Listener{
		sock:      fd(socket),
		localAddr: AX25Addr{localAddr},
		close:     make(chan struct{}),
	}, nil
}

func DialAX25Context(ctx context.Context, axPort, mycall, targetcall string) (*Conn, error) {
	if err := checkPort(axPort); err != nil {
		return nil, err
	}

	// Setup local address (via callsign of supplied axPort)
	localAddr := newAX25Addr(mycall)
	if err := localAddr.setPort(axPort); err != nil {
		return nil, err
	}
	remoteAddr := newAX25Addr(targetcall)

	// Create file descriptor
	var socket fd
	if f, err := syscall.Socket(syscall.AF_AX25, syscall.SOCK_SEQPACKET, 0); err != nil {
		return nil, err
	} else {
		socket = fd(f)
	}

	// Bind
	if err := socket.bind(localAddr); err != nil {
		return nil, err
	}

	// Connect
	err := socket.connectContext(ctx, remoteAddr)
	if err != nil {
		socket.close()
		return nil, err
	}

	return &Conn{
		ReadWriteCloser: os.NewFile(uintptr(socket), axPort),
		localAddr:       AX25Addr{localAddr},
		remoteAddr:      AX25Addr{remoteAddr},
	}, nil
}

// DialAX25Timeout acts like DialAX25 but takes a timeout.
func DialAX25Timeout(axPort, mycall, targetcall string, timeout time.Duration) (*Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	conn, err := DialAX25Context(ctx, axPort, mycall, targetcall)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		// Local timeout reached.
		err = fmt.Errorf("Dial timeout")
	}
	return conn, err
}

func (c *Conn) Close() error {
	if !c.ok() {
		return syscall.EINVAL
	}

	return c.ReadWriteCloser.Close()
}

func (c *Conn) Write(p []byte) (n int, err error) {
	if !c.ok() {
		return 0, syscall.EINVAL
	}

	n, err = c.ReadWriteCloser.Write(p)
	perr, ok := err.(*os.PathError)
	if !ok {
		return
	}

	switch perr.Err.Error() {
	case "message too long":
		return n, ErrMessageTooLong
	default:
		return
	}
}

func (c *Conn) Read(p []byte) (n int, err error) {
	if !c.ok() {
		return 0, syscall.EINVAL
	}

	n, err = c.ReadWriteCloser.Read(p)
	perr, ok := err.(*os.PathError)
	if !ok {
		return
	}

	// TODO: These errors should not be checked using string comparison!
	// The weird error handling here is needed because of how the *os.File treats
	// the underlying fd. This should be fixed the same way as net.FileConn does.
	switch perr.Err.Error() {
	case "transport endpoint is not connected": // We get this error when the remote hangs up
		return n, io.EOF
	default:
		return
	}
}

// DialAX25 connects to the remote station targetcall using the named axport and mycall.
//
// An error will be returned if axPort is empty.
func DialAX25(axPort, mycall, targetcall string) (*Conn, error) {
	return DialAX25Context(context.Background(), axPort, mycall, targetcall)
}

func (sock fd) connectContext(ctx context.Context, addr ax25Addr) (err error) {
	if err = syscall.SetNonblock(int(sock), true); err != nil {
		return err
	}
	defer syscall.SetNonblock(int(sock), false)

	err = sock.connect(addr)
	if err == nil {
		return nil // Connected
	} else if err != syscall.EINPROGRESS {
		return err
	}

	// Wait for response as long as the dial context is valid.
	for {
		if ctx.Err() != nil {
			sock.close()
			return ctx.Err()
		}
		fdset := new(syscall.FdSet)
		maxFd := fdSet(fdset, int(sock))
		tv := syscall.NsecToTimeval(int64(10 * time.Millisecond))
		n, err := syscall.Select(maxFd+1, nil, fdset, nil, &tv)
		switch {
		case n < 0 && err != syscall.EINTR:
			sock.close()
			return err
		case n > 0:
			// Verify that connection is OK
			nerr, err := syscall.GetsockoptInt(int(sock), syscall.SOL_SOCKET, syscall.SO_ERROR)
			if err != nil {
				sock.close()
				return err
			}
			err = syscall.Errno(nerr)
			if nerr != 0 && err != syscall.EINPROGRESS && err != syscall.EALREADY && err != syscall.EINTR {
				sock.close()
				return err
			}
			return nil // Connected
		default:
			// Nothing has changed yet. Keep looping.
			continue
		}
	}
}

// waitRead blocks until the socket is ready for read or the call is canceled
//
// The error syscall.EINVAL is returned if the cancel channel is closed, indicating
// that the socket is being closed by another thread.
func (sock fd) waitRead(cancel <-chan struct{}) error {
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}

	done := make(chan struct{})
	go func() {
		select {
		case <-cancel:
			pw.Write([]byte("\n"))
		case <-done:
			return
		}
	}()
	defer func() { close(done); pw.Close() }()

	fdset := new(syscall.FdSet)
	maxFd := fdSet(fdset, int(sock), int(pr.Fd()))

	syscall.SetNonblock(int(sock), true)
	defer func() { syscall.SetNonblock(int(sock), false) }()

	var n int
	for {
		n, err = syscall.Select(maxFd+1, fdset, nil, nil, nil)
		if n < 0 || err != nil {
			return err
		}

		if fdIsSet(fdset, int(sock)) {
			break // sock is ready for read
		} else {
			return syscall.EINVAL
		}
	}
	return nil
}

func (sock fd) close() error {
	return syscall.Close(int(sock))
}

func (sock fd) accept() (nfd fd, addr ax25Addr, err error) {
	addrLen := C.socklen_t(unsafe.Sizeof(addr))
	n, err := C.accept(
		C.int(sock),
		(*C.struct_sockaddr)(unsafe.Pointer(&addr)),
		&addrLen)

	if addrLen != C.socklen_t(unsafe.Sizeof(addr)) {
		panic("unexpected socklet_t")
	}

	return fd(n), addr, err
}

func (sock fd) connect(addr ax25Addr) (err error) {
	_, err = C.connect(
		C.int(sock),
		(*C.struct_sockaddr)(unsafe.Pointer(&addr)),
		C.socklen_t(unsafe.Sizeof(addr)))

	return
}

func (sock fd) bind(addr ax25Addr) (err error) {
	_, err = C.bind(
		C.int(sock),
		(*C.struct_sockaddr)(unsafe.Pointer(&addr)),
		C.socklen_t(unsafe.Sizeof(addr)))

	return
}

type ax25_address *C.ax25_address

func (a ax25Addr) Address() Address {
	return AddressFromString(
		C.GoString(C.ax25_ntoa(a.ax25_address())),
	)
}

func (a ax25Addr) Digis() []Address {
	digis := make([]Address, a.numDigis())
	for i, digi := range a.digis() {
		digis[i] = AddressFromString(C.GoString(C.ax25_ntoa(digi)))
	}
	return digis
}

func (a *ax25Addr) numDigis() int {
	return int(a.fsa_ax25.sax25_ndigis)
}

func (a *ax25Addr) digis() []ax25_address {
	digis := make([]ax25_address, a.numDigis())
	for i := range digis {
		digis[i] = (*C.ax25_address)(unsafe.Pointer(&a.fsa_digipeater[i]))
	}
	return digis
}

func (a *ax25Addr) ax25_address() ax25_address {
	return (*C.ax25_address)(unsafe.Pointer(&a.fsa_ax25.sax25_call.ax25_call))
}

func (a *ax25Addr) setPort(port string) (err error) {
	C.ax25_aton_entry(
		C.ax25_config_get_addr(C.CString(port)),
		&a.fsa_digipeater[0].ax25_call[0],
	)
	a.fsa_ax25.sax25_ndigis = 1
	return
}

func newAX25Addr(address string) ax25Addr {
	var addr C.struct_full_sockaddr_ax25

	if C.ax25_aton(C.CString(address), &addr) < 0 {
		panic("ax25_aton")
	}
	addr.fsa_ax25.sax25_family = syscall.AF_AX25

	return ax25Addr(addr)
}

func fdSet(p *syscall.FdSet, fd ...int) (max int) {
	// Shamelessly stolen from src/pkg/exp/inotify/inotify_linux.go:
	//
	// Create fdSet, taking into consideration that
	// 64-bit OS uses Bits: [16]int64, while 32-bit OS uses Bits: [32]int32.
	// This only support File Descriptors up to 1024
	//
	fElemSize := 32 * 32 / len(p.Bits)

	for _, i := range fd {
		if i > 1024 {
			panic(fmt.Errorf("fdSet: File Descriptor >= 1024: %v", i))
		}
		if i > max {
			max = i
		}
		p.Bits[i/fElemSize] |= 1 << uint(i%fElemSize)
	}
	return max
}

func fdIsSet(p *syscall.FdSet, i int) bool {
	fElemSize := 32 * 32 / len(p.Bits)
	return p.Bits[i/fElemSize]&(1<<uint(i%fElemSize)) != 0
}
