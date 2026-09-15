package link

import (
	"net/netip"
	"strings"
	"testing"
)

// The subnet arithmetic these used to cover now lives in internal/core, where
// internal/dhcp can reach it too; see internal/core/subnet_test.go. What is left
// here is what this module actually decides.

func TestViewInterfaceCarriesTheSubnetHalves(t *testing.T) {
	observed := testInterfaces(t)
	cfg := Config{Adopted: []string{"lan0"}}
	info := find(t, Join(cfg, observed), "lan0")

	v := viewInterface(info, observedByName(observed), cfg)
	switch {
	case v.Address != "192.168.1.2":
		t.Errorf("Address = %q, want 192.168.1.2", v.Address)
	case v.Subnet != "192.168.1.0/24":
		t.Errorf("Subnet = %q, want 192.168.1.0/24", v.Subnet)
	case v.MAC != "aa:bb:cc:dd:ee:ff":
		t.Errorf("MAC = %q", v.MAC)
	case !v.Running:
		t.Error("Running is false for an interface with a carrier")
	}
}

// The interface row names the network it carries, which is what lets it answer
// "what is this NIC for" with something other than the address it happens to
// hold at the moment.
func TestViewInterfaceNamesItsNetwork(t *testing.T) {
	observed := testInterfaces(t)
	cfg := Config{
		Adopted: []string{"lan0"},
		Groups:  []Group{{Name: "lan", Members: []string{"lan0"}}},
	}

	withGroup := viewInterface(find(t, Join(cfg, observed), "lan0"), observedByName(observed), cfg)
	if withGroup.Group != "lan" {
		t.Errorf("Group = %q, want lan", withGroup.Group)
	}
	without := viewInterface(find(t, Join(cfg, observed), "lan1"), observedByName(observed), cfg)
	if without.Group != "" {
		t.Errorf("Group = %q for an interface in no network, want empty", without.Group)
	}
}

// The derived range is published with the network rather than left to each
// client, so that `dhcp`, the form and the CLI cannot end up with three
// opinions about where the static block ends.
func TestViewGroupPublishesTheDerivedRange(t *testing.T) {
	observed := testInterfaces(t)
	g := Group{
		Name:    "lan",
		Members: []string{"lan0"},
		IPv4:    &GroupIPv4{Subnet: netip.MustParsePrefix("172.16.1.0/24")},
	}

	v := viewGroup(g, observedByName(observed))
	switch {
	case v.Subnet != "172.16.1.0/24":
		t.Errorf("Subnet = %q", v.Subnet)
	case v.Router != "172.16.1.1":
		t.Errorf("Router = %q, want the derived 172.16.1.1", v.Router)
	case v.RouterExplicit:
		t.Error("RouterExplicit is true for a derived address")
	case v.SuggestedStart != "172.16.1.100" || v.SuggestedEnd != "172.16.1.254":
		t.Errorf("derived range %s-%s, want 172.16.1.100-172.16.1.254", v.SuggestedStart, v.SuggestedEnd)
	}
}

// A network whose member is not on this machine is a row that still has to be
// shown — it is the typo case and the not-plugged-in-yet case — but it must not
// look like one that is working.
func TestViewGroupReportsAnAbsentMember(t *testing.T) {
	observed := testInterfaces(t)
	g := Group{Name: "lan", Members: []string{"nosuch0"}}

	if v := viewGroup(g, observedByName(observed)); v.Present {
		t.Error("Present is true for a network whose member does not exist")
	}
}

func TestBuildPlanNamesAdoptAndRelease(t *testing.T) {
	stored := Config{Adopted: []string{"lan0"}}
	desired := Config{Adopted: []string{"lan1"}}

	plan := buildPlan(stored, desired, testInterfaces(t))
	if plan.Empty {
		t.Fatal("plan is empty for a change of adopted interface")
	}
	if plan.Impact != impactNone {
		t.Errorf("Impact = %q, want %q; adoption touches nothing on the box", plan.Impact, impactNone)
	}

	kinds := map[string]string{}
	for _, c := range plan.Changes {
		kinds[nameOf(c.Path)] = c.Kind
	}
	if kinds["lan1"] != kindCreate {
		t.Errorf("lan1 = %q, want %q", kinds["lan1"], kindCreate)
	}
	if kinds["lan0"] != kindDelete {
		t.Errorf("lan0 = %q, want %q", kinds["lan0"], kindDelete)
	}
}

func TestBuildPlanIsEmptyForNoChange(t *testing.T) {
	cfg := Config{Adopted: []string{"lan0"}}
	if plan := buildPlan(cfg, cfg, testInterfaces(t)); !plan.Empty {
		t.Errorf("plan = %+v, want empty", plan)
	}
}

// Creating a network on an interface that already holds a different address has
// to say, in the plan, that the old address is going away. This is the change
// that can take the operator's own session with it, and §5.5's guard is not
// built — the plan is the only warning there is.
func TestBuildPlanShowsTheAddressBeingRemoved(t *testing.T) {
	stored := Config{Adopted: []string{"lan0"}}
	desired := Config{
		Adopted: []string{"lan0"},
		Groups: []Group{{
			Name:    "lan",
			Members: []string{"lan0"},
			IPv4:    &GroupIPv4{Subnet: netip.MustParsePrefix("172.16.1.0/24")},
		}},
	}

	plan := buildPlan(stored, desired, testInterfaces(t))
	if plan.Impact != impactDisruptive {
		t.Errorf("Impact = %q, want %q; lan0's 192.168.1.2/24 is being removed", plan.Impact, impactDisruptive)
	}

	var kernel string
	for _, c := range plan.Changes {
		if strings.HasPrefix(c.Path, "interfaces[") {
			kernel = c.Diff
		}
	}
	switch {
	case kernel == "":
		t.Fatalf("no kernel change in %+v", plan.Changes)
	case !strings.Contains(kernel, "- ip addr del 192.168.1.2/24 dev lan0"):
		t.Errorf("diff does not name the address being removed:\n%s", kernel)
	case !strings.Contains(kernel, "+ ip addr add 172.16.1.1/24 dev lan0"):
		t.Errorf("diff does not name the address being added:\n%s", kernel)
	}
}

// Adding a network to an interface that has no address takes nothing away, so
// it must not be classified the same as a renumber — an operator who is warned
// about everything stops reading the warnings.
func TestBuildPlanDoesNotCallAFirstAddressDisruptive(t *testing.T) {
	cfg := Config{
		Adopted: []string{"lan1"},
		Groups: []Group{{
			Name:    "lan",
			Members: []string{"lan1"},
			IPv4:    &GroupIPv4{Subnet: netip.MustParsePrefix("172.16.1.0/24")},
		}},
	}

	plan := buildPlan(Config{Adopted: []string{"lan1"}}, cfg, testInterfaces(t))
	if plan.Impact != impactRestart {
		t.Errorf("Impact = %q, want %q; lan1 has no address to lose", plan.Impact, impactRestart)
	}
}

// Re-applying an unchanged config is not a no-op once the kernel is involved.
// Somebody who ran `ip addr del` by hand has to show up here as work to do,
// which is what §5.4 means by drift being free.
func TestBuildPlanSeesDriftAgainstAnUnchangedConfig(t *testing.T) {
	cfg := Config{
		Adopted: []string{"lan1"},
		Groups: []Group{{
			Name:    "lan",
			Members: []string{"lan1"},
			IPv4:    &GroupIPv4{Subnet: netip.MustParsePrefix("172.16.1.0/24")},
		}},
	}

	// lan1 carries no address in the fixture, so intent and reality disagree.
	plan := buildPlan(cfg, cfg, testInterfaces(t))
	if plan.Empty {
		t.Error("plan is empty although the kernel does not have the network's address")
	}
}
