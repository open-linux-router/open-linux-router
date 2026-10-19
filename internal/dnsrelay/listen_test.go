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

// A service address that is not up yet must not cost the box its resolver.
//
// This is the asymmetry socket.go and bindAll argue about, in one test: the
// operator's address is intent and a failure to bind it is worth refusing
// over, while the address olr promised to a dial-in client is created by
// another module and may simply not exist yet. On a boot where the tunnel comes
// up after us, all-or-nothing would mean no DNS at all until something else
// restarted the relay.
func TestRelayKeepsServingWhenAServiceAddressIsMissing(t *testing.T) {
	upstream := newFakeUpstream(t, func(query []byte) []byte { return query })
	cfg := Config{
		Listen:     []netip.AddrPort{freePort(t)},
		AlsoListen: []netip.AddrPort{netip.AddrPortFrom(unbindable, DefaultPort)},
		Upstream:   upstream.addr(),
	}
	_, at := startRelay(t, cfg, nil, func(r *Relay) { r.retryEvery = 10 * time.Millisecond })

	// Long enough for the retry goroutine to have made one failed attempt.
	time.Sleep(30 * time.Millisecond)

	if got := ask(t, at, buildQuery(t, 0x6262, "example.com.", dnsmessage.TypeA)); len(got) == 0 {
		t.Fatal("a missing service address stopped the relay answering on its pinned one")
	}
}

// And when the address does come up, the relay must start answering on it
// without a restart: the client configurations naming it are already on
// people's phones, so "apply again later" is not a fix.
func TestRelayAnswersOnAServiceAddressOnceItComesUp(t *testing.T) {
	upstream := newFakeUpstream(t, func(query []byte) []byte { return query })
	optional, release := heldPort(t)
	cfg := Config{
		Listen:     []netip.AddrPort{freePort(t)},
		AlsoListen: []netip.AddrPort{optional},
		Upstream:   upstream.addr(),
	}
	startRelay(t, cfg, nil, func(r *Relay) { r.retryEvery = 10 * time.Millisecond })

	release()
	waitForListener(t, optional)

	if got := ask(t, optional, buildQuery(t, 0x6363, "example.com.", dnsmessage.TypeA)); len(got) == 0 {
		t.Fatal("the service address came up but the relay did not answer on it")
	}
}

// Under the wildcard there is nothing to add: the socket already covers every
// address this box holds or will hold. Asking for a second, pinned copy of one
// would be a bind that can only fail.
func TestAlsoIsEmptyUnderTheWildcard(t *testing.T) {
	cfg := Config{AlsoListen: []netip.AddrPort{netip.MustParseAddrPort("10.6.0.1:53")}}
	if got := cfg.Also(); len(got) != 0 {
		t.Errorf("Also() = %v under the wildcard, want nothing", got)
	}
}

// A pinned address wins over the same address arriving as a service address.
func TestAlsoDropsDuplicatesAndPins(t *testing.T) {
	pin := netip.MustParseAddrPort("172.16.1.1:53")
	tunnel := netip.MustParseAddrPort("10.6.0.1:53")
	cfg := Config{
		Listen:     []netip.AddrPort{pin},
		AlsoListen: []netip.AddrPort{tunnel, pin, tunnel},
	}
	got := cfg.Also()
	if len(got) != 1 || got[0] != tunnel {
		t.Errorf("Also() = %v, want just %v", got, tunnel)
	}
}

// heldPort reserves one loopback port for both transports and hands back the
// release, so a test can watch the relay pick the address up on a later retry.
func heldPort(t *testing.T) (netip.AddrPort, func()) {
	t.Helper()
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	port := udp.LocalAddr().(*net.UDPAddr).Port
	tcp, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		udp.Close()
		t.Fatal(err)
	}
	addr := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(port))
	return addr, func() { udp.Close(); tcp.Close() }
}

// waitForListener polls until something accepts TCP on addr, which is the
// cheapest evidence that both sockets are bound.
func waitForListener(t *testing.T, addr netip.AddrPort) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr.String(), 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the relay never bound %s: %v", addr, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
