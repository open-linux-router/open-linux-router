package dnsrelay

import (
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// publishedConfig is loopbackConfig plus a published-names file holding names.
func publishedConfig(t *testing.T, upstream netip.AddrPort, names ...string) Config {
	t.Helper()
	cfg := loopbackConfig(t, upstream)
	cfg.PublishedFile = filepath.Join(t.TempDir(), "published.json")
	writePublished(t, cfg.PublishedFile, names...)
	return cfg
}

func writePublished(t *testing.T, path string, names ...string) {
	t.Helper()
	data, err := MarshalPublished(Published{Names: names})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// countingUpstream answers NOERROR with nothing and counts what reached it.
func countingUpstream(t *testing.T) (*fakeUpstream, func() int) {
	t.Helper()
	var mu sync.Mutex
	n := 0
	up := newFakeUpstream(t, func(query []byte) []byte {
		mu.Lock()
		n++
		mu.Unlock()
		q, _, err := ParseQuestion(query)
		if err != nil {
			t.Error(err)
			return nil
		}
		out := buildResponse(t, 0, q.Name+".", q.Type, dnsmessage.RCodeSuccess, nil)
		copy(out[:2], query[:2])
		return out
	})
	return up, func() int { mu.Lock(); defer mu.Unlock(); return n }
}

// The addresses a loopback test pretends the box has on the asking network.
var lanAddrs = []netip.Addr{
	netip.MustParseAddr("172.16.1.1"),
	netip.MustParseAddr("fd54:2147:3988:4175::1"),
}

func withAddrs(addrs ...netip.Addr) func(*Relay) {
	return func(r *Relay) { r.localAddrs = func(arrival) []netip.Addr { return addrs } }
}

// The fix for a name that did not resolve on the network it was published for:
// the relay answers it itself, with this box's address, and asks nobody.
func TestRelayAnswersAPublishedNameItself(t *testing.T) {
	up, asked := countingUpstream(t)
	_, at := startRelay(t, publishedConfig(t, up.addr(), "nas.home.example.com"), nil,
		withAddrs(lanAddrs...))

	// Mixed case on purpose: the question is matched canonically.
	obs, err := Observe(ask(t, at, buildQuery(t, 1, "NAS.home.example.com.", dnsmessage.TypeA)))
	if err != nil {
		t.Fatal(err)
	}
	if obs.Rcode != "NOERROR" || len(obs.Addrs) != 1 || obs.Addrs[0].String() != "172.16.1.1" {
		t.Errorf("rcode %s, addrs %v; want NOERROR [172.16.1.1]", obs.Rcode, obs.Addrs)
	}

	// Asked over IPv4, so no AAAA: the client has not shown it can reach this
	// box over IPv6 (Relay.addrsFor).
	obs, err = Observe(ask(t, at, buildQuery(t, 1, "nas.home.example.com.", dnsmessage.TypeAAAA)))
	if err != nil {
		t.Fatal(err)
	}
	if obs.Rcode != "NOERROR" || len(obs.Addrs) != 0 {
		t.Errorf("AAAA over IPv4: rcode %s, addrs %v; want NODATA", obs.Rcode, obs.Addrs)
	}
	if n := asked(); n != 0 {
		t.Errorf("a published name was forwarded upstream %d times", n)
	}
}

// Browsers ask for an HTTPS record next to every A. Relaying it would ask the
// internet about a name this box has claimed; NODATA says the name exists and
// has no such record.
func TestRelayAnswersOtherTypesForAPublishedNameWithNoData(t *testing.T) {
	up, asked := countingUpstream(t)
	_, at := startRelay(t, publishedConfig(t, up.addr(), "nas.home.example.com"), nil,
		withAddrs(lanAddrs...))

	obs, err := Observe(ask(t, at, buildQuery(t, 2, "nas.home.example.com.", dnsmessage.Type(65))))
	if err != nil {
		t.Fatal(err)
	}
	if obs.Rcode != "NOERROR" || len(obs.Addrs) != 0 {
		t.Errorf("rcode %s, addrs %v; want NODATA", obs.Rcode, obs.Addrs)
	}
	if n := asked(); n != 0 {
		t.Errorf("forwarded upstream %d times", n)
	}
}

// Only the exact names. A sibling under the same domain is somebody else's —
// a device, or a name hosted publicly — and is relayed as before.
func TestRelayForwardsNamesThatAreNotPublished(t *testing.T) {
	up, asked := countingUpstream(t)
	_, at := startRelay(t, publishedConfig(t, up.addr(), "nas.home.example.com"), nil,
		withAddrs(lanAddrs...))

	for _, name := range []string{"unicom.home.example.com.", "home.example.com.", "x.nas.home.example.com."} {
		ask(t, at, buildQuery(t, 3, name, dnsmessage.TypeA))
	}
	if n := asked(); n != 3 {
		t.Errorf("forwarded %d of 3 unpublished names", n)
	}
}

// The answer depends on where the query arrived, which is the whole reason this
// lives in the relay. The UDP path has to carry the destination through.
func TestRelayAnswersFromWhereTheQueryArrived(t *testing.T) {
	up, _ := countingUpstream(t)

	var mu sync.Mutex
	var seen []arrival
	cfg := publishedConfig(t, up.addr(), "nas.home.example.com")
	_, at := startRelay(t, cfg, nil, func(r *Relay) {
		r.localAddrs = func(a arrival) []netip.Addr {
			mu.Lock()
			seen = append(seen, a)
			mu.Unlock()
			return lanAddrs
		}
	})

	ask(t, at, buildQuery(t, 4, "nas.home.example.com.", dnsmessage.TypeA))

	conn, err := net.DialTCP("tcp", nil, net.TCPAddrFromAddrPort(at))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := writeTCPMessage(conn, buildQuery(t, 5, "nas.home.example.com.", dnsmessage.TypeA)); err != nil {
		t.Fatal(err)
	}
	if _, err := readTCPMessage(conn); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("addresses were looked up %d times, want 2", len(seen))
	}
	for i, transport := range []string{"udp", "tcp"} {
		if seen[i].addr != at.Addr() {
			t.Errorf("%s: arrived at %v, want %v", transport, seen[i].addr, at.Addr())
		}
	}
}

// Publishing a name must not restart DNS for the house, so the file is re-read
// on SIGHUP with the policies.
func TestReloadPicksUpPublishedNames(t *testing.T) {
	up, _ := countingUpstream(t)
	cfg := publishedConfig(t, up.addr())
	relay, at := startRelay(t, cfg, nil, withAddrs(lanAddrs...))

	query := buildQuery(t, 6, "wiki.home.example.com.", dnsmessage.TypeA)
	if obs, _ := Observe(ask(t, at, query)); len(obs.Addrs) != 0 {
		t.Fatalf("answered before it was published: %v", obs.Addrs)
	}

	writePublished(t, cfg.PublishedFile, "wiki.home.example.com")
	if err := relay.Reload(); err != nil {
		t.Fatal(err)
	}
	if obs, _ := Observe(ask(t, at, query)); len(obs.Addrs) != 1 {
		t.Fatalf("not answered after reload: %v", obs.Addrs)
	}

	// And unpublishing: the file is deleted when nothing is published.
	if err := os.Remove(cfg.PublishedFile); err != nil {
		t.Fatal(err)
	}
	if err := relay.Reload(); err != nil {
		t.Fatal(err)
	}
	if relay.publishes("wiki.home.example.com") {
		t.Error("still published after the file was removed")
	}
}

// A policy blocking the local domain for a device still blocks: policy is the
// operator's rule about that client, and it comes first.
func TestBlockingStillWinsOverAPublishedName(t *testing.T) {
	up, _ := countingUpstream(t)
	_, at := startRelay(t, publishedConfig(t, up.addr(), "nas.home.example.com"),
		[]Policy{{Name: "default", Block: []string{"home.example.com"}}},
		withAddrs(lanAddrs...))

	obs, err := Observe(ask(t, at, buildQuery(t, 7, "nas.home.example.com.", dnsmessage.TypeA)))
	if err != nil {
		t.Fatal(err)
	}
	if obs.Rcode != "NXDOMAIN" {
		t.Errorf("rcode %s, want the block's NXDOMAIN", obs.Rcode)
	}
}

// This box's addresses are not where any forwarded traffic goes, so they must
// not land in the domain→address map the traffic view attributes from.
func TestPublishedAnswersStayOutOfTheNameMap(t *testing.T) {
	up, _ := countingUpstream(t)
	cfg := publishedConfig(t, up.addr(), "nas.home.example.com")
	cfg.QueryLogEntries = 10
	relay, at := startRelay(t, cfg, nil, withAddrs(lanAddrs...))

	ask(t, at, buildQuery(t, 8, "nas.home.example.com.", dnsmessage.TypeA))

	deadline := time.Now().Add(2 * time.Second)
	for len(relay.Queries(Unbounded)) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(relay.Queries(Unbounded)) == 0 {
		t.Fatal("the query was not logged")
	}
	if names := relay.Names(time.Now(), Unbounded); len(names) != 0 {
		t.Errorf("name map = %v, want empty", names)
	}
}

func TestLoadPublishedTreatsAMissingFileAsNone(t *testing.T) {
	set, err := LoadPublished(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil || len(set) != 0 {
		t.Fatalf("set %v, err %v", set, err)
	}
}

func TestLoadPublishedRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "published.json")
	if err := os.WriteFile(path, []byte(`{"names":[],"surprise":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPublished(path); err == nil {
		t.Fatal("accepted a field it does not understand")
	}
}

// On a real interface: the addresses of the interface the query arrived on,
// minus the ones a client cannot use. Loopback is the only interface a test can
// count on, and every address on it is unanswerable, so what is left is the
// fallback — which must not hand out 127.0.0.1 either.
func TestSystemLocalAddrsNeverAnswersLoopback(t *testing.T) {
	lo := netip.MustParseAddr("127.0.0.1")
	if got := systemLocalAddrs(arrival{addr: lo}); len(got) != 0 {
		t.Errorf("got %v, want nothing", got)
	}
}

func TestAnswerable(t *testing.T) {
	for addr, want := range map[string]bool{
		"172.16.1.1":             true,
		"fd54:2147:3988:4175::1": true,
		"2001:db8::1":            true,
		"127.0.0.1":              false,
		"::1":                    false,
		"fe80::1":                false,
		"0.0.0.0":                false,
		"169.254.1.1":            false,
	} {
		if got := answerable(netip.MustParseAddr(addr)); got != want {
			t.Errorf("answerable(%s) = %v, want %v", addr, got, want)
		}
	}
}

// IPv6 addresses are handed out only to a client that asked over IPv6. IPv4 is
// always handed out, over either family.
func TestAddrsForGivesIPv6OnlyToAQueryThatArrivedOverIt(t *testing.T) {
	r := &Relay{localAddrs: func(arrival) []netip.Addr { return lanAddrs }}

	over4 := r.addrsFor(arrival{addr: netip.MustParseAddr("172.16.1.1")})
	if len(over4) != 1 || !over4[0].Is4() {
		t.Errorf("over IPv4: %v, want only the IPv4 address", over4)
	}
	if unknown := r.addrsFor(arrival{}); len(unknown) != 1 || !unknown[0].Is4() {
		t.Errorf("arrival unknown: %v, want only the IPv4 address", unknown)
	}
	if over6 := r.addrsFor(arrival{addr: netip.MustParseAddr("fd54:2147:3988:4175::1")}); len(over6) != 2 {
		t.Errorf("over IPv6: %v, want both", over6)
	}
	if len(lanAddrs) != 2 {
		t.Fatal("addrsFor modified the slice it was given")
	}
}
