package gateway

import (
	"errors"
	"net/http"
	"net/netip"
	"testing"
)

// defaultPrefix is the spelling of "everything", which is how a default route
// arrives from the kernel.
func defaultPrefix(family int) netip.Prefix {
	if family == 6 {
		return netip.PrefixFrom(netip.IPv6Unspecified(), 0)
	}
	return netip.PrefixFrom(netip.IPv4Unspecified(), 0)
}

func TestViewRoutesGroupsTablesAndNamesThem(t *testing.T) {
	cfg := Config{Enabled: true, Exits: []Exit{
		{Name: "Proxy", Slot: 1},
		{Name: "Modem", Slot: 2},
	}}
	proxyTable, modemTable := cfg.Exits[0].Table(), cfg.Exits[1].Table()

	v := viewRoutes([]KernelRoute{
		// The main table, in the order a router actually holds it.
		{Family: 4, Table: 254, Dst: netip.MustParsePrefix("192.168.1.0/24"), Dev: "br-lan", Kind: "unicast", Proto: "kernel"},
		{Family: 4, Table: 254, Dst: defaultPrefix(4), Via: netip.MustParseAddr("192.168.1.1"), Dev: "ens18", Metric: 100, Kind: "unicast", Proto: "dhcp"},
		// Our own table for one exit.
		{Family: 4, Table: proxyTable, Dst: defaultPrefix(4), Via: netip.MustParseAddr("192.168.1.50"), Dev: "ens18", Kind: "unicast", Proto: "static"},
		// Somebody else's table, pointed at by a rule olr did not write.
		{Family: 4, Table: 9051, Dst: defaultPrefix(4), Dev: "docker0", Kind: "unicast", Proto: "boot"},
		// The kernel's local table: interface addressing, not a decision.
		{Family: 4, Table: 255, Dst: netip.MustParsePrefix("192.168.1.1/32"), Dev: "br-lan", Kind: "local", Proto: "kernel"},
		{Family: 6, Table: 254, Dst: netip.MustParsePrefix("2001:db8::/64"), Dev: "br-lan", Kind: "unicast", Proto: "kernel"},
	}, cfg, testTime())

	if !v.Known {
		t.Fatal("a read table should be known")
	}
	if len(v.Tables) != 4 {
		t.Fatalf("want main, two exit tables and the foreign one, got %d: %+v", len(v.Tables), v.Tables)
	}

	// main first, then olr's tables in rule order, then anybody else's, and IPv6
	// after IPv4: the order a packet meets them.
	want := []struct {
		family int
		table  int
		name   string
		owner  string
	}{
		{4, 254, "main", "main"},
		{4, proxyTable, "Proxy", "exit"},
		{4, 9051, "table 9051", "other"},
		{6, 254, "main", "main"},
	}
	for i, w := range want {
		got := v.Tables[i]
		if got.Family != w.family || got.Table != w.table || got.Name != w.name || got.Owner != w.owner {
			t.Errorf("table %d: want family %d table %d %q/%s, got family %d table %d %q/%s",
				i, w.family, w.table, w.name, w.owner, got.Family, got.Table, got.Name, got.Owner)
		}
	}

	if p := v.Tables[1]; p.Exit != "Proxy" || p.Priority != cfg.Exits[0].Priority() {
		t.Errorf("an exit's table must name its exit and rule priority, got %+v", p)
	}
	if modemTable == proxyTable {
		t.Fatalf("fixture error: both exits allocated table %d", proxyTable)
	}

	// The route that matches everything leads, and the default route of ours is
	// spelled as a destination rather than left blank.
	main := v.Tables[0].Routes
	if len(main) != 2 || !main[0].Default || main[0].Dst != "0.0.0.0/0" {
		t.Errorf("default route should lead the table, got %+v", main)
	}
	if main[0].Via != "192.168.1.1" || main[0].Proto != "dhcp" {
		t.Errorf("route detail lost: %+v", main[0])
	}
	if main[1].Dst != "192.168.1.0/24" {
		t.Errorf("the more specific route should follow, got %+v", main[1])
	}
}

// A table the configuration knows nothing about is still shown: the box may be
// running an exit that was deleted from the file, and a page that hid it would
// agree with the file rather than with the router.
func TestViewRoutesShowsTablesConfigurationDoesNotKnow(t *testing.T) {
	v := viewRoutes([]KernelRoute{
		{Family: 4, Table: Base + 3, Dst: defaultPrefix(4), Dev: "wg0", Kind: "unicast", Proto: "static"},
	}, Config{}, testTime())

	if len(v.Tables) != 1 {
		t.Fatalf("want the stale table, got %+v", v.Tables)
	}
	if got := v.Tables[0]; got.Name != "table 8103" || got.Owner != "other" || got.Exit != "" {
		t.Errorf("an unowned table is named by its number: %+v", got)
	}
}

func TestGetRoutesReportsWhyItCouldNotRead(t *testing.T) {
	h, _ := newTestHandler(t, &StaticKernel{RoutesErr: errors.New("operation not permitted")})

	w := do(t, h, http.MethodGet, "/routes", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	v := decode[routesView](t, w)
	if v.Known {
		t.Error("a failed read must not report a known table")
	}
	if v.Error != "operation not permitted" {
		t.Errorf("the reason should travel with the absence, got %q", v.Error)
	}
	if v.AsOf != testTime() && v.AsOf.IsZero() {
		t.Errorf("as_of should be stamped, got %v", v.AsOf)
	}
}

func TestGetRoutesServesTheTable(t *testing.T) {
	k := &StaticKernel{Routes: []KernelRoute{
		{Family: 4, Table: 254, Dst: netip.MustParsePrefix("10.0.0.0/8"), Via: netip.MustParseAddr("10.0.0.1"), Dev: "eth0", Kind: "unicast", Proto: "static"},
	}}
	h, a := newTestHandler(t, k)
	if _, err := a.Save(Config{Enabled: true, Exits: []Exit{{Name: "Proxy", Slot: 1, Via: Via{Kind: ViaBlocked}}}}); err != nil {
		t.Fatal(err)
	}

	w := do(t, h, http.MethodGet, "/routes", nil)
	v := decode[routesView](t, w)
	if !v.Known || len(v.Tables) != 1 || len(v.Tables[0].Routes) != 1 {
		t.Fatalf("want one route in one table, got %+v", v)
	}
	if got := v.Tables[0].Routes[0]; got.Dst != "10.0.0.0/8" || got.Via != "10.0.0.1" || got.Dev != "eth0" {
		t.Errorf("route lost in translation: %+v", got)
	}
}
