//go:build !linux

package dial

import (
	"errors"
	"net"
)

// ErrUnsupported is returned where a mechanism exists only on Linux.
var ErrUnsupported = errors.New("binding a socket to an interface is only supported on Linux")

// bindToDevice refuses rather than dialling unbound.
//
// The same reasoning internal/gateway/probe_other.go gives about an unmarked
// probe. A reflector request that was supposed to leave by one interface and
// left by whichever the routing table preferred does not fail — it returns an
// address, and that address gets published. A check that says "I could not ask
// the way you asked me to" is honest; one that asks a different question and
// answers confidently is worse than none.
//
// Only reached on a developer's machine: the records that need this are the
// multi-WAN ones, and olr runs on Linux.
func bindToDevice(*net.Dialer, string) error { return ErrUnsupported }
