package dns

import (
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The regression this file exists for: the message used to assert that the
// holder was systemd-resolved and send the operator to edit resolved.conf. On
// an olr box the holder is frequently dnsmasq — we tell people to install
// dnsmasq, and Debian's full package ships a service that binds :53 — and that
// advice then points at a file which has nothing to do with the problem.
func TestPortInUseErrorNamesDnsmasqRatherThanGuessingResolved(t *testing.T) {
	holder := core.Holder{PID: 3712, Name: "dnsmasq", Unit: "dnsmasq.service"}
	msg := portInUseError(holder, true).Error()

	if !strings.Contains(msg, "dnsmasq (pid 3712, dnsmasq.service)") {
		t.Errorf("missing the holder:\n%s", msg)
	}
	if !strings.Contains(msg, "systemctl disable --now dnsmasq.service") {
		t.Errorf("missing the fix:\n%s", msg)
	}
	if strings.Contains(msg, "resolved.conf") {
		t.Errorf("still sending a dnsmasq operator to resolved.conf:\n%s", msg)
	}
}

// systemd-resolved is the holder that must not be disabled: the box resolves
// through it, so stopping it takes name resolution away from the machine the
// operator is typing on. Telling it to give up the socket is the correct and
// non-obvious move, and that advice must survive.
func TestPortInUseErrorKeepsTheResolvedAdviceForResolved(t *testing.T) {
	holder := core.Holder{PID: 900, Name: "systemd-resolve", Unit: "systemd-resolved.service"}
	msg := portInUseError(holder, true).Error()

	if !strings.Contains(msg, "DNSStubListener=no") || !strings.Contains(msg, "resolved.conf") {
		t.Errorf("lost the resolved advice:\n%s", msg)
	}
	if strings.Contains(msg, "disable --now") {
		t.Errorf("told the operator to disable the resolver the box depends on:\n%s", msg)
	}
}

func TestPortInUseErrorHandlesAHolderWithNoUnit(t *testing.T) {
	msg := portInUseError(core.Holder{PID: 4242, Name: "named"}, true).Error()
	if !strings.Contains(msg, "named (pid 4242)") {
		t.Errorf("missing the holder:\n%s", msg)
	}
	if strings.Contains(msg, "systemctl") {
		t.Errorf("offered systemctl for a process outside systemd:\n%s", msg)
	}
}

// When the lookup fails the guess is allowed back, but only as a guess, and it
// still has to hand over the commands that answer the question.
func TestPortInUseErrorFallsBackToFindingTheHolder(t *testing.T) {
	msg := portInUseError(core.Holder{}, false).Error()
	for _, want := range []string{"53", "ss -lunp", "ss -ltnp", "If it is systemd-resolved"} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q:\n%s", want, msg)
		}
	}
}
