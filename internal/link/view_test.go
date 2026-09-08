package link

import (
	"net/netip"
	"testing"
)

func TestHostRange(t *testing.T) {
	for _, tc := range []struct {
		prefix     string
		start, end string
		ok         bool
	}{
		{prefix: "192.168.1.2/24", start: "192.168.1.1", end: "192.168.1.254", ok: true},
		{prefix: "192.168.1.2/16", start: "192.168.0.1", end: "192.168.255.254", ok: true},
		{prefix: "10.0.4.1/20", start: "10.0.0.1", end: "10.0.15.254", ok: true},
		{prefix: "10.0.0.1/30", start: "10.0.0.1", end: "10.0.0.2", ok: true},

		// No assignable addresses at all, which is a real answer rather than a
		// range of zero.
		{prefix: "10.0.0.1/31"},
		{prefix: "10.0.0.1/32"},
		{prefix: "fd00::1/64"},
	} {
		t.Run(tc.prefix, func(t *testing.T) {
			prefix := netip.MustParsePrefix(tc.prefix)
			start, end, ok := hostRange(prefix)
			if ok != tc.ok {
				t.Fatalf("hostRange(%s) ok = %v, want %v", tc.prefix, ok, tc.ok)
			}
			if !ok {
				return
			}
			if start.String() != tc.start || end.String() != tc.end {
				t.Errorf("hostRange(%s) = %s-%s, want %s-%s",
					tc.prefix, start, end, tc.start, tc.end)
			}
		})
	}
}

// The suggestion has to exclude the three addresses a range must not contain,
// or the form would prefill something dhcp then refuses — which reads as the
// router contradicting itself.
func TestSuggestRangeAvoidsTheRoutersOwnAddress(t *testing.T) {
	for _, tc := range []struct {
		prefix     string
		start, end string
	}{
		// The deployment this was built for: the box sits at .2 on an existing
		// LAN whose gateway is .1, so the range starts above it.
		{prefix: "192.168.1.2/24", start: "192.168.1.3", end: "192.168.1.254"},

		// The box is the gateway.
		{prefix: "192.168.1.1/24", start: "192.168.1.2", end: "192.168.1.254"},

		// Mid-subnet: the larger side wins.
		{prefix: "192.168.1.100/24", start: "192.168.1.101", end: "192.168.1.254"},
		{prefix: "192.168.1.200/24", start: "192.168.1.1", end: "192.168.1.199"},
	} {
		t.Run(tc.prefix, func(t *testing.T) {
			start, end, ok := suggestRange(netip.MustParsePrefix(tc.prefix))
			if !ok {
				t.Fatalf("suggestRange(%s) refused", tc.prefix)
			}
			if start.String() != tc.start || end.String() != tc.end {
				t.Errorf("suggestRange(%s) = %s-%s, want %s-%s",
					tc.prefix, start, end, tc.start, tc.end)
			}

			// Whatever it picked, the router's own address is not in it.
			self := netip.MustParsePrefix(tc.prefix).Addr()
			if self.Compare(start) >= 0 && self.Compare(end) <= 0 {
				t.Errorf("suggested %s-%s contains the interface's own address %s", start, end, self)
			}
		})
	}
}

// A /16 would otherwise suggest sixty-five thousand addresses: legal, and an
// absurd default.
func TestSuggestRangeIsCapped(t *testing.T) {
	start, end, ok := suggestRange(netip.MustParsePrefix("10.0.0.1/8"))
	if !ok {
		t.Fatal("suggestRange refused a /8")
	}
	if n := runLen(start, end); n != maxSuggested {
		t.Errorf("suggested %s-%s is %d addresses, want the cap of %d", start, end, n, maxSuggested)
	}
}

func TestSuggestRangeRefusesWhenThereIsNothingToSuggest(t *testing.T) {
	if _, _, ok := suggestRange(netip.MustParsePrefix("10.0.0.1/31")); ok {
		t.Error("suggestRange offered a range on a /31")
	}
}

func TestViewInterfaceCarriesTheSubnetHalves(t *testing.T) {
	observed := testInterfaces(t)
	info := find(t, Join(Config{Adopted: []string{"lan0"}}, observed), "lan0")

	v := viewInterface(info, observedByName(observed))
	switch {
	case v.Address != "192.168.1.2":
		t.Errorf("Address = %q, want 192.168.1.2", v.Address)
	case v.Subnet != "192.168.1.0/24":
		t.Errorf("Subnet = %q, want 192.168.1.0/24", v.Subnet)
	case v.SuggestedStart != "192.168.1.3" || v.SuggestedEnd != "192.168.1.254":
		t.Errorf("suggested %s-%s, want 192.168.1.3-192.168.1.254", v.SuggestedStart, v.SuggestedEnd)
	case v.MAC != "aa:bb:cc:dd:ee:ff":
		t.Errorf("MAC = %q", v.MAC)
	case !v.Running:
		t.Error("Running is false for an interface with a carrier")
	}
}

// An interface with no IPv4 address is exactly the one no range can be served
// on, so the fields a form would prefill from are absent rather than guessed.
func TestViewInterfaceOmitsTheHintWithoutAnAddress(t *testing.T) {
	observed := testInterfaces(t)
	info := find(t, Join(Config{}, observed), "lan1")

	v := viewInterface(info, observedByName(observed))
	if v.Address != "" || v.Subnet != "" || v.SuggestedStart != "" {
		t.Errorf("view = %+v, want no address, subnet or suggestion", v)
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
