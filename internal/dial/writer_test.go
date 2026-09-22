package dial

import (
	"context"
	"net/netip"
	"slices"
	"strings"
	"testing"
)

func desiredUplink() Desired {
	return Desired{
		Interface: "enp2s0",
		Address:   netip.MustParsePrefix("192.168.2.9/24"),
		Gateway:   netip.MustParseAddr("192.168.2.1"),
		Up:        true,
	}
}

// The order is the argument, and it is the property worth pinning: the address
// first so the link comes up already carrying one, the route last because a
// route out of an interface with no address is a route nothing can use.
func TestTheWriterAddressesThenRaisesThenRoutes(t *testing.T) {
	w := &RecordingWriter{}
	steps, err := w.Apply(context.Background(), desiredUplink())
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, s := range steps {
		got = append(got, s.Description)
	}
	want := []string{
		"add 192.168.2.9/24 to enp2s0",
		"bring enp2s0 up",
		"route traffic out via 192.168.2.1 on enp2s0",
	}
	if !slices.Equal(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
}

func TestDesiredForReadsTheStoredUplink(t *testing.T) {
	u := testUplink()
	if got := DesiredFor(Config{Uplink: &u}); got != desiredUplink() {
		t.Errorf("DesiredFor = %+v", got)
	}
	if got := DesiredFor(Config{}); !got.Empty() {
		t.Errorf("a box with no uplink asked for %+v", got)
	}
	// An uplink with no IPv4 block still brings the interface up and writes
	// nothing else — the shape the DHCP-client form will arrive in.
	bare := DesiredFor(Config{Uplink: &Uplink{Interface: "enp2s0"}})
	if bare.Empty() || bare.Address.IsValid() || bare.Gateway.IsValid() {
		t.Errorf("a bare uplink asked for %+v", bare)
	}
}

func TestAnAgreeingKernelNeedsNoWork(t *testing.T) {
	obs := Observed{
		Present:    true,
		Up:         true,
		Addrs:      []netip.Prefix{netip.MustParsePrefix("192.168.2.9/24")},
		Gateway:    netip.MustParseAddr("192.168.2.1"),
		GatewayDev: "enp2s0",
	}
	if plan := PlanUplink(desiredUplink(), obs); !plan.Empty() {
		t.Errorf("an already-correct box produced %+v", plan)
	}
}

// §5.4: somebody who runs `ip route del default` by hand has to show up here
// the same way a hand-edited config file does in the file-rendering modules.
func TestADeletedRouteIsDrift(t *testing.T) {
	obs := Observed{
		Present: true,
		Up:      true,
		Addrs:   []netip.Prefix{netip.MustParsePrefix("192.168.2.9/24")},
	}
	plan := PlanUplink(desiredUplink(), obs)
	if plan.Empty() || !plan.Gateway.IsValid() {
		t.Fatalf("a missing default route did not show as drift: %+v", plan)
	}
	// Nothing to displace, so this is not a takeover.
	if plan.ReplacesGateway.IsValid() {
		t.Error("writing a route onto a box that had none reported a replacement")
	}
}

// The distinction the impact rests on: providing a way out takes nothing away,
// taking one over can drop the session asking for it.
func TestReplacingSomebodyElsesDefaultRouteIsReported(t *testing.T) {
	obs := Observed{
		Present:    true,
		Up:         true,
		Addrs:      []netip.Prefix{netip.MustParsePrefix("192.168.2.9/24")},
		Gateway:    netip.MustParseAddr("192.168.2.254"),
		GatewayDev: "enp1s0",
	}
	plan := PlanUplink(desiredUplink(), obs)
	if plan.ReplacesGateway != netip.MustParseAddr("192.168.2.254") {
		t.Errorf("the route being displaced was not reported: %+v", plan)
	}
}

// The whole point of the object, in one assertion: this writer never removes
// what something else put on the uplink. internal/link's PlanAddrs does the
// opposite, deliberately, and PlanUplink's comment has the argument.
func TestForeignAddressesAreReportedAndNeverRemoved(t *testing.T) {
	leased := netip.MustParsePrefix("100.64.3.8/10")
	obs := Observed{
		Present: true,
		Up:      true,
		Addrs:   []netip.Prefix{netip.MustParsePrefix("192.168.2.9/24"), leased},
		Gateway: netip.MustParseAddr("192.168.2.1"), GatewayDev: "enp2s0",
	}

	plan := PlanUplink(desiredUplink(), obs)
	if !slices.Contains(plan.Foreign, leased) {
		t.Errorf("a foreign address was not reported: %+v", plan)
	}
	for _, line := range DescribeUplinkPlan(plan) {
		if strings.Contains(line, "del") {
			t.Errorf("the plan removes an address it did not put there: %s", line)
		}
	}
}

func TestADownInterfaceIsBroughtUp(t *testing.T) {
	obs := Observed{
		Present: true,
		Addrs:   []netip.Prefix{netip.MustParsePrefix("192.168.2.9/24")},
		Gateway: netip.MustParseAddr("192.168.2.1"), GatewayDev: "enp2s0",
	}
	plan := PlanUplink(desiredUplink(), obs)
	if !plan.BringUp {
		t.Errorf("a down interface was left down: %+v", plan)
	}
}

// A route that exists and leaves by the wrong interface is the failure this
// whole read exists for: the box looks configured and sends everything the
// wrong way.
func TestARouteOutOfTheWrongInterfaceIsDrift(t *testing.T) {
	obs := Observed{
		Present: true, Up: true,
		Addrs:      []netip.Prefix{netip.MustParsePrefix("192.168.2.9/24")},
		Gateway:    netip.MustParseAddr("192.168.2.1"),
		GatewayDev: "enp1s0",
	}
	if plan := PlanUplink(desiredUplink(), obs); plan.Empty() {
		t.Error("a default route out of the wrong interface read as agreement")
	}
}

// §5.2 gives an uplink change no rollback, so a half-landed apply has to come
// back as what happened rather than as an error alone.
func TestAFailedApplyStillReportsItsSteps(t *testing.T) {
	w := &RecordingWriter{Err: context.DeadlineExceeded}
	steps, err := w.Apply(context.Background(), desiredUplink())
	if err == nil {
		t.Fatal("a failing writer reported success")
	}
	if len(steps) != 3 {
		t.Fatalf("got %d steps back from a failure; want all of them", len(steps))
	}
	for _, s := range steps {
		if s.Done || s.Error == "" {
			t.Errorf("step %q did not report its failure", s.Description)
		}
	}
}

// Moving the way out takes the old address with it; handing it back, or
// dropping to no static address, does not. Config.RemoveUplink has why the
// second half is the safe one.
func TestRetiringIsThePreviousAddressOnlyWhenTheUplinkMoves(t *testing.T) {
	stored := Config{Uplink: &Uplink{Interface: "ens19", IPv4: &UplinkIPv4{
		Address: netip.MustParsePrefix("192.168.1.3/24"),
		Gateway: netip.MustParseAddr("192.168.1.1"),
	}}}
	moved := stored.Clone()
	moved.Uplink.Interface = "ens18"
	moved.Uplink.IPv4.Address = netip.MustParsePrefix("192.168.1.2/24")
	renumbered := stored.Clone()
	renumbered.Uplink.IPv4.Address = netip.MustParsePrefix("192.168.1.4/24")
	sameAddressElsewhere := stored.Clone()
	sameAddressElsewhere.Uplink.Interface = "ens18"
	regatewayed := stored.Clone()
	regatewayed.Uplink.IPv4.Gateway = netip.MustParseAddr("192.168.1.254")
	noStatic := Config{Uplink: &Uplink{Interface: "ens18"}}

	old := netip.MustParsePrefix("192.168.1.3/24")
	for _, tc := range []struct {
		name    string
		desired Config
		from    string
		retire  netip.Prefix
	}{
		{"moved", moved, "ens19", old},
		{"renumbered in place", renumbered, "ens19", old},
		{"same address, other interface", sameAddressElsewhere, "ens19", old},
		{"only the gateway changed", regatewayed, "", netip.Prefix{}},
		{"unchanged", stored, "", netip.Prefix{}},
		{"handed back", Config{}, "", netip.Prefix{}},
		{"no static address", noStatic, "", netip.Prefix{}},
	} {
		from, retire := Retiring(stored, tc.desired)
		if from != tc.from || retire != tc.retire {
			t.Errorf("%s: Retiring = (%q, %v), want (%q, %v)", tc.name, from, retire, tc.from, tc.retire)
		}
	}
}

// The old address comes off after everything that replaces it, so a box that
// fails halfway still has the way out it started with.
func TestTheWriterRetiresTheOldAddressLast(t *testing.T) {
	w := &RecordingWriter{}
	d := desiredUplink()
	d.Retire, d.RetireFrom = netip.MustParsePrefix("192.168.1.3/24"), "ens19"
	if _, err := w.Apply(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	last := w.Steps[len(w.Steps)-1].Description
	if last != "remove 192.168.1.3/24 from ens19" {
		t.Errorf("last step = %q, want the retirement", last)
	}
	if (Desired{Interface: "ens18", Retire: d.Retire, RetireFrom: "ens19"}).Empty() {
		t.Error("a retirement alone read as nothing to do")
	}
}
