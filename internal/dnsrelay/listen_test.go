package dnsrelay

import (
	"net"
	"net/netip"
	"testing"
	"time"

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

// The regression this whole change exists for.
//
// A pinned listen address stops existing when the network changes under the box
// — a new lease, a renumbered upstream, a cable moved. Run used to return the
// bind error, and with Restart=always that was a crash loop that took DNS down
// for the whole building, with the reason buried in the journal.
//
// It now falls back to the wildcard and keeps answering. Serving from an
// address the operator did not name is a smaller wrong than not serving, and it
// is the only one of the two anybody can recover from without a console.
func TestRelayFallsBackToTheWildcardWhenAPinnedAddressIsGone(t *testing.T) {
	upstream := newFakeUpstream(t, func(query []byte) []byte { return query })
	port := freePort(t).Port()

	cfg := Config{
		Listen:    []netip.AddrPort{netip.AddrPortFrom(unbindable, port)},
		AllowFrom: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
		Upstream:  upstream.addr(),
	}
	startRelay(t, cfg, nil)

	// The fallback keeps the port the operator pinned, so whatever was pointed
	// at this relay — DHCP option 6, a hijack rule — still reaches it.
	at := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), port)
	query := buildQuery(t, 0x5151, "example.com.", dnsmessage.TypeA)
	if got := ask(t, at, query); len(got) == 0 {
		t.Fatal("the relay bound nothing and answered nothing")
	}
}

// Binding everywhere must not answer everybody. The source check is what keeps
// a wildcard-bound resolver off the internet, and it runs on every datagram
// regardless of which socket received it — so widening the bind cannot widen
// who gets an answer.
func TestWildcardBindStillDropsAForeignSource(t *testing.T) {
	upstream := newFakeUpstream(t, func(query []byte) []byte { return query })
	port := freePort(t).Port()

	cfg := Config{
		// Wildcard, but only a network this query will not come from.
		AllowFrom: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		Upstream:  upstream.addr(),
	}
	cfg.Listen = []netip.AddrPort{netip.AddrPortFrom(netip.IPv4Unspecified(), port)}
	relay, _ := startRelay(t, cfg, nil)

	at := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), port)
	if answered := askExpectingSilence(t, at, buildQuery(t, 0x5151, "example.com.", dnsmessage.TypeA)); answered {
		t.Error("a source outside allow_from was answered, which is what makes a resolver a reflector")
	}
	// Dropped rather than refused, and counted: a REFUSED reply is still a
	// reply, and the gap has to be visible.
	if got := relay.counters.Refused.Load(); got == 0 {
		t.Error("the drop was not counted, so it would be invisible")
	}
}

// askExpectingSilence sends a query and reports whether anything came back.
//
// A separate helper from ask because the absence of a reply is the assertion
// here, so a read timeout is the pass rather than a failure.
func askExpectingSilence(t *testing.T, at netip.AddrPort, query []byte) bool {
	t.Helper()

	conn, err := net.DialUDP("udp", nil, net.UDPAddrFromAddrPort(at))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(query); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 65535)
	_, err = conn.Read(buf)
	return err == nil
}
