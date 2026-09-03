package dns

import (
	"encoding/json"
	"net/netip"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// docWith builds a configuration document holding this module's subtree, or
// none at all when raw is nil.
func docWith(t *testing.T, raw []byte) core.Document {
	t.Helper()
	var d core.Document
	if raw != nil {
		d.Set(ModuleName, raw)
	}
	return d
}

func mustPrefix(t *testing.T, s string) netip.Prefix {
	t.Helper()
	p, err := netip.ParsePrefix(s)
	if err != nil {
		t.Fatalf("ParsePrefix(%q): %v", s, err)
	}
	return p
}

func mustAddrPort(t *testing.T, s string) netip.AddrPort {
	t.Helper()
	ap, err := netip.ParseAddrPort(s)
	if err != nil {
		t.Fatalf("ParseAddrPort(%q): %v", s, err)
	}
	return ap
}

// Rendering is deterministic only if the input order is, and drift detection
// compares rendered bytes. Without a canonical order, reordering a JSON array
// would read as drift and schedule a restart of the resolver.
func TestNormalizeIsCanonical(t *testing.T) {
	a := Config{
		Listen: []netip.AddrPort{
			mustAddrPort(t, "192.168.1.1:53"), mustAddrPort(t, "10.0.0.1:53"),
		},
		AllowFrom: []netip.Prefix{
			mustPrefix(t, "192.168.1.0/24"), mustPrefix(t, "10.0.0.0/8"),
			mustPrefix(t, "192.168.1.0/24"), // a duplicate
		},
		Policies: []Policy{
			{Name: "kids", Block: []string{"B.example.COM", "a.example.com."}},
			{Name: "default"},
		},
		Hijack: Hijack{Interfaces: []string{"lan1", "lan0", "lan1"}},
	}
	b := Config{
		Listen: []netip.AddrPort{
			mustAddrPort(t, "10.0.0.1:53"), mustAddrPort(t, "192.168.1.1:53"),
		},
		AllowFrom: []netip.Prefix{
			mustPrefix(t, "10.0.0.0/8"), mustPrefix(t, "192.168.1.0/24"),
		},
		Policies: []Policy{
			{Name: "default"},
			{Name: "kids", Block: []string{"a.example.com", "*.b.example.com"}},
		},
		Hijack: Hijack{Interfaces: []string{"lan0", "lan1"}},
	}

	a.Normalize()
	b.Normalize()

	ja, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	jb, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(ja) != string(jb) {
		t.Errorf("two spellings of one config normalised differently:\n a: %s\n b: %s", ja, jb)
	}
}

// "*.example.com" and "example.com" block the same names, so keeping both
// spellings would be two entries that diff against each other forever.
func TestNormalizeNameStripsTheWildcardAndTheRootDot(t *testing.T) {
	for _, in := range []string{"Example.COM", "example.com.", "*.example.com", "  example.com  "} {
		if got := NormalizeName(in); got != "example.com" {
			t.Errorf("NormalizeName(%q) = %q, want example.com", in, got)
		}
	}
}

func TestUnmarshalRejectsUnknownFields(t *testing.T) {
	// A mistyped key that silently did nothing is the worst outcome this module
	// has: a blocklist that did not load looks exactly like a blocklist with
	// nothing on it.
	_, err := UnmarshalConfig([]byte(`{"enabled":true,"blocklist":["x"]}`))
	if err == nil {
		t.Fatal("an unknown field was accepted")
	}
	if !strings.Contains(err.Error(), "blocklist") {
		t.Errorf("the error does not name the offending field: %v", err)
	}
}

func TestConfigRoundTrips(t *testing.T) {
	want := Config{
		Enabled:   true,
		Listen:    []netip.AddrPort{mustAddrPort(t, "192.168.1.1:53")},
		AllowFrom: []netip.Prefix{mustPrefix(t, "192.168.1.0/24")},
		Upstream: Upstream{
			Mode:    ModeForward,
			Servers: []netip.AddrPort{mustAddrPort(t, "1.1.1.1:853")},
			TLS:     true,
			TLSName: "cloudflare-dns.com",
		},
		Policies: []Policy{{
			Name:     "kids",
			Clients:  []netip.Prefix{mustPrefix(t, "192.168.1.50/32")},
			Block:    []string{"example.com"},
			Response: RespondZero,
		}},
		Hijack:   Hijack{Enabled: true, Interfaces: []string{"lan0"}, BlockDoT: true},
		QueryLog: QueryLog{Enabled: true, Entries: 100},
	}

	data, err := MarshalConfig(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalConfig(data)
	if err != nil {
		t.Fatal(err)
	}

	regot, err := MarshalConfig(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(regot) {
		t.Errorf("round trip changed the document:\n before: %s\n after:  %s", data, regot)
	}
}

// netip.Addr marshals as a string but reflects as an empty object; a duration
// wrapper reflects as an integer. Both traps are core's to fix, and this asserts
// the wire form this module actually publishes rather than trusting it.
func TestAddressesMarshalAsStrings(t *testing.T) {
	data, err := MarshalConfig(Config{
		Listen:    []netip.AddrPort{mustAddrPort(t, "192.168.1.1:53")},
		AllowFrom: []netip.Prefix{mustPrefix(t, "192.168.1.0/24")},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"192.168.1.1:53"`, `"192.168.1.0/24"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("rendered config does not contain %s:\n%s", want, data)
		}
	}
}

// A document without a "dns" key means the module has never been configured,
// which is a legitimate state and exactly what a fresh install looks like.
func TestFromDocumentTreatsAnAbsentSectionAsUnconfigured(t *testing.T) {
	cfg, err := FromDocument(docWith(t, nil))
	if err != nil {
		t.Fatalf("an absent section was an error: %v", err)
	}
	if cfg.Enabled || len(cfg.Listen) != 0 {
		t.Errorf("an absent section produced a non-zero config: %+v", cfg)
	}
}

func TestCloneDoesNotShareBackingArrays(t *testing.T) {
	original := Config{
		Listen:    []netip.AddrPort{mustAddrPort(t, "192.168.1.1:53")},
		AllowFrom: []netip.Prefix{mustPrefix(t, "192.168.1.0/24")},
		Upstream:  Upstream{Servers: []netip.AddrPort{mustAddrPort(t, "1.1.1.1:53")}},
		Policies: []Policy{{
			Name:    "kids",
			Clients: []netip.Prefix{mustPrefix(t, "192.168.1.50/32")},
			Block:   []string{"example.com"},
		}},
		Hijack: Hijack{Interfaces: []string{"lan0"}},
	}

	clone := original.Clone()
	clone.Listen[0] = mustAddrPort(t, "10.0.0.1:53")
	clone.Policies[0].Block[0] = "other.example"
	clone.Hijack.Interfaces[0] = "lan9"

	if original.Listen[0].String() != "192.168.1.1:53" {
		t.Error("editing the clone changed the original's listen address")
	}
	if original.Policies[0].Block[0] != "example.com" {
		t.Error("editing the clone changed the original's blocklist")
	}
	if original.Hijack.Interfaces[0] != "lan0" {
		t.Error("editing the clone changed the original's interfaces")
	}
}

func TestPolicyAccessors(t *testing.T) {
	var c Config
	c.SetPolicy(Policy{Name: "kids", Block: []string{"a.example"}})
	c.SetPolicy(Policy{Name: "default"})

	if _, ok := c.Policy("kids"); !ok {
		t.Fatal("SetPolicy did not store the policy")
	}
	// The default policy is the one with no clients — the row an operator reads
	// as "everyone else".
	def, ok := c.DefaultPolicy()
	if !ok || def.Name != "default" {
		t.Errorf("DefaultPolicy() = %+v, %v", def, ok)
	}

	c.SetPolicy(Policy{Name: "kids", Block: []string{"b.example"}})
	if p, _ := c.Policy("kids"); len(p.Block) != 1 || p.Block[0] != "b.example" {
		t.Errorf("SetPolicy did not replace by name: %+v", p)
	}
	if len(c.Policies) != 2 {
		t.Errorf("SetPolicy added a duplicate: %d policies", len(c.Policies))
	}

	if !c.RemovePolicy("kids") || c.RemovePolicy("kids") {
		t.Error("RemovePolicy did not report what it did")
	}
}

// The redirect target is derived per family, never configured. A v4-only
// redirect on a dual-stack network leaks every query a client sends over IPv6.
func TestRedirectTargetIsPerFamily(t *testing.T) {
	c := Config{Listen: []netip.AddrPort{
		mustAddrPort(t, "192.168.1.1:53"),
		mustAddrPort(t, "[fd00::1]:53"),
	}}
	c.Normalize()

	v4, ok := c.RedirectTarget(false)
	if !ok || !v4.Addr().Is4() {
		t.Errorf("RedirectTarget(v4) = %v, %v", v4, ok)
	}
	v6, ok := c.RedirectTarget(true)
	if !ok || v6.Addr().Is4() {
		t.Errorf("RedirectTarget(v6) = %v, %v", v6, ok)
	}

	only4 := Config{Listen: []netip.AddrPort{mustAddrPort(t, "192.168.1.1:53")}}
	if _, ok := only4.RedirectTarget(true); ok {
		t.Error("a v4-only config claimed to have a v6 redirect target")
	}
}

func TestQueryLogEntriesDefault(t *testing.T) {
	if got := (QueryLog{}).EntriesOrDefault(); got != DefaultQueryLogEntries {
		t.Errorf("EntriesOrDefault() = %d, want %d", got, DefaultQueryLogEntries)
	}
	if got := (QueryLog{Entries: 10}).EntriesOrDefault(); got != 10 {
		t.Errorf("EntriesOrDefault() = %d, want 10", got)
	}
}

func TestVocabularyDefaults(t *testing.T) {
	// The empty string is a legal stored value for both enums, and it has to
	// resolve to the documented default rather than to nothing.
	if got := UpstreamMode("").OrDefault(); got != ModeRecurse {
		t.Errorf("empty upstream mode = %q, want %q", got, ModeRecurse)
	}
	if got := BlockResponse("").OrDefault(); got != RespondNXDOMAIN {
		t.Errorf("empty block response = %q, want %q", got, RespondNXDOMAIN)
	}
	if !UpstreamMode("").Valid() || !BlockResponse("").Valid() {
		t.Error("the empty string must be a valid stored value for both enums")
	}
	if UpstreamMode("nonsense").Valid() || BlockResponse("nonsense").Valid() {
		t.Error("an unknown value was accepted as valid")
	}
}
