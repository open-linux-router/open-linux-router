package dnsrelay

import (
	"net/netip"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

// unbindable is an address this machine cannot possibly hold.
//
// RFC 5737 reserves 192.0.2.0/24 for documentation and it is never assigned to
// an interface, so binding it fails with EADDRNOTAVAIL on every platform. That
// is the same error a box gets from an address that was real yesterday and is
// not today, which is the case these tests are about.
var unbindable = netip.MustParseAddr("192.0.2.1")

func TestListenOrWildcardCoversBothFamilies(t *testing.T) {
	got := Config{}.ListenOrWildcard()
	if len(got) != 2 {
		t.Fatalf("ListenOrWildcard() = %v, want one address per family", got)
	}

	var v4, v6 bool
	for _, a := range got {
		if a.Port() != DefaultPort {
			t.Errorf("%v is not on the default port", a)
		}
		if !a.Addr().IsUnspecified() {
			t.Errorf("%v is not a wildcard", a)
		}
		if a.Addr().Is4() {
			v4 = true
		} else {
			v6 = true
		}
	}
	if !v4 || !v6 {
		t.Errorf("ListenOrWildcard() = %v, want both families", got)
	}

	// A pinned address is intent and is passed through untouched.
	pinned := Config{Listen: []netip.AddrPort{netip.MustParseAddrPort("192.168.1.1:53")}}
	if got := pinned.ListenOrWildcard(); len(got) != 1 || got[0] != pinned.Listen[0] {
		t.Errorf("ListenOrWildcard() = %v, want the pinned address alone", got)
	}
}

func TestPortFollowsAPinnedAddress(t *testing.T) {
	if got := (Config{}).Port(); got != DefaultPort {
		t.Errorf("Port() = %d, want %d", got, DefaultPort)
	}
	pinned := Config{Listen: []netip.AddrPort{netip.MustParseAddrPort("192.168.1.1:5300")}}
	if got := pinned.Port(); got != 5300 {
		t.Errorf("Port() = %d, want the pinned 5300", got)
	}
}

// An explicitly pinned address must not silently turn into a wildcard.
func TestRelayRefusesUnavailablePinnedAddress(t *testing.T) {
	upstream := newFakeUpstream(t, func(query []byte) []byte { return query })
	cfg := Config{Listen: []netip.AddrPort{netip.AddrPortFrom(unbindable, freePort(t).Port())}, Upstream: upstream.addr()}
	relay, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := relay.bind(); err == nil {
		t.Fatal("unavailable pinned address fell back to wildcard")
	}
}

// A wildcard listener accepts queries arriving on any address. The firewall
// is responsible for deciding which networks can reach it.
func TestWildcardBindAnswersQueries(t *testing.T) {
	upstream := newFakeUpstream(t, func(query []byte) []byte { return query })
	port := freePort(t).Port()
	cfg := Config{Listen: []netip.AddrPort{netip.AddrPortFrom(netip.IPv4Unspecified(), port)}, Upstream: upstream.addr()}
	startRelay(t, cfg, nil)
	at := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), port)
	if got := ask(t, at, buildQuery(t, 0x5151, "example.com.", dnsmessage.TypeA)); len(got) == 0 {
		t.Fatal("wildcard listener did not answer")
	}
}
