[![PkgGoDev](https://pkg.go.dev/badge/github.com/la5nta/wl2k-go)](https://pkg.go.dev/github.com/la5nta/wl2k-go)
[![Build status](https://github.com/la5nta/wl2k-go/actions/workflows/go.yaml/badge.svg)](https://github.com/la5nta/wl2k-go/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/la5nta/wl2k-go)](https://goreportcard.com/report/github.com/la5nta/wl2k-go)

### Looking for the cross platform winlink client? Visit [http://getpat.io](http://getpat.io).

## Overview

wl2k-go is a collection of Go packages implementing various parts needed to build a Winlink client.

The project's goal is to encourage and facilitate development of cross-platform Winlink clients.

_This project is under heavy development and breaking API changes are to be expected._

## Pat: The client application

On 6 March 2016 the cmd/wl2k application **moved** to it's own [repository](https://github.com/la5nta/pat).

Check out [getpat.io](http://getpat.io) for the latest version of the cross platform Winlink client, Pat.

## fbb: The FBB (Winlink 2000 - B2F) protocol implementation

An implementation of the B2 Forwarding Protocol and Winlink 2000 Message Structure (the WL2K-protocol).

```go
mycall := "LA5NTA"
mbox := mailbox.NewDirHandler("/tmp/mailbox", false)
session := fbb.NewSession(
	mycall,
	telnet.TargetCall,
	"JP20qh",
	mbox, // Use /tmp/mailbox as the mailbox for this session
)

// Exchange messages over any connection implementing the net.Conn interface
conn, _ := telnet.Dial(mycall)
session.Exchange(conn)

// Print subjects of messages in the inbox
msgs, _ := mbox.Inbox()
for _, msg := range msgs {
	fmt.Printf("Have message: %s\n", msg.Subject())
}
```

For detailed package documentation, see <http://godoc.org/github.com/la5nta/wl2k-go>.

A big thanks to paclink-unix by Nicholas S. Castellano N2QZ (and others). Without their effort and choice to share their knowledge through open source code, this implementation would probably never exist.

Paclink-unix was used as reference implementation for the B2F protocol since the start of this project.

### Gzip experiment

Gzip message compression has been added as an experimental B2F extension, as an alternative to LZHUF. The feature can be enabled by setting the environment variable `GZIP_EXPERIMENT=1` at runtime.

The protocol extension is negotiated by an additional character (G) in the handshake SID as well as a new proposal code (D), thus making it backwards compatible with software not supporting gzip compression.

The G sid flag tells the other party that gzip is supported through a D-proposal. The D-proposal has the same format as C-proposals, but is used to flag the data as gzip compressed.

The gzip feature works transparently, which means that it will not break protocol if it's unsupported by the other winlink node.

## lzhuf: The compression

Package lzhuf implements the lzhuf compression used by the binary FBB protocols B, B1 and B2.

For detailed package documentation, see <http://godoc.org/github.com/la5nta/wl2k-go/lzhuf>.

## transport

Package transport provides access to various connected modes commonly used for winlink.

The modes is made available through common interfaces and idioms from the net package, mainly net.Conn and net.Listener.

For detailed package documentation, see <http://godoc.org/github.com/la5nta/wl2k-go/transport>.

#### telnet
* A simple TCP dialer/listener for the "telnet"-method.
* Supports both P2P and CMS dialing.

#### ax25
* Wrapper for the Linux AX.25 library (build with tag "libax25").
* Kenwood TH-D7x/TM-D7x0 (or similar) TNCs over serial.

#### ardop
A driver for the ARDOP_WIN and ARDOPc TNCs. Provides dialing and listen capabilities over ARDOP (Amateur Radio Digital Open Protocol).

## mailbox: Directory based MBoxHandler implementation

For detailed package documentation, see <http://godoc.org/github.com/la5nta/wl2k-go/mailbox>.

```go
mbox := mailbox.NewDirHandler("/tmp/mailbox", false)

session := fbb.NewSession(
    "N0CALL",
    telnet.TargetCall,
    "JP20qh",
    mbox,
)
```

## rigcontrol/hamlib

Go bindings for a _subset_ of hamlib. It provides both native cgo bindings and a rigctld client.

Build with `-tags libhamlib` to link against libhamlib (the native library).

See <http://godoc.org/github.com/la5nta/wl2k-go/rigcontrol/hamlib> for more details.

## Copyright/License

Copyright (c) 2014-2015 Martin Hebnes Pedersen LA5NTA

(See LICENSE)

## Thanks to

The JNOS developers for the lzhuf implementation which got ported to Go.

The paclink-unix team (Nicholas S. Castellano N2QZ and others) - reference implementation

Amateur Radio Safety Foundation, Inc. - The Winlink 2000 project

F6FBB Jean-Paul ROUBELAT - the FBB forwarding protocol

### Contributors (alphabetical)

* LA3QMA - Kai Günter Brandt
* LA5NTA - Martin Hebnes Pedersen
* Colin Stagner

_wl2k-go is not affiliated with The Winlink Development Team nor the Winlink 2000 project [http://winlink.org]._
