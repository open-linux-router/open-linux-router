package dnsrelay

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// fakeUpstream is a resolver on loopback that answers with whatever bytes it
// was given. Handing it a fixed reply is what lets the passthrough test assert
// on identity rather than on equivalence.
type fakeUpstream struct {
	t     *testing.T
	udp   *net.UDPConn
	tcp   *net.TCPListener
	reply func(query []byte) []byte
}

func newFakeUpstream(t *testing.T, reply func([]byte) []byte) *fakeUpstream {
	t.Helper()
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	tcpAddr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: udp.LocalAddr().(*net.UDPAddr).Port}
	tcp, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		udp.Close()
		t.Fatal(err)
	}

	u := &fakeUpstream{t: t, udp: udp, tcp: tcp, reply: reply}
	go u.serveUDP()
	go u.serveTCP()
	t.Cleanup(func() { udp.Close(); tcp.Close() })
	return u
}

func (u *fakeUpstream) addr() netip.AddrPort {
	return u.udp.LocalAddr().(*net.UDPAddr).AddrPort()
}

func (u *fakeUpstream) serveUDP() {
	buf := make([]byte, 65535)
	for {
		n, from, err := u.udp.ReadFromUDPAddrPort(buf)
		if err != nil {
			return
		}
		query := append([]byte(nil), buf[:n]...)
		if _, err := u.udp.WriteToUDPAddrPort(u.reply(query), from); err != nil {
			return
		}
	}
}

func (u *fakeUpstream) serveTCP() {
	for {
		conn, err := u.tcp.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			for {
				query, err := readTCPMessage(conn)
				if err != nil {
					return
				}
				if err := writeTCPMessage(conn, u.reply(query)); err != nil {
					return
				}
			}
		}()
	}
}

// socketPath returns a unix socket path short enough to bind.
//
// Not t.TempDir(): a sockaddr_un's path is capped at around a hundred bytes,
// and macOS puts temp directories under /var/folders/<gibberish>/T/<TestName>/,
// which overflows it. The same test would then pass on Linux and fail here for
// a reason with nothing to do with the code — which is exactly what the four
// pre-existing TestListenUnix failures in internal/core are.
func socketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "olrdns")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "o.sock")
}

// freePort reserves a port on loopback and hands it back, so a test can bind it
// as the relay's own listener.
func freePort(t *testing.T) netip.AddrPort {
	t.Helper()
	l, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	addr := l.LocalAddr().(*net.UDPAddr).AddrPort()
	l.Close()
	return addr
}

// startRelay runs a relay against a fake upstream and returns where to reach it.
func startRelay(t *testing.T, cfg Config, policies []Policy) (*Relay, netip.AddrPort) {
	t.Helper()

	if cfg.PolicyDir == "" && len(policies) > 0 {
		dir := t.TempDir()
		for _, p := range policies {
			data, err := MarshalPolicy(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, p.Name+".json"), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		cfg.PolicyDir = dir
	}

	// Quiet: these tests exercise failure paths on purpose, and the warnings
	// they produce are expected rather than interesting.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	relay, err := New(cfg, logger)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := relay.Run(ctx, func() { close(ready) }); err != nil {
			t.Errorf("Run: %v", err)
		}
	}()

	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("the relay never reported itself ready")
	}
	t.Cleanup(func() { cancel(); <-done })

	return relay, cfg.Listen[0]
}

func ask(t *testing.T, at netip.AddrPort, query []byte) []byte {
	t.Helper()
	conn, err := net.DialUDP("udp", nil, net.UDPAddrFromAddrPort(at))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(query); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 65535)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	return buf[:n]
}

func loopbackConfig(t *testing.T, upstream netip.AddrPort) Config {
	t.Helper()
	return Config{
		Listen:    []netip.AddrPort{freePort(t)},
		AllowFrom: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
		Upstream:  upstream,
	}
}

// The invariant the whole relay is built on: parse a copy for observation,
// forward the original bytes untouched. Re-serialising would break DNSSEC
// signatures, drop EDNS options we did not model, and mangle record types we
// have never heard of.
func TestRelayForwardsResponsesByteForByte(t *testing.T) {
	// A reply with a record type this code knows nothing about, and trailing
	// bytes it could not have produced itself. If anything re-serialised, this
	// would not survive.
	canned := buildResponse(t, 0, "www.example.com.", dnsmessage.TypeA, dnsmessage.RCodeSuccess, []answer{
		{owner: "www.example.com.", ttl: 300, a: [4]byte{93, 184, 216, 34}},
	})
	up := newFakeUpstream(t, func(query []byte) []byte {
		out := append([]byte(nil), canned...)
		// Echo the client's transaction ID, as a real resolver would.
		copy(out[:2], query[:2])
		return out
	})

	_, at := startRelay(t, loopbackConfig(t, up.addr()), nil)

	query := buildQuery(t, 0x4242, "www.example.com.", dnsmessage.TypeA)
	got := ask(t, at, query)

	want := append([]byte(nil), canned...)
	copy(want[:2], query[:2])
	if !bytes.Equal(got, want) {
		t.Errorf("the response was not relayed verbatim:\n got %x\nwant %x", got, want)
	}
}

func TestRelayForwardsOverTCP(t *testing.T) {
	up := newFakeUpstream(t, func(query []byte) []byte {
		out := buildResponse(t, 0, "www.example.com.", dnsmessage.TypeA, dnsmessage.RCodeSuccess, []answer{
			{owner: "www.example.com.", ttl: 300, a: [4]byte{1, 2, 3, 4}},
		})
		copy(out[:2], query[:2])
		return out
	})

	_, at := startRelay(t, loopbackConfig(t, up.addr()), nil)

	conn, err := net.DialTCP("tcp", nil, net.TCPAddrFromAddrPort(at))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}

	query := buildQuery(t, 7, "www.example.com.", dnsmessage.TypeA)
	if err := writeTCPMessage(conn, query); err != nil {
		t.Fatal(err)
	}
	response, err := readTCPMessage(conn)
	if err != nil {
		t.Fatal(err)
	}
	obs, err := Observe(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.Addrs) != 1 || obs.Addrs[0].String() != "1.2.3.4" {
		t.Errorf("Addrs = %v", obs.Addrs)
	}

	// Connection reuse is RFC 7766's whole point; a second query on the same
	// connection has to work.
	if err := writeTCPMessage(conn, query); err != nil {
		t.Fatal(err)
	}
	if _, err := readTCPMessage(conn); err != nil {
		t.Fatalf("the connection was not reusable: %v", err)
	}
}

// A blocked name is answered by us and never reaches the upstream at all.
func TestRelayBlocksWithoutAskingUpstream(t *testing.T) {
	asked := make(chan struct{}, 8)
	up := newFakeUpstream(t, func(query []byte) []byte {
		asked <- struct{}{}
		out := buildResponse(t, 0, "ads.example.com.", dnsmessage.TypeA, dnsmessage.RCodeSuccess, nil)
		copy(out[:2], query[:2])
		return out
	})

	_, at := startRelay(t, loopbackConfig(t, up.addr()), []Policy{{
		Name: "default", Block: []string{"example.com"},
	}})

	response := ask(t, at, buildQuery(t, 9, "ads.example.com.", dnsmessage.TypeA))

	obs, err := Observe(response)
	if err != nil {
		t.Fatal(err)
	}
	if obs.Rcode != "NXDOMAIN" {
		t.Errorf("Rcode = %s, want NXDOMAIN", obs.Rcode)
	}
	select {
	case <-asked:
		t.Error("a blocked name was forwarded upstream anyway")
	default:
	}
}

// Dropped without an answer, not refused with one. A REFUSED reply is still a
// reply, and answering an unsolicited source at all is what makes a resolver
// useful as a reflector.
func TestRelayDropsQueriesFromOutsideTheAllowList(t *testing.T) {
	up := newFakeUpstream(t, func(query []byte) []byte { return query })

	cfg := loopbackConfig(t, up.addr())
	cfg.AllowFrom = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	relay, at := startRelay(t, cfg, nil)

	conn, err := net.DialUDP("udp", nil, net.UDPAddrFromAddrPort(at))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write(buildQuery(t, 1, "example.com.", dnsmessage.TypeA)); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 512)
	if n, err := conn.Read(buf); err == nil {
		t.Errorf("a query from outside the allow list was answered with %d bytes", n)
	}

	// The refusal has to be counted, or a steady stream of them is invisible.
	if got := relay.Snapshot().Refused; got == 0 {
		t.Error("the refusal was not counted")
	}
}

// An empty allow list denies everybody: a relay that answers nobody is a
// visible outage somebody fixes in minutes, where one that answers the internet
// is an amplifier nobody notices until it is used against a third party.
func TestEmptyAllowListDeniesEverybody(t *testing.T) {
	up := newFakeUpstream(t, func(query []byte) []byte { return query })
	cfg := loopbackConfig(t, up.addr())
	cfg.AllowFrom = nil

	relay, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if relay.allowed(netip.MustParseAddr("127.0.0.1")) {
		t.Error("an empty allow list let somebody in")
	}
}

func TestRelayRecordsWhatItSaw(t *testing.T) {
	up := newFakeUpstream(t, func(query []byte) []byte {
		out := buildResponse(t, 0, "www.example.com.", dnsmessage.TypeA, dnsmessage.RCodeSuccess, []answer{
			{owner: "www.example.com.", ttl: 300, cname: "example.cdn.net."},
			{owner: "example.cdn.net.", ttl: 300, a: [4]byte{93, 184, 216, 34}},
		})
		copy(out[:2], query[:2])
		return out
	})

	cfg := loopbackConfig(t, up.addr())
	cfg.QueryLogEntries = 16
	relay, at := startRelay(t, cfg, nil)

	ask(t, at, buildQuery(t, 11, "www.example.com.", dnsmessage.TypeA))

	// The tee is asynchronous by design, so the observation lags the answer.
	// That is the whole point of it, and it is why this waits rather than
	// asserting immediately.
	deadline := time.Now().Add(3 * time.Second)
	var queries []Query
	for time.Now().Before(deadline) {
		if queries = relay.Queries(); len(queries) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(queries) != 1 {
		t.Fatalf("recorded %d queries, want 1", len(queries))
	}
	if queries[0].Name != "www.example.com" {
		t.Errorf("Name = %q, want the QNAME rather than the record owner", queries[0].Name)
	}
	if queries[0].Client.Unmap() != netip.MustParseAddr("127.0.0.1") {
		t.Errorf("Client = %s", queries[0].Client)
	}

	names := relay.Names(time.Now())
	if len(names) != 1 || names[0].Name != "www.example.com" {
		t.Fatalf("name map = %+v", names)
	}
	if names[0].Addr.String() != "93.184.216.34" {
		t.Errorf("Addr = %s", names[0].Addr)
	}
}

// A blocked answer's addresses are ours, not the name's. Recording 0.0.0.0
// against a domain would pollute the map with an address no traffic goes to.
func TestBlockedAnswersStayOutOfTheNameMap(t *testing.T) {
	up := newFakeUpstream(t, func(query []byte) []byte { return query })

	cfg := loopbackConfig(t, up.addr())
	cfg.QueryLogEntries = 16
	relay, at := startRelay(t, cfg, []Policy{{
		Name: "default", Block: []string{"ads.example"}, Response: RespondZero,
	}})

	ask(t, at, buildQuery(t, 12, "ads.example.", dnsmessage.TypeA))

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(relay.Queries()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	queries := relay.Queries()
	if len(queries) != 1 || !queries[0].Blocked {
		t.Fatalf("the block was not recorded: %+v", queries)
	}
	if queries[0].Policy != "default" {
		t.Errorf("Policy = %q — without it the log cannot say why", queries[0].Policy)
	}
	if names := relay.Names(time.Now()); len(names) != 0 {
		t.Errorf("a blocked answer entered the name map: %+v", names)
	}
}

// SIGHUP re-reads the policy directory. Rebinding a socket is a restart; a
// blocklist edit must not interrupt a query.
func TestReloadPicksUpNewPolicies(t *testing.T) {
	up := newFakeUpstream(t, func(query []byte) []byte {
		out := buildResponse(t, 0, "later.example.", dnsmessage.TypeA, dnsmessage.RCodeSuccess, []answer{
			{owner: "later.example.", ttl: 300, a: [4]byte{1, 2, 3, 4}},
		})
		copy(out[:2], query[:2])
		return out
	})

	dir := t.TempDir()
	cfg := loopbackConfig(t, up.addr())
	cfg.PolicyDir = dir
	relay, at := startRelay(t, cfg, nil)

	if obs, err := Observe(ask(t, at, buildQuery(t, 1, "later.example.", dnsmessage.TypeA))); err != nil {
		t.Fatal(err)
	} else if obs.Rcode != "NOERROR" {
		t.Fatalf("Rcode = %s before the policy exists", obs.Rcode)
	}

	data, err := MarshalPolicy(Policy{Name: "default", Block: []string{"later.example"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "default.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := relay.Reload(); err != nil {
		t.Fatal(err)
	}

	obs, err := Observe(ask(t, at, buildQuery(t, 2, "later.example.", dnsmessage.TypeA)))
	if err != nil {
		t.Fatal(err)
	}
	if obs.Rcode != "NXDOMAIN" {
		t.Errorf("Rcode = %s after the reload, want NXDOMAIN", obs.Rcode)
	}
}

// A half-written policy file must not silently unblock everything it used to
// block. Failing closed on the previous rules is the safe direction.
func TestReloadKeepsTheOldPoliciesWhenAFileIsBroken(t *testing.T) {
	up := newFakeUpstream(t, func(query []byte) []byte { return query })

	dir := t.TempDir()
	data, err := MarshalPolicy(Policy{Name: "default", Block: []string{"ads.example"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "default.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := loopbackConfig(t, up.addr())
	cfg.PolicyDir = dir
	relay, _ := startRelay(t, cfg, nil)

	if err := os.WriteFile(filepath.Join(dir, "default.json"), []byte("{ truncated"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := relay.Reload(); err == nil {
		t.Fatal("a broken policy file reloaded successfully")
	}

	if d := relay.Policies().Decide(netip.MustParseAddr("127.0.0.1"), "ads.example"); !d.Blocked {
		t.Error("a failed reload unblocked a name that was blocked before it")
	}
}

// The tee is asynchronous, so it can drop. What it must never do is block the
// answer — and the size of the lie that introduces has to be visible.
func TestDroppedObservationsAreCounted(t *testing.T) {
	relay := &Relay{
		log:     NewQueryLog(4),
		names:   NewNameMap(),
		clients: NewClientTable(),
		tee:     make(chan observation, 1),
		started: time.Now(),
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		cfg:     Config{},
	}

	// Nothing is consuming the tee, so everything past the buffer is dropped
	// rather than blocking.
	for range 20 {
		relay.observe(netip.MustParseAddr("127.0.0.1"), result{response: []byte{0, 0}}, time.Now())
	}
	if got := relay.counters.Dropped.Load(); got == 0 {
		t.Error("observations were dropped without being counted")
	}
}

// A response the observer cannot read still costs a statistics entry and
// nothing else — and the query is still logged from what the question told us,
// because a response we could not parse is not a query that did not happen.
func TestUnparseableResponsesAreCountedAndStillLogged(t *testing.T) {
	relay := &Relay{
		log:     NewQueryLog(4),
		names:   NewNameMap(),
		clients: NewClientTable(),
		tee:     make(chan observation, 1),
		started: time.Now(),
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	relay.record(observation{
		at: time.Now(), client: netip.MustParseAddr("127.0.0.1"),
		msg:      []byte{0xff},
		haveQ:    true,
		question: Question{Name: "example.com", TypeName: "A"},
	})

	if got := relay.counters.Unparsed.Load(); got != 1 {
		t.Errorf("Unparsed = %d, want 1", got)
	}
	queries := relay.Queries()
	if len(queries) != 1 || queries[0].Rcode != "UNPARSED" {
		t.Errorf("the query was dropped from the log entirely: %+v", queries)
	}
}

func TestObservationSocketServesTheLog(t *testing.T) {
	up := newFakeUpstream(t, func(query []byte) []byte {
		out := buildResponse(t, 0, "example.com.", dnsmessage.TypeA, dnsmessage.RCodeSuccess, []answer{
			{owner: "example.com.", ttl: 300, a: [4]byte{1, 2, 3, 4}},
		})
		copy(out[:2], query[:2])
		return out
	})

	cfg := loopbackConfig(t, up.addr())
	cfg.QueryLogEntries = 16
	cfg.ObserveSocket = socketPath(t)
	_, at := startRelay(t, cfg, nil)

	ask(t, at, buildQuery(t, 3, "example.com.", dnsmessage.TypeA))

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", cfg.ObserveSocket)
		},
	}}

	deadline := time.Now().Add(3 * time.Second)
	var body QueriesResponse
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://relay/queries")
		if err != nil {
			t.Fatal(err)
		}
		err = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if len(body.Queries) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if len(body.Queries) != 1 || body.Queries[0].Name != "example.com" {
		t.Errorf("the socket did not serve the query log: %+v", body.Queries)
	}
	if body.Stats.Since.IsZero() {
		t.Error("the reply does not say when the log started, so it implies a history it lacks")
	}
	if body.Stats.Capacity != 16 {
		t.Errorf("Capacity = %d, want 16", body.Stats.Capacity)
	}
}

// The query log is a record of what everybody in the building looked up. It is
// not something to leave world-readable because the default mode said so.
func TestObservationSocketIsOwnerOnly(t *testing.T) {
	up := newFakeUpstream(t, func(query []byte) []byte { return query })
	cfg := loopbackConfig(t, up.addr())
	cfg.ObserveSocket = socketPath(t)
	startRelay(t, cfg, nil)

	info, err := os.Stat(cfg.ObserveSocket)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("socket mode = %o, want 600", mode)
	}
}

// A socket left behind by a killed process would otherwise make every start
// fail to bind, which under Restart=always is a crash loop rather than a
// one-off.
func TestObservationSocketReplacesAStaleFile(t *testing.T) {
	up := newFakeUpstream(t, func(query []byte) []byte { return query })
	cfg := loopbackConfig(t, up.addr())
	cfg.ObserveSocket = socketPath(t)

	if err := os.WriteFile(cfg.ObserveSocket, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	startRelay(t, cfg, nil)

	// Binding succeeded, which is the assertion; confirm it is really a socket
	// now rather than the regular file that was there.
	info, err := os.Stat(cfg.ObserveSocket)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Error("the stale file was not replaced by a socket")
	}
}

func TestNewRefusesAConfigThatCannotServe(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if _, err := New(Config{Upstream: netip.MustParseAddrPort("127.0.0.1:5353")}, logger); err == nil {
		t.Error("a relay with nowhere to listen was accepted")
	}
	if _, err := New(Config{Listen: []netip.AddrPort{freePort(t)}}, logger); err == nil {
		t.Error("a relay with no upstream was accepted")
	}
}

func TestLoadPoliciesTreatsAMissingDirectoryAsNone(t *testing.T) {
	got, err := LoadPolicies(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("a missing policy directory was an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d policies", len(got))
	}
}

// Sorted, so two boxes with the same files compile the same set and a directory
// read order cannot change which default policy wins.
func TestLoadPoliciesIsOrdered(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"zulu", "alpha", "mike"} {
		data, err := MarshalPolicy(Policy{Name: name})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A non-JSON file in the directory is not ours and must be ignored.
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadPolicies(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "mike", "zulu"}
	if len(got) != len(want) {
		t.Fatalf("loaded %d policies, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i] {
			t.Errorf("policy %d = %q, want %q", i, got[i].Name, want[i])
		}
	}
}

func TestTCPFraming(t *testing.T) {
	var buf bytes.Buffer
	conn := &pipeConn{r: &buf, w: &buf}

	msg := []byte("a DNS message, notionally")
	if err := writeTCPMessage(conn, msg); err != nil {
		t.Fatal(err)
	}
	// One write, not two: a separate write for the prefix produces a
	// two-packet answer that some clients handle poorly.
	if got := binary.BigEndian.Uint16(buf.Bytes()[:2]); int(got) != len(msg) {
		t.Errorf("length prefix = %d, want %d", got, len(msg))
	}

	got, err := readTCPMessage(conn)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, msg) {
		t.Errorf("round trip = %q, want %q", got, msg)
	}
}

// pipeConn is the minimum net.Conn the framing helpers touch.
type pipeConn struct {
	r io.Reader
	w io.Writer
}

func (p *pipeConn) Read(b []byte) (int, error)       { return p.r.Read(b) }
func (p *pipeConn) Write(b []byte) (int, error)      { return p.w.Write(b) }
func (p *pipeConn) Close() error                     { return nil }
func (p *pipeConn) LocalAddr() net.Addr              { return nil }
func (p *pipeConn) RemoteAddr() net.Addr             { return nil }
func (p *pipeConn) SetDeadline(time.Time) error      { return nil }
func (p *pipeConn) SetReadDeadline(time.Time) error  { return nil }
func (p *pipeConn) SetWriteDeadline(time.Time) error { return nil }
