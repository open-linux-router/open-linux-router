package dhcp

import (
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// An operator who hits this is looking at a DHCP server that would not start.
// "port in use" alone does not tell them whose it is, and on an olr box the
// answer is very often the distribution's own dnsmasq — which is why the
// holder, when known, has to appear in the text.
func TestPortInUseErrorNamesTheHolderAndHowToStopIt(t *testing.T) {
	holder := core.Holder{PID: 3712, Name: "dnsmasq", Unit: "dnsmasq.service"}
	msg := portInUseError(holder, true).Error()

	for _, want := range []string{
		"67",
		"dnsmasq (pid 3712, dnsmasq.service)",
		"systemctl disable --now dnsmasq.service",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q:\n%s", want, msg)
		}
	}

	// `stop` alone leaves a unit that comes back at the next boot and takes the
	// port before olr's does. The message has to say so, or the operator learns
	// it from a router that lost DHCP overnight.
	if !strings.Contains(msg, "disable --now") || !strings.Contains(msg, "boot") {
		t.Errorf("the message does not explain why `stop` is not enough:\n%s", msg)
	}
}

// A holder with no unit can still be named. Killing a pid is worse advice than
// disabling a unit, so the message must not offer a systemctl command it cannot
// know will work.
func TestPortInUseErrorHandlesAHolderWithNoUnit(t *testing.T) {
	msg := portInUseError(core.Holder{PID: 4242, Name: "udhcpd"}, true).Error()
	if !strings.Contains(msg, "udhcpd (pid 4242)") {
		t.Errorf("missing the holder:\n%s", msg)
	}
	if strings.Contains(msg, "systemctl") {
		t.Errorf("offered systemctl for a process outside systemd:\n%s", msg)
	}
}

// The lookup needs root, so "cannot tell" is an ordinary outcome rather than an
// edge case, and it has to leave the operator with a way to find out.
func TestPortInUseErrorFallsBackToFindingTheHolder(t *testing.T) {
	msg := portInUseError(core.Holder{}, false).Error()
	for _, want := range []string{"67", "dnsmasq", "ss -lunp"} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q:\n%s", want, msg)
		}
	}
}
