package link

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The box this was found on: a network on ens19 removed, and its address left
// behind with nothing in olr that knew why.
func lanOn(member string) Config {
	return Config{
		Adopted: []string{"lan0", "lan1"},
		Networks: []Network{{
			Name: "lan", Members: []string{member},
			IPv4: &NetworkIPv4{Subnet: netip.MustParsePrefix("192.168.1.0/24")},
		}},
	}
}

func TestRemovingANetworkRetiresItsRouterAddress(t *testing.T) {
	before, after := lanOn("lan0"), Config{Adopted: []string{"lan0", "lan1"}}

	got := RetiredFor(before, after)
	want := []Desired{{Interface: "lan0", AddOnly: true,
		Retire: []netip.Prefix{netip.MustParsePrefix("192.168.1.1/24")}}}
	if len(got) != 1 || got[0].Interface != want[0].Interface || !got[0].AddOnly ||
		!slices.Equal(got[0].Retire, want[0].Retire) {
		t.Errorf("RetiredFor = %+v, want %+v", got, want)
	}

	// An interface still in some network is that network's apply's business.
	moved := lanOn("lan0")
	moved.Networks[0].Name = "home"
	if got := RetiredFor(before, moved); len(got) != 0 {
		t.Errorf("a member that moved networks was retired: %+v", got)
	}
}

// The plan says so on the interface, only for an address that is still there,
// and not at all when the address is being kept for the uplink.
func TestThePlanShowsARemovedNetworksAddressComingOff(t *testing.T) {
	observed := []Interface{{Name: "lan0", Up: true, Prefixes: prefixes(t, "192.168.1.1/24")}}
	before, after := lanOn("lan0"), Config{Adopted: []string{"lan0", "lan1"}}

	plan := buildPlan(before, after, observed, Options{})
	change, ok := linkChangeAt(plan, "interfaces[lan0]")
	if !ok || change.Diff != "- ip addr del 192.168.1.1/24 dev lan0\n" || change.Impact != impactDisruptive {
		t.Errorf("interface change = %+v (found %v)", change, ok)
	}

	gone := []Interface{{Name: "lan0", Up: true}}
	if _, ok := linkChangeAt(buildPlan(before, after, gone, Options{}), "interfaces[lan0]"); ok {
		t.Error("the plan promised to remove an address that is not there")
	}

	kept := buildPlan(before, after, observed, Options{KeepAddresses: true})
	if _, ok := linkChangeAt(kept, "interfaces[lan0]"); ok {
		t.Errorf("keep_addresses still planned a removal: %+v", kept.Changes)
	}
	if network, _ := linkChangeAt(kept, "networks[lan]"); network.Impact != impactRestart {
		t.Errorf("a removal keeping its address is %q, want %q", network.Impact, impactRestart)
	}
}

// Apply hands the writer the retirement — even when no network is left, which
// used to mean the writer was not called at all.
func TestApplyRetiresUnlessAskedToKeep(t *testing.T) {
	document := `{"link": {"adopted": ["lan0", "lan1"], "networks": [{"name": "lan", "members": ["lan0"],
		"ipv4": {"subnet": "192.168.1.0/24"}}]}}`
	after := Config{Adopted: []string{"lan0", "lan1"}}

	for _, keep := range []bool{false, true} {
		w := &RecordingWriter{}
		a := Applier{Store: storeWith(t, document), Source: staticSource(testInterfaces(t)...), Writer: w}
		if _, err := a.ApplyWith(context.Background(), after, Options{KeepAddresses: keep}); err != nil {
			t.Fatal(err)
		}
		retired := slices.ContainsFunc(w.Applied, func(d Desired) bool { return len(d.Retire) > 0 })
		if retired == keep {
			t.Errorf("keep=%v: writer got %+v", keep, w.Applied)
		}
	}
}

// --- removing an address by hand --------------------------------------------

func removalApplier(t *testing.T) (Applier, *RecordingWriter) {
	t.Helper()
	w := &RecordingWriter{}
	return Applier{
		Store: storeWith(t, `{"link": {"adopted": ["lan0", "lan1"], "networks": [{"name": "lan",
			"members": ["lan0"], "ipv4": {"subnet": "192.168.1.0/24", "router": "192.168.1.2"}}]}}`),
		Source: staticSource(
			Interface{Name: "lan0", Up: true, Prefixes: prefixes(t, "192.168.1.2/24")},
			Interface{Name: "lan1", Up: true, Prefixes: prefixes(t, "172.16.1.1/24", "192.168.1.3/24")},
			Interface{Name: "wan0", Up: true, Prefixes: prefixes(t, "203.0.113.4/24")},
		),
		Writer: w,
	}, w
}

func TestRemoveAddressRefusesWhatItDoesNotOwn(t *testing.T) {
	a, w := removalApplier(t)
	uplink := []Claim{{Interface: "lan1", Address: netip.MustParsePrefix("192.168.1.3/24")}}
	ctx := context.Background()

	for _, tc := range []struct {
		name  string
		iface string
		addr  string
		want  error
	}{
		{"not adopted", "wan0", "203.0.113.4/24", ErrNotAdopted},
		{"a network's", "lan0", "192.168.1.2/24", ErrNetworkOwned},
		{"the uplink's", "lan1", "192.168.1.3/24", ErrClaimed},
		{"not there", "lan1", "10.0.0.1/8", ErrNotPresent},
	} {
		_, err := a.RemoveAddress(ctx, tc.iface, netip.MustParsePrefix(tc.addr), uplink)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	if len(w.Applied) != 0 {
		t.Fatalf("a refusal reached the writer: %+v", w.Applied)
	}

	if _, err := a.RemoveAddress(ctx, "lan1", netip.MustParsePrefix("172.16.1.1/24"), uplink); err != nil {
		t.Fatal(err)
	}
	if len(w.Applied) != 1 || !w.Applied[0].AddOnly ||
		!slices.Equal(w.Applied[0].Retire, []netip.Prefix{netip.MustParsePrefix("172.16.1.1/24")}) {
		t.Errorf("writer got %+v, want exactly the one address and nothing claimed", w.Applied)
	}
}

func TestRemovingAnAddressOverHTTP(t *testing.T) {
	a, _ := removalApplier(t)
	h := HTTP{Applier: a, Lock: core.NewLock(), Events: core.NewEvents()}.Handler()

	for _, tc := range []struct {
		path  string
		local string
		want  int
	}{
		{"/interfaces/lan1/addresses/172.16.1.1/24", "", http.StatusOK},
		{"/interfaces/lan1/addresses/10.0.0.1/8", "", http.StatusNotFound},
		{"/interfaces/lan0/addresses/192.168.1.2/24", "", http.StatusUnprocessableEntity},
		{"/interfaces/lan1/addresses/nonsense", "", http.StatusBadRequest},
		// The session that would be cut off is the one asking.
		{"/interfaces/lan1/addresses/172.16.1.1/24", "172.16.1.1", http.StatusConflict},
	} {
		r := httptest.NewRequest(http.MethodDelete, tc.path, nil)
		if tc.local != "" {
			r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey,
				&net.TCPAddr{IP: net.ParseIP(tc.local), Port: 8080}))
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != tc.want {
			t.Errorf("DELETE %s (local %q) = %d, want %d: %s", tc.path, tc.local, rec.Code, tc.want,
				strings.TrimSpace(rec.Body.String()))
		}
	}
}

func linkChangeAt(plan planView, path string) (changeView, bool) {
	for _, c := range plan.Changes {
		if c.Path == path {
			return c, true
		}
	}
	return changeView{}, false
}
