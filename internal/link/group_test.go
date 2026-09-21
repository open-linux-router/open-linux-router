package link

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The network object, which is the half of this module that reaches the kernel.
//
// Almost all of it is checkable without one: a subnet, a router address inside
// it, and a member that has been adopted are answerable from the document
// alone. That is the property the whole change exists to create — `dhcp` gets
// to validate a range against stored intent instead of against an observation,
// and this is where that intent becomes checkable.

func group(name, subnet string, members ...string) Group {
	g := Group{Name: name, Members: members}
	if subnet != "" {
		g.IPv4 = &GroupIPv4{Subnet: netip.MustParsePrefix(subnet)}
	}
	return g
}

func validateGroup(t *testing.T, cfg Config) Result {
	t.Helper()
	cfg.Normalize()
	return Validate(cfg, testInterfaces(t))
}

func firstError(r Result) string {
	if len(r.Errors) == 0 {
		return ""
	}
	return r.Errors[0].String()
}

// A subnet typed as 172.16.1.5/24 is the same network as 172.16.1.0/24. Storing
// both spellings would make every downstream byte comparison call the
// difference drift.
func TestNormalizeMasksTheSubnet(t *testing.T) {
	cfg := Config{
		Adopted: []string{"lan0"},
		Groups:  []Group{group("lan", "172.16.1.5/24", "lan0")},
	}
	cfg.Normalize()

	if got := cfg.Groups[0].IPv4.Subnet.String(); got != "172.16.1.0/24" {
		t.Errorf("subnet = %s, want 172.16.1.0/24", got)
	}
}

// A form that helpfully fills the router field in with the default must not
// thereby turn a default into a pin — otherwise changing the subnet later would
// leave the old router address behind.
func TestNormalizeDropsARouterThatMatchesTheDefault(t *testing.T) {
	derived := netip.MustParseAddr("172.16.1.1")
	pinned := netip.MustParseAddr("172.16.2.254")

	cfg := Config{
		Adopted: []string{"lan0", "lan1"},
		Groups: []Group{
			{Name: "a", Members: []string{"lan0"}, IPv4: &GroupIPv4{
				Subnet: netip.MustParsePrefix("172.16.1.0/24"), Router: &derived}},
			{Name: "b", Members: []string{"lan1"}, IPv4: &GroupIPv4{
				Subnet: netip.MustParsePrefix("172.16.2.0/24"), Router: &pinned}},
		},
	}
	cfg.Normalize()

	a, _ := cfg.Group("a")
	if a.IPv4.Router != nil {
		t.Errorf("a pinned the derived address %s instead of leaving it derived", *a.IPv4.Router)
	}
	if a.IPv4.RouterAddr().String() != "172.16.1.1" {
		t.Errorf("a resolves to %s, want 172.16.1.1", a.IPv4.RouterAddr())
	}

	b, _ := cfg.Group("b")
	if b.IPv4.Router == nil || b.IPv4.Router.String() != "172.16.2.254" {
		t.Errorf("b lost its explicitly pinned router address")
	}
}

func TestCloneDoesNotShareGroupState(t *testing.T) {
	pinned := netip.MustParseAddr("172.16.1.9")
	original := Config{
		Adopted: []string{"lan0"},
		Groups: []Group{{Name: "lan", Members: []string{"lan0"}, IPv4: &GroupIPv4{
			Subnet: netip.MustParsePrefix("172.16.1.0/24"), Router: &pinned}}},
	}

	clone := original.Clone()
	clone.Groups[0].Members[0] = "other0"
	*clone.Groups[0].IPv4.Router = netip.MustParseAddr("172.16.1.99")

	if original.Groups[0].Members[0] != "lan0" {
		t.Error("editing the clone's members changed the original")
	}
	if original.Groups[0].IPv4.Router.String() != "172.16.1.9" {
		t.Error("editing the clone's router address changed the original")
	}
}

// design.md §3.4 is adopt-only. Writing an address onto an interface nobody
// handed us is a larger version of the exact surprise that rule forbids.
func TestValidateRefusesANetworkOnAnUnadoptedInterface(t *testing.T) {
	res := validateGroup(t, Config{Groups: []Group{group("lan", "172.16.1.0/24", "lan0")}})
	if res.OK() {
		t.Fatal("accepted a network on an interface nobody adopted")
	}
	if !strings.Contains(firstError(res), "olr adopt lan0") {
		t.Errorf("error does not name the fix: %s", firstError(res))
	}
}

// The schema allows several members because §4.4 says a group has bridge
// members, but nothing creates a bridge yet — and two interfaces in one subnet
// without one is a broken network rather than a configured one.
func TestValidateRefusesMultipleMembersUntilBridgingExists(t *testing.T) {
	res := validateGroup(t, Config{
		Adopted: []string{"lan0", "lan1"},
		Groups:  []Group{group("lan", "172.16.1.0/24", "lan0", "lan1")},
	})
	if res.OK() {
		t.Fatal("accepted a two-member network")
	}
	if !strings.Contains(firstError(res), "bridging") {
		t.Errorf("error does not explain why: %s", firstError(res))
	}
}

func TestValidateRefusesAnInterfaceInTwoNetworks(t *testing.T) {
	res := validateGroup(t, Config{
		Adopted: []string{"lan0"},
		Groups: []Group{
			group("a", "172.16.1.0/24", "lan0"),
			group("b", "172.16.2.0/24", "lan0"),
		},
	})
	if res.OK() {
		t.Fatal("accepted one interface carrying two networks")
	}
}

// Overlapping subnets mean the routing table has two entries that match, and
// which one wins is not something any surface above here could explain.
func TestValidateRefusesOverlappingSubnets(t *testing.T) {
	res := validateGroup(t, Config{
		Adopted: []string{"lan0", "lan1"},
		Groups: []Group{
			group("a", "10.0.0.0/8", "lan0"),
			group("b", "10.1.2.0/24", "lan1"),
		},
	})
	if res.OK() {
		t.Fatal("accepted two networks whose subnets overlap")
	}
	if !strings.Contains(firstError(res), "overlaps") {
		t.Errorf("error does not say what is wrong: %s", firstError(res))
	}
}

func TestValidateRefusesARouterOutsideItsSubnet(t *testing.T) {
	for _, router := range []string{
		"192.168.1.1",  // a different subnet entirely
		"172.16.1.0",   // the network address
		"172.16.1.255", // the broadcast address
	} {
		t.Run(router, func(t *testing.T) {
			addr := netip.MustParseAddr(router)
			g := group("lan", "172.16.1.0/24", "lan0")
			g.IPv4.Router = &addr

			res := validateGroup(t, Config{Adopted: []string{"lan0"}, Groups: []Group{g}})
			if res.OK() {
				t.Fatalf("accepted %s as the router address of 172.16.1.0/24", router)
			}
		})
	}
}

// /31 and /32 have no host addresses, so there is no router address to assign
// and nothing for DHCP to hand out.
func TestValidateRefusesASubnetWithNoHostAddresses(t *testing.T) {
	res := validateGroup(t, Config{
		Adopted: []string{"lan0"},
		Groups:  []Group{group("lan", "172.16.1.0/31", "lan0")},
	})
	if res.OK() {
		t.Fatal("accepted a /31 as a network")
	}
}

// Lowercase only, because `guest` and `Guest` being two networks is a trap
// nobody would find funny at three in the morning.
func TestValidateRefusesAnUppercaseName(t *testing.T) {
	res := validateGroup(t, Config{
		Adopted: []string{"lan0"},
		Groups:  []Group{group("Guest", "172.16.1.0/24", "lan0")},
	})
	if res.OK() {
		t.Fatal("accepted an uppercase network name")
	}
}

// A network serving only RA is legitimate — it is what the v4/v6 split makes
// expressible — but it is far more often a half-finished config, so it warns
// rather than passing silently.
func TestValidateWarnsButAcceptsANetworkWithNoIPv4(t *testing.T) {
	res := validateGroup(t, Config{
		Adopted: []string{"lan0"},
		Groups:  []Group{group("lan", "", "lan0")},
	})
	if !res.OK() {
		t.Fatalf("refused a network with no IPv4: %s", firstError(res))
	}
	if len(res.Warnings) == 0 {
		t.Error("accepted it silently; a network that serves no addresses is worth remarking on")
	}
}

// The warning an operator meets on a fresh box used to end in "give it one"
// with nowhere to do that. Now it names the command.
func TestValidatePointsAnAddresslessInterfaceAtNetAdd(t *testing.T) {
	// Up, so the "it is down" branch does not answer first, and with no address
	// and no network — which is exactly what a freshly adopted NIC looks like.
	observed := testInterfaces(t)
	for i := range observed {
		if observed[i].Name == "lan1" {
			observed[i].Up = true
		}
	}
	res := Validate(Config{Adopted: []string{"lan1"}}, observed)

	var found bool
	for _, w := range res.Warnings {
		if strings.Contains(w.Message, "olr net add") {
			found = true
		}
	}
	if !found {
		t.Errorf("no warning names `olr net add`: %+v", res.Warnings)
	}
}

// --- the kernel plan --------------------------------------------------------

// The ownership claim, stated as a test: an interface in a network has its IPv4
// addressing owned by olr, so an address the network does not call for is
// removed. This is the line that can drop the operator's own session, so it had
// better be exactly what the comment on PlanAddrs says it is.
func TestPlanAddrsReplacesAForeignAddressOnAMember(t *testing.T) {
	cfg := Config{
		Adopted: []string{"lan0"},
		Groups:  []Group{group("lan", "172.16.1.0/24", "lan0")},
	}

	plans := PlanAddrs(cfg, testInterfaces(t))
	if len(plans) != 1 {
		t.Fatalf("got %d plans, want 1: %+v", len(plans), plans)
	}
	p := plans[0]
	if len(p.Add) != 1 || p.Add[0].String() != "172.16.1.1/24" {
		t.Errorf("Add = %v, want [172.16.1.1/24]", p.Add)
	}
	if len(p.Remove) != 1 || p.Remove[0].String() != "192.168.1.2/24" {
		t.Errorf("Remove = %v, want [192.168.1.2/24]", p.Remove)
	}
}

// IPv6 is not touched at all: the prefix comes from delegation and dnsmasq
// derives it from the interface, so olr has no v6 address to write and no
// business removing one it did not put there.
func TestPlanAddrsLeavesIPv6Alone(t *testing.T) {
	observed := testInterfaces(t)
	for i := range observed {
		if observed[i].Name == "lan0" {
			observed[i].Prefixes = append(observed[i].Prefixes,
				netip.MustParsePrefix("2001:db8::1/64"))
		}
	}

	cfg := Config{
		Adopted: []string{"lan0"},
		Groups:  []Group{group("lan", "172.16.1.0/24", "lan0")},
	}
	for _, p := range PlanAddrs(cfg, observed) {
		for _, r := range p.Remove {
			if r.Addr().Is6() {
				t.Errorf("planned to remove the IPv6 address %s", r)
			}
		}
	}
}

func TestPlanAddrsIsEmptyWhenTheKernelAlreadyAgrees(t *testing.T) {
	observed := testInterfaces(t)
	for i := range observed {
		if observed[i].Name == "lan1" {
			observed[i].Up = true
			observed[i].Prefixes = []netip.Prefix{netip.MustParsePrefix("172.16.1.1/24")}
		}
	}

	cfg := Config{
		Adopted: []string{"lan1"},
		Groups:  []Group{group("lan", "172.16.1.0/24", "lan1")},
	}
	if plans := PlanAddrs(cfg, observed); len(plans) != 0 {
		t.Errorf("planned %+v against a kernel that already matches", plans)
	}
}

// An interface that does not exist gets no steps. Inventing them would produce
// a plan whose every line is going to fail.
func TestPlanAddrsSkipsAnAbsentMember(t *testing.T) {
	cfg := Config{
		Adopted: []string{"nosuch0"},
		Groups:  []Group{group("lan", "172.16.1.0/24", "nosuch0")},
	}
	if plans := PlanAddrs(cfg, testInterfaces(t)); len(plans) != 0 {
		t.Errorf("planned %+v for an interface this machine does not have", plans)
	}
}

// Adoption still touches nothing on the box, which is §7's promise that
// installing and adopting change nothing. A config with no networks must not
// even reach the writer — on a machine with no Linux kernel it would fail, for
// a reason that has nothing to do with what was asked.
func TestApplyDoesNotTouchTheKernelWithoutNetworks(t *testing.T) {
	w := &RecordingWriter{}
	a := Applier{Store: storeWith(t, ""), Source: staticSource(testInterfaces(t)...), Writer: w}

	if _, err := a.Apply(t.Context(), Config{Adopted: []string{"lan0"}}); err != nil {
		t.Fatalf("adopting failed: %v", err)
	}
	if len(w.Applied) != 0 {
		t.Errorf("the writer was called with %+v for an adoption-only change", w.Applied)
	}
}

func TestApplyProgramsTheNetworksAddress(t *testing.T) {
	w := &RecordingWriter{}
	a := Applier{Store: storeWith(t, ""), Source: staticSource(testInterfaces(t)...), Writer: w}

	cfg := Config{
		Adopted: []string{"lan0"},
		Groups:  []Group{group("lan", "172.16.1.0/24", "lan0")},
	}
	if _, err := a.Apply(t.Context(), cfg); err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if len(w.Applied) != 1 {
		t.Fatalf("writer saw %+v, want one interface", w.Applied)
	}
	d := w.Applied[0]
	if d.Interface != "lan0" || len(d.Addrs) != 1 || d.Addrs[0].String() != "172.16.1.1/24" {
		t.Errorf("writer saw %+v, want lan0 with 172.16.1.1/24", d)
	}
	if !d.Up {
		t.Error("the member was not asked to be brought up")
	}
}

// Store first, program second: if only one of the two survives it must be the
// stored intent. A box whose config says 172.16.1.1 and whose kernel says
// otherwise is drift — visible on every surface and fixable by re-applying. The
// reverse is a box nobody can explain.
func TestApplyStoresIntentEvenWhenTheKernelRefuses(t *testing.T) {
	w := &RecordingWriter{Err: ErrUnsupported}
	a := Applier{Store: storeWith(t, ""), Source: staticSource(testInterfaces(t)...), Writer: w}

	cfg := Config{
		Adopted: []string{"lan0"},
		Groups:  []Group{group("lan", "172.16.1.0/24", "lan0")},
	}
	if _, err := a.Apply(t.Context(), cfg); err == nil {
		t.Fatal("apply reported success although the kernel refused")
	}

	stored, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := stored.Group("lan"); !ok {
		t.Error("the network was not stored, so the failure left nothing to retry or inspect")
	}
}

// The bug this exists to stop: an address is kernel state and the kernel
// forgets it, so a box came back from a reboot with its configuration intact
// and its router address gone — dnsmasq holding no address inside the range it
// serves, and `dns` deriving allow_from from an interface no longer carrying
// the LAN.
func TestRestorePutsTheAddressBackAfterAReboot(t *testing.T) {
	w := &RecordingWriter{}
	a := Applier{Store: storeWith(t, ""), Source: staticSource(testInterfaces(t)...), Writer: w}

	cfg := Config{
		Adopted: []string{"lan0"},
		Groups:  []Group{group("lan", "172.16.1.0/24", "lan0")},
	}
	if _, err := a.Apply(t.Context(), cfg); err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	// The reboot: the kernel forgets, the document does not.
	w.Applied = nil

	if _, err := a.Restore(t.Context()); err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	if len(w.Applied) != 1 {
		t.Fatalf("writer saw %+v, want one interface", w.Applied)
	}
	d := w.Applied[0]
	if d.Interface != "lan0" || len(d.Addrs) != 1 || d.Addrs[0].String() != "172.16.1.1/24" {
		t.Errorf("writer saw %+v, want lan0 with 172.16.1.1/24", d)
	}
	if !d.Up {
		t.Error("the member was not asked to be brought up")
	}
}

// Startup may not enforce PlanAddrs' ownership claim. An operator applying a
// change has said what an interface's addressing is; startup has been told
// nothing and is racing every other address source on the box. Until `link`
// grows the WAN/LAN split PlanAddrs already assumes, a one-armed router
// carries its uplink address on a group member — and a restore that removed it
// would take the box off the network on every boot, with the only way back
// being physical.
func TestRestoreNeverTakesAnAddressOffTheBox(t *testing.T) {
	w := &RecordingWriter{}
	a := Applier{Store: storeWith(t, ""), Source: staticSource(testInterfaces(t)...), Writer: w}

	cfg := Config{
		Adopted: []string{"lan0"},
		Groups:  []Group{group("lan", "172.16.1.0/24", "lan0")},
	}
	if _, err := a.Apply(t.Context(), cfg); err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	w.Applied = nil

	if _, err := a.Restore(t.Context()); err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	if len(w.Applied) != 1 {
		t.Fatalf("writer saw %+v, want one interface", w.Applied)
	}
	if !w.Applied[0].AddOnly {
		t.Error("a boot-time restore must add without removing")
	}

	// The operator's path keeps the claim. If this ever flips, the ownership
	// PlanAddrs documents has quietly stopped being enforced anywhere.
	w.Applied = nil
	if _, err := a.Apply(t.Context(), cfg); err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if w.Applied[0].AddOnly {
		t.Error("an operator's apply still owns the interface's addressing")
	}
}

// §7's promise survives the new call: a box with no networks is one olr has
// not been asked to address, and startup must not reach a kernel on it.
func TestRestoreTouchesNothingWithoutNetworks(t *testing.T) {
	w := &RecordingWriter{}
	a := Applier{Store: storeWith(t, ""), Source: staticSource(testInterfaces(t)...), Writer: w}

	if _, err := a.Apply(t.Context(), Config{Adopted: []string{"lan0"}}); err != nil {
		t.Fatalf("adopting failed: %v", err)
	}
	w.Applied = nil

	if _, err := a.Restore(t.Context()); err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	if len(w.Applied) != 0 {
		t.Errorf("the writer was called with %+v on a box with no networks", w.Applied)
	}
}

// Restore is not Apply: the operator said nothing, so nothing is stored. A
// boot that rewrote olr.json would put a modification time on a file nobody
// edited, which is the one thing an operator reads to answer "when did this
// box last change?".
func TestRestoreDoesNotRewriteTheDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "olr.json")
	store := core.NewStore(path, ModuleName, "dhcp")
	w := &RecordingWriter{}
	a := Applier{Store: store, Source: staticSource(testInterfaces(t)...), Writer: w}

	cfg := Config{
		Adopted: []string{"lan0"},
		Groups:  []Group{group("lan", "172.16.1.0/24", "lan0")},
	}
	if _, err := a.Apply(t.Context(), cfg); err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	// Dated well into the past, so "unchanged" is unambiguous rather than a
	// question about filesystem timestamp resolution.
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	if _, err := a.Restore(t.Context()); err != nil {
		t.Fatalf("restore failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old) {
		t.Errorf("restore rewrote the configuration document (mtime moved to %v)", info.ModTime())
	}
}
