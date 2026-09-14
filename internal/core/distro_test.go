package core

import (
	"context"
	"strings"
	"testing"
)

// stubUnit answers for a box the test does not have.
type stubUnit struct {
	Unit
	unit   string
	status UnitStatus
	err    error
}

func (s stubUnit) Status(context.Context) (UnitStatus, error) {
	if s.err != nil {
		return UnitStatus{Unit: s.unit}, s.err
	}
	st := s.status
	st.Unit = s.unit
	return st, nil
}

// withUnits points DistroConflicts at a fake systemd for the duration of a test.
// Units not named are absent from the box.
func withUnits(t *testing.T, units map[string]UnitStatus) {
	t.Helper()
	prev := newUnit
	t.Cleanup(func() { newUnit = prev })
	newUnit = func(name string) (Unit, error) {
		return stubUnit{unit: name, status: units[name]}, nil
	}
}

// The table is the product decision, so it is asserted rather than trusted:
// every entry must carry a fix, and systemd-resolved's must not be "stop it".
// The box resolves through systemd-resolved, so disabling it takes name
// resolution away from the machine the operator is typing on — including from
// apt — until olr's relay is serving.
func TestDistroBackendsCarryAFixAndSpareTheResolver(t *testing.T) {
	for _, b := range DistroBackends {
		if b.Fix == "" {
			t.Errorf("%s has no fix", b.Unit)
		}
		if b.Detail == "" {
			t.Errorf("%s has no detail", b.Unit)
		}
		if len(b.Ports) == 0 {
			t.Errorf("%s claims no ports, so no module will ever ask about it", b.Unit)
		}
		if b.Unit == "systemd-resolved.service" {
			if !strings.Contains(b.Fix, "DNSStubListener=no") {
				t.Errorf("systemd-resolved fix should hand over the socket, not stop it:\n%s", b.Fix)
			}
			if strings.Contains(b.Fix, "disable --now systemd-resolved") ||
				strings.Contains(b.Fix, "stop systemd-resolved") {
				t.Errorf("told the operator to stop the resolver the box depends on:\n%s", b.Fix)
			}
			// The drop-in, not an edit of resolved.conf: removable in one
			// command and no conffile prompt on the next apt upgrade.
			if !strings.Contains(b.Fix, "resolved.conf.d") {
				t.Errorf("systemd-resolved fix edits the main file instead of a drop-in:\n%s", b.Fix)
			}
			continue
		}
		if !strings.Contains(b.Fix, "disable --now "+b.Unit) {
			t.Errorf("%s fix does not disable it:\n%s", b.Unit, b.Fix)
		}
	}
}

// dnsmasq holds both ports, so it has to reach both modules. Getting this wrong
// is silent: the DHCP page would simply never mention the daemon taking its
// socket.
func TestDnsmasqBlocksBothDhcpAndDns(t *testing.T) {
	withUnits(t, map[string]UnitStatus{
		"dnsmasq.service": {Installed: true, Active: true, Enabled: true},
	})

	for _, port := range []int{53, 67} {
		got := DistroConflicts(context.Background(), port)
		if len(got) != 1 || got[0].Unit != "dnsmasq.service" {
			t.Fatalf("port %d: got %+v; want one dnsmasq.service blocker", port, got)
		}
		if got[0].Kind != BlockerDistroBackend {
			t.Errorf("port %d: kind is %q", port, got[0].Kind)
		}
		if !strings.Contains(got[0].Fix, "disable --now dnsmasq.service") {
			t.Errorf("port %d: fix does not name the unit:\n%s", port, got[0].Fix)
		}
	}
}

// A unit that is stopped but enabled is still reported. It comes back at the
// next power cut and fights olr for the socket with nobody watching, which is
// the worst moment to discover it and the cheapest one to prevent.
func TestStoppedButEnabledStillBlocks(t *testing.T) {
	withUnits(t, map[string]UnitStatus{
		"unbound.service": {Installed: true, Active: false, Enabled: true},
	})

	got := DistroConflicts(context.Background(), 53)
	if len(got) != 1 {
		t.Fatalf("got %+v; want the stopped-but-enabled unit reported", got)
	}
	if !strings.Contains(got[0].Summary, "enabled at boot") {
		t.Errorf("summary does not say which half of live is true: %q", got[0].Summary)
	}
}

// Installed and doing nothing is not a conflict. Debian leaves plenty of units
// on disk that nobody has asked for, and a router page that cried about each of
// them would teach the operator to stop reading.
func TestInstalledButIdleDoesNotBlock(t *testing.T) {
	withUnits(t, map[string]UnitStatus{
		"dnsmasq.service": {Installed: true, Active: false, Enabled: false},
	})

	if got := DistroConflicts(context.Background(), 53); len(got) != 0 {
		t.Fatalf("got %+v; want nothing for an idle unit", got)
	}
}

// Not installed at all is the ordinary case and must stay silent.
func TestAbsentUnitsDoNotBlock(t *testing.T) {
	withUnits(t, map[string]UnitStatus{})

	if got := DistroConflicts(context.Background(), 53); len(got) != 0 {
		t.Fatalf("got %+v; want nothing on a box with no distro backends", got)
	}
}

// A box with no service manager reports nothing rather than guessing. §5.4: "we
// could not ask" must never reach an operator dressed as "something is wrong".
func TestNoServiceManagerReportsNothing(t *testing.T) {
	prev := newUnit
	t.Cleanup(func() { newUnit = prev })
	newUnit = func(string) (Unit, error) { return nil, ErrNoServiceManager }

	if got := DistroConflicts(context.Background(), 53); len(got) != 0 {
		t.Fatalf("got %+v; want nothing when systemd cannot be asked", got)
	}
}

// `olr enable` asks about the whole box and must see each unit once, however
// many ports it binds — dnsmasq takes two, and warning about it twice would
// read as two problems.
func TestAllConflictsReportEachUnitOnce(t *testing.T) {
	withUnits(t, map[string]UnitStatus{
		"dnsmasq.service": {Installed: true, Active: true, Enabled: true},
		"unbound.service": {Installed: true, Active: true, Enabled: true},
	})

	got := DistroConflictsAll(context.Background())
	if len(got) != 2 {
		t.Fatalf("got %d blockers, want 2 (one per live unit): %+v", len(got), got)
	}
	seen := map[string]bool{}
	for _, b := range got {
		if seen[b.Unit] {
			t.Errorf("%s reported twice", b.Unit)
		}
		seen[b.Unit] = true
	}
}

// A port nothing in the table claims has no blockers, which is what keeps a
// module from having to filter a list that mostly concerns somebody else.
func TestUnclaimedPortHasNoBlockers(t *testing.T) {
	withUnits(t, map[string]UnitStatus{
		"dnsmasq.service": {Installed: true, Active: true, Enabled: true},
	})

	if got := DistroConflicts(context.Background(), 8080); len(got) != 0 {
		t.Fatalf("got %+v; want nothing for a port no backend claims", got)
	}
}
