package devices

import (
	"net/netip"
	"testing"
	"time"
)

func lease(mac, ip, hostname string, active bool) Sighting {
	return Sighting{MAC: mac, IP: ip, Hostname: hostname, Source: SourceDHCPLease, Active: active}
}

func arp(mac, ip string, active bool) Sighting {
	return Sighting{MAC: mac, IP: ip, Source: SourceARP, Active: active}
}

func find(t *testing.T, list []Resolved, mac string) Resolved {
	t.Helper()
	for _, r := range list {
		if r.MAC == mac {
			return r
		}
	}
	t.Fatalf("%s is not in the list", mac)
	return Resolved{}
}

// The union of three key sets. Dropping any one loses a real case: the printer
// named last year that is powered off, the guest phone nobody has named, and
// the reservation made for a device that has not connected yet.
func TestBuildUnionsIdentityPresenceAndReservations(t *testing.T) {
	cfg := Config{Devices: []Device{{MAC: "aa:aa:aa:aa:aa:aa", Name: "Named but absent"}}}
	sightings := []Sighting{lease("bb:bb:bb:bb:bb:bb", "192.168.1.50", "guest-phone", true)}
	fixed := map[string]string{"cc:cc:cc:cc:cc:cc": "192.168.1.9"}

	list, problems := Build(cfg, sightings, fixed, nil)
	if len(problems) != 0 {
		t.Errorf("unexpected problems: %s", problemStrings(problems))
	}
	if len(list) != 3 {
		t.Fatalf("got %d devices, want 3:\n%+v", len(list), list)
	}

	stored := find(t, list, "aa:aa:aa:aa:aa:aa")
	if !stored.Stored {
		t.Error("the named device should be marked stored")
	}
	if stored.Presence != nil {
		t.Error("a device nobody saw should have no presence")
	}
	if stored.Online() {
		t.Error("a device nobody saw is not online")
	}

	seen := find(t, list, "bb:bb:bb:bb:bb:bb")
	if seen.Stored {
		t.Error("a device nobody described should not be marked stored")
	}
	if !seen.Online() {
		t.Error("an active lease means online")
	}

	reserved := find(t, list, "cc:cc:cc:cc:cc:cc")
	if reserved.FixedIP != "192.168.1.9" {
		t.Errorf("FixedIP = %q, want the reservation", reserved.FixedIP)
	}
}

// The rule from icon-style-spec.md: a device whose picture the operator
// corrected must not have it silently changed back by the next fingerprint
// update.
func TestBuildOperatorCategoryBeatsDetection(t *testing.T) {
	// The hostname says printer; the operator says it is a server.
	cfg := Config{Devices: []Device{{
		MAC:      "aa:aa:aa:aa:aa:aa",
		Category: CategoryServer,
	}}}
	sightings := []Sighting{lease("aa:aa:aa:aa:aa:aa", "192.168.1.20", "laserjet-m140we", true)}

	list, _ := Build(cfg, sightings, nil, nil)
	got := find(t, list, "aa:aa:aa:aa:aa:aa")

	if got.Category != CategoryServer {
		t.Errorf("Category = %q, want the operator's answer %q", got.Category, CategoryServer)
	}
	if got.CategoryOrigin != OriginOperator {
		t.Errorf("CategoryOrigin = %q, want %q", got.CategoryOrigin, OriginOperator)
	}
	// Detection is still reported, so the UI can show that this is an override.
	if got.Detected.Category != CategoryPrinter {
		t.Errorf("Detected.Category = %q, want the guess preserved %q",
			got.Detected.Category, CategoryPrinter)
	}
}

func TestBuildFallsBackToDetection(t *testing.T) {
	sightings := []Sighting{lease("aa:aa:aa:aa:aa:aa", "192.168.1.20", "Toms-iPhone", true)}

	list, _ := Build(Config{}, sightings, nil, nil)
	got := find(t, list, "aa:aa:aa:aa:aa:aa")

	if got.Category != CategoryPhone {
		t.Errorf("Category = %q, want %q", got.Category, CategoryPhone)
	}
	if got.CategoryOrigin != OriginDetected {
		t.Errorf("CategoryOrigin = %q, want %q", got.CategoryOrigin, OriginDetected)
	}
}

func TestBuildFallsBackToUnknown(t *testing.T) {
	sightings := []Sighting{lease("aa:aa:aa:aa:aa:aa", "192.168.1.20", "some-box", true)}

	list, _ := Build(Config{}, sightings, nil, nil)
	got := find(t, list, "aa:aa:aa:aa:aa:aa")

	if got.Category != CategoryUnknown {
		t.Errorf("Category = %q, want %q", got.Category, CategoryUnknown)
	}
	if got.CategoryOrigin != OriginNone {
		t.Errorf("CategoryOrigin = %q, want it to admit nothing supplied a value", got.CategoryOrigin)
	}
}

// A stored name is intent; a hostname is what the client calls itself. Both are
// shown, but they are never conflated.
func TestBuildNamePrecedence(t *testing.T) {
	cfg := Config{Devices: []Device{{MAC: "aa:aa:aa:aa:aa:aa", Name: "Study laptop"}}}
	sightings := []Sighting{lease("aa:aa:aa:aa:aa:aa", "192.168.1.20", "toms-macbook", true)}

	list, _ := Build(cfg, sightings, nil, nil)
	got := find(t, list, "aa:aa:aa:aa:aa:aa")

	if got.Name != "Study laptop" || got.NameOrigin != OriginOperator {
		t.Errorf("Name = %q (%s), want the operator's", got.Name, got.NameOrigin)
	}

	// Without a stored name, the observed hostname shows through — but marked
	// as observed, not promoted into intent.
	list2, _ := Build(Config{}, sightings, nil, nil)
	got2 := find(t, list2, "aa:aa:aa:aa:aa:aa")
	if got2.Name != "toms-macbook" || got2.NameOrigin != OriginObserved {
		t.Errorf("Name = %q (%s), want the hostname marked observed", got2.Name, got2.NameOrigin)
	}

	// And with neither, the MAC — a device is never nameless on screen.
	list3, _ := Build(Config{}, []Sighting{arp("dd:dd:dd:dd:dd:dd", "192.168.1.7", true)}, nil, nil)
	got3 := find(t, list3, "dd:dd:dd:dd:dd:dd")
	if got3.DisplayName() != "dd:dd:dd:dd:dd:dd" {
		t.Errorf("DisplayName = %q, want the MAC", got3.DisplayName())
	}
}

// Both sources see the same printer. It is one device, seen twice.
func TestMergeCombinesSources(t *testing.T) {
	expires := time.Now().Add(time.Hour)
	sightings := []Sighting{
		{MAC: "aa:aa:aa:aa:aa:aa", IP: "192.168.1.20", Hostname: "laserjet",
			Source: SourceDHCPLease, Active: true, Expires: &expires},
		arp("AA:AA:AA:AA:AA:AA", "192.168.1.20", true),
		arp("aa:aa:aa:aa:aa:aa", "192.168.1.21", false),
	}

	merged, problems := Merge(sightings)
	if len(problems) != 0 {
		t.Errorf("unexpected problems: %s", problemStrings(problems))
	}
	if len(merged) != 1 {
		t.Fatalf("got %d devices, want 1: those are all the same hardware", len(merged))
	}

	p := merged["aa:aa:aa:aa:aa:aa"]
	if len(p.IPs) != 2 {
		t.Errorf("IPs = %v, want both addresses deduplicated to two", p.IPs)
	}
	if len(p.Sources) != 2 {
		t.Errorf("Sources = %v, want both", p.Sources)
	}
	if !p.Active {
		t.Error("active in any source means active")
	}
	if p.Expires == nil {
		t.Error("the lease expiry should survive the merge")
	}
	if p.Hostname != "laserjet" {
		t.Errorf("Hostname = %q, want the one the lease heard", p.Hostname)
	}
}

// A lease heard the client state its own name; ARP never carries one. So a
// lease hostname wins regardless of the order sightings arrive in.
func TestMergePrefersTheLeaseHostname(t *testing.T) {
	sightings := []Sighting{
		{MAC: "aa:aa:aa:aa:aa:aa", Hostname: "from-arp", Source: SourceARP},
		{MAC: "aa:aa:aa:aa:aa:aa", Hostname: "from-lease", Source: SourceDHCPLease},
	}
	merged, _ := Merge(sightings)
	if got := merged["aa:aa:aa:aa:aa:aa"].Hostname; got != "from-lease" {
		t.Errorf("Hostname = %q, want %q", got, "from-lease")
	}

	// And the other way round, to prove it is not just arrival order.
	reversed, _ := Merge([]Sighting{sightings[1], sightings[0]})
	if got := reversed["aa:aa:aa:aa:aa:aa"].Hostname; got != "from-lease" {
		t.Errorf("Hostname = %q, want %q", got, "from-lease")
	}
}

func TestMergeReportsAnUnreadableMAC(t *testing.T) {
	merged, problems := Merge([]Sighting{
		{MAC: "nonsense", IP: "192.168.1.1", Source: SourceARP},
	})
	if len(merged) != 0 {
		t.Errorf("got %d devices, want 0", len(merged))
	}
	if len(problems) != 1 {
		t.Fatalf("a garbled MAC should be reported, not silently dropped; got %d problems",
			len(problems))
	}
}

// Sorting by presence would reorder the whole list every time a phone slept,
// which is the row-jumping that makes a table impossible to click in.
func TestBuildSortsByNameNotByPresence(t *testing.T) {
	cfg := Config{Devices: []Device{
		{MAC: "aa:aa:aa:aa:aa:aa", Name: "Alpha"},
		{MAC: "bb:bb:bb:bb:bb:bb", Name: "Bravo"},
		{MAC: "cc:cc:cc:cc:cc:cc", Name: "Charlie"},
	}}

	// Bravo is the only one online; it must not jump to the top.
	online := []Sighting{lease("bb:bb:bb:bb:bb:bb", "192.168.1.50", "", true)}

	list, _ := Build(cfg, online, nil, nil)
	want := []string{"Alpha", "Bravo", "Charlie"}
	for i, w := range want {
		if list[i].Name != w {
			t.Fatalf("position %d = %q, want %q (order: %v)", i, list[i].Name, w, names(list))
		}
	}

	// Taking Bravo offline must not move anything either.
	offline, _ := Build(cfg, nil, nil, nil)
	for i, w := range want {
		if offline[i].Name != w {
			t.Errorf("after going offline, position %d = %q, want %q", i, offline[i].Name, w)
		}
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	cfg := Config{Devices: []Device{
		{MAC: "aa:aa:aa:aa:aa:aa"},
		{MAC: "bb:bb:bb:bb:bb:bb"},
	}}
	sightings := []Sighting{
		arp("cc:cc:cc:cc:cc:cc", "192.168.1.3", true),
		arp("dd:dd:dd:dd:dd:dd", "192.168.1.4", true),
	}

	first, _ := Build(cfg, sightings, nil, nil)
	for i := 0; i < 20; i++ {
		again, _ := Build(cfg, sightings, nil, nil)
		for j := range first {
			if first[j].MAC != again[j].MAC {
				t.Fatalf("iteration %d differs at %d: %q vs %q", i, j, first[j].MAC, again[j].MAC)
			}
		}
	}
}

func TestBuildEmpty(t *testing.T) {
	list, problems := Build(Config{}, nil, nil, nil)
	if len(list) != 0 {
		t.Errorf("got %d devices, want an empty list rather than a failure", len(list))
	}
	if len(problems) != 0 {
		t.Errorf("unexpected problems: %s", problemStrings(problems))
	}
}

func names(list []Resolved) []string {
	out := make([]string, 0, len(list))
	for _, r := range list {
		out = append(out, r.DisplayName())
	}
	return out
}

// --- placing a device on a network ------------------------------------------

func netw(iface, start, end string) Network {
	return Network{
		Interface: iface,
		Start:     netip.MustParseAddr(start),
		End:       netip.MustParseAddr(end),
	}
}

// What a source saw beats what an address implies, because a range only ever
// says where an address *would* come from.
func TestBuildPrefersTheObservedInterface(t *testing.T) {
	s := arp("aa:bb:cc:dd:ee:ff", "192.168.1.150", true)
	s.Interface = "lan0"

	list, _ := Build(Config{}, []Sighting{s}, nil,
		[]Network{netw("iot0", "192.168.1.100", "192.168.1.200")})

	got := find(t, list, "aa:bb:cc:dd:ee:ff")
	if got.Network != "lan0" {
		t.Errorf("Network = %q, want lan0 — the neighbour table saw it there", got.Network)
	}
	if got.NetworkOrigin != OriginObserved {
		t.Errorf("NetworkOrigin = %q, want %q", got.NetworkOrigin, OriginObserved)
	}
}

// A lease carries no interface, so an away device can only be placed by range.
// Getting this wrong empties the map of everything that is asleep.
func TestBuildPlacesALeaseByItsRange(t *testing.T) {
	list, _ := Build(Config{}, []Sighting{lease("aa:bb:cc:dd:ee:ff", "192.168.30.142", "tv", false)}, nil,
		[]Network{
			netw("lan0", "192.168.1.100", "192.168.1.200"),
			netw("iot0", "192.168.30.100", "192.168.30.240"),
		})

	got := find(t, list, "aa:bb:cc:dd:ee:ff")
	if got.Network != "iot0" {
		t.Errorf("Network = %q, want iot0", got.Network)
	}
	if got.NetworkOrigin != OriginDetected {
		t.Errorf("NetworkOrigin = %q, want %q — a range is an inference", got.NetworkOrigin, OriginDetected)
	}
}

// The reserved printer: its address sits outside every pool by design. Left
// unplaced rather than filed under the nearest network, because a guess drawn
// on a map reads exactly like a fact.
func TestBuildLeavesAnUnplaceableDeviceEmpty(t *testing.T) {
	list, _ := Build(Config{}, []Sighting{lease("aa:bb:cc:dd:ee:ff", "192.168.1.10", "printer", true)}, nil,
		[]Network{netw("lan0", "192.168.1.100", "192.168.1.200")})

	got := find(t, list, "aa:bb:cc:dd:ee:ff")
	if got.Network != "" {
		t.Errorf("Network = %q, want empty — .10 is outside the pool", got.Network)
	}
	if got.NetworkOrigin != OriginNone {
		t.Errorf("NetworkOrigin = %q, want %q", got.NetworkOrigin, OriginNone)
	}
}

// A stored device nothing has ever seen has no address to place it by.
func TestBuildPlacesNothingWithoutPresence(t *testing.T) {
	cfg := Config{Devices: []Device{{MAC: "aa:bb:cc:dd:ee:ff", Name: "Spare laptop"}}}

	list, _ := Build(cfg, nil, nil, []Network{netw("lan0", "192.168.1.100", "192.168.1.200")})

	if got := find(t, list, "aa:bb:cc:dd:ee:ff"); got.Network != "" {
		t.Errorf("Network = %q, want empty", got.Network)
	}
}

func TestNetworkHolds(t *testing.T) {
	n := netw("lan0", "192.168.1.100", "192.168.1.200")

	for _, tc := range []struct {
		addr string
		want bool
	}{
		{"192.168.1.100", true}, // inclusive at the bottom
		{"192.168.1.200", true}, // and at the top
		{"192.168.1.150", true},
		{"192.168.1.99", false},
		{"192.168.1.201", false},
		{"10.0.0.5", false},
		// netip.Addr.Compare orders v4 before v6, so a v6 range would otherwise
		// hold every v4 address on the box.
		{"2001:db8::1", false},
	} {
		if got := n.Holds(netip.MustParseAddr(tc.addr)); got != tc.want {
			t.Errorf("Holds(%s) = %v, want %v", tc.addr, got, tc.want)
		}
	}

	if (Network{Interface: "lan0"}).Holds(netip.MustParseAddr("192.168.1.1")) {
		t.Error("a network with no range should hold nothing")
	}
}
