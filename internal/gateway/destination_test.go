package gateway

import (
	"net/http"
	"net/netip"
	"strings"
	"testing"
)

func TestDestinationRoutingUsesOtherwiseUnassignedSocksExit(t *testing.T) {
	c := Config{Enabled: true, Exits: []Exit{{Name: "Socks", Via: Via{Kind: ViaInterface, Interface: "olrsocks0"}}},
		Destinations: []DestinationAssignment{{"1.2.0.0/16", "Socks"}, {"1.2.3.4", "Socks"}}}
	c.Normalize()
	if r := Validate(c, testLinks()); !r.OK() {
		t.Fatal(r.Err())
	}
	links := testLinks()
	links["olrsocks0"] = LinkInfo{Name: "olrsocks0", Up: true}
	links["wan"] = LinkInfo{Name: "wan", Up: true, Prefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}}
	d := Render(c, links, nil)
	if len(d.Table.Sources) != 0 || len(d.Table.Destinations) != 4 || len(d.Table.Exits) != 1 {
		t.Fatalf("unexpected rules: %+v", d.Table)
	}
	if d.Table.Destinations[0].Destination != netip.MustParsePrefix("1.2.3.4/32") {
		t.Fatal("longest prefix must win")
	}
	if !strings.Contains(d.Text(), "unreachable") || !strings.Contains(d.Text(), "olrsocks0") {
		t.Fatal("missing SOCKS fail-closed routes", d.Text())
	}
	missing := Render(c, testLinks(), nil)
	for _, route := range missing.Routes {
		if route.Dev == "olrsocks0" {
			t.Fatal("missing SOCKS backend still routes to TUN")
		}
	}
	if !strings.Contains(missing.Text(), "unreachable") {
		t.Fatal("missing SOCKS backend must block")
	}
	if !c.InUse("Socks") || len(c.UsedBy("Socks")) != 2 {
		t.Fatal("destination references lost")
	}
	c.Rename("Socks", "Renamed")
	if c.Destinations[0].Exit != "Renamed" {
		t.Fatal("rename lost reference")
	}
}

func TestDestinationValidation(t *testing.T) {
	for _, rules := range [][]DestinationAssignment{
		{{"example.com", "Proxy"}}, {{"1.2.3.4", "missing"}},
		{{"1.2.3.4", "Proxy"}, {"1.2.3.4/32", "Proxy"}},
		{{"1.2.3.4/24", "Proxy"}, {"1.2.3.0/24", "Proxy"}},
		{{"::ffff:1.2.3.4", "Proxy"}},
	} {
		c := testConfig()
		c.Destinations = rules
		c.Normalize()
		if Validate(c, testLinks()).OK() {
			t.Fatalf("accepted invalid rules: %+v", rules)
		}
	}
}

func TestDestinationItemEditsPreserveOtherAssignments(t *testing.T) {
	h, a := newTestHandler(t, &StaticKernel{})
	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != 200 {
		t.Fatal(w.Body)
	}
	for _, address := range []string{"1.2.3.4/32", "1.2.0.0/16", "1.2.3.4/32"} {
		if w := do(t, h, http.MethodPut, "/destinations/"+address, assignmentBody{Exit: "Proxy"}); w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
	}
	c, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Destinations) != 2 || len(c.Interfaces) != 2 {
		t.Fatal("item update lost assignments", c)
	}
	if w := do(t, h, http.MethodDelete, "/exits/Proxy", nil); w.Code < 400 {
		t.Fatal("removed referenced exit")
	}
	if w := do(t, h, http.MethodDelete, "/destinations/1.2.3.4/32", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	c, _ = a.Load()
	if len(c.Destinations) != 1 {
		t.Fatal(c.Destinations)
	}
}
