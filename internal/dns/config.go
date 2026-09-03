package dns

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// ResolverPort is the loopback port unbound answers on.
//
// Not 53, and that is the whole shape of this module: olr owns :53 so that it
// sees every query with the client's address attached, and unbound sits behind
// it doing the part we have no business writing — recursion, DNSSEC, caching
// (docs/dns.md §4).
const ResolverPort = 5353

// DefaultResolver is where the relay forwards when nothing says otherwise.
var DefaultResolver = netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), ResolverPort)

// DefaultQueryLogEntries is how many answered queries the relay keeps.
//
// Deliberately a count and not a duration. The log lives in the relay's memory
// (docs/dns.md §7.5 is open about where it should eventually live), so the
// number that matters is the one bounding the memory, and an operator can
// reason about "the last 5000 queries" without knowing the query rate.
const DefaultQueryLogEntries = 5000

// Config is the dns module's intent — what the operator asked for, not what is
// running. It is the single source for the CLI flags, the REST body, the UI
// form, and the MCP tool definition (design.md §3.2 rule 3), so a field added
// here appears on every surface without further work.
//
// Fields without `omitempty` are reflected as schema-required (design.md §10,
// config format), so the tags are load-bearing.
type Config struct {
	// Enabled controls whether DNS is served at all. Disabling stops both the
	// relay and the resolver but keeps the configuration, so it can be turned
	// back on without retyping it.
	//
	// Global rather than per-group, matching internal/dhcp. design.md §5.6
	// wants per-group `auto | on | off`; groups do not exist until the link
	// module lands, and inventing a second spelling here would be one more
	// thing to unpick then.
	Enabled bool `json:"enabled"`

	// Listen is where the relay answers queries.
	//
	// Explicit addresses, never a wildcard. A resolver reachable from the
	// internet is an amplifier (docs/dns.md §5), and the difference between
	// "0.0.0.0 plus a firewall rule" and "the LAN address" is one missing rule.
	Listen []netip.AddrPort `json:"listen,omitempty"`

	// AllowFrom are the source prefixes permitted to ask. Anything else is
	// dropped without an answer.
	//
	// The second half of the same defence, and it is not redundant: an address
	// bound to a LAN interface still answers a packet routed to it from
	// somewhere else. Empty means the relay derives it from the listen
	// addresses' own subnets rather than defaulting open — see Validate.
	AllowFrom []netip.Prefix `json:"allow_from,omitempty"`

	// Upstream is how names actually get resolved.
	Upstream Upstream `json:"upstream"`

	// Policies decide what a given client is allowed to look up. A policy with
	// no Clients is the default one, applying to everybody not matched by
	// another.
	Policies []Policy `json:"policies,omitempty"`

	// Hijack redirects clients that did not ask us, and closes the encrypted
	// side doors they would otherwise use instead.
	Hijack Hijack `json:"hijack"`

	// QueryLog is the observation half — the reason to own :53 at all.
	QueryLog QueryLog `json:"query_log"`

	// ExtraConf is the module's declared escape hatch (design.md §3.2 rule 5,
	// which names this field): appended verbatim to the rendered unbound
	// config. It is revisioned with everything else, which is the whole point —
	// editing the daemon's file out of band is what this exists to make
	// unnecessary.
	//
	// It may not set directives the module renders itself; see validateExtra.
	ExtraConf string `json:"extra_unbound_conf,omitempty"`
}

// Upstream is where unbound gets its answers.
type Upstream struct {
	// Mode is recursion or forwarding. Empty means ModeRecurse.
	Mode UpstreamMode `json:"mode,omitempty"`

	// Servers are the forwarders, used only in ModeForward. A server without a
	// port means 53, or 853 when TLS is set.
	Servers []netip.AddrPort `json:"servers,omitempty"`

	// TLS forwards over DoT rather than plaintext.
	//
	// Worth having rather than a nicety: forwarding in plaintext to a public
	// resolver hands every name the house looks up to whoever carries the
	// traffic, which is most of what an operator thought they were avoiding by
	// choosing that resolver. It is ignored in ModeRecurse, where there is no
	// forwarder to protect.
	TLS bool `json:"tls,omitempty"`

	// TLSName is the certificate name to require, e.g. "cloudflare-dns.com".
	// Without it unbound will not authenticate the upstream, so TLS buys
	// encryption and not identity.
	TLSName string `json:"tls_name,omitempty"`
}

// UpstreamMode selects how unbound resolves.
type UpstreamMode string

const (
	// ModeRecurse walks the DNS from the root. No third party sees the whole
	// picture of what this house looks up, which is the privacy argument, and
	// there is no forwarder to be down, which is the availability one.
	ModeRecurse UpstreamMode = "recurse"

	// ModeForward sends everything to the named servers. Faster from cold on a
	// small network, and the only option where an upstream's own filtering is
	// wanted.
	ModeForward UpstreamMode = "forward"
)

// UpstreamModes lists the vocabulary, for flag help and schema enums.
func UpstreamModes() []UpstreamMode { return []UpstreamMode{ModeRecurse, ModeForward} }

// Valid reports whether m is a known mode. The empty string is valid and means
// ModeRecurse.
func (m UpstreamMode) Valid() bool { return m == "" || slices.Contains(UpstreamModes(), m) }

// OrDefault resolves the empty string.
func (m UpstreamMode) OrDefault() UpstreamMode {
	if m == "" {
		return ModeRecurse
	}
	return m
}

// Policy is what one set of clients may look up.
//
// Blocking lives here — in olr's own relay — rather than in unbound's views,
// which is a departure from docs/dns.md §6's "blocking is native and cheap" and
// is forced by the rest of that document. unbound selects a view by the
// client's netblock; once the relay owns :53, every query reaches unbound from
// 127.0.0.1 and all views collapse into one. The relay is the only thing left
// that still knows who asked. §4.1 already anticipates the cost: "returning
// NXDOMAIN or an override requires synthesising a response. Small, and the only
// code path that must be exactly right."
type Policy struct {
	// Name identifies the policy on every surface, and names the file it is
	// rendered into.
	Name string `json:"name"`

	// Clients are the source prefixes this policy governs. The most specific
	// matching prefix wins, so a /32 for one tablet beats a /24 for the house.
	//
	// Empty makes this the default policy — the one applying to every client no
	// other policy claims. At most one policy may be the default.
	Clients []netip.Prefix `json:"clients,omitempty"`

	// Block are the names this policy refuses.
	//
	// A pattern covers the name and everything under it: "example.com" blocks
	// example.com and www.example.com alike. That is the reading an operator
	// means, and exact-only matching would be a support question on the first
	// day. A leading "*." is accepted and means the same thing.
	Block []string `json:"block,omitempty"`

	// Allow are exceptions that beat Block, so "block social media except the
	// one site the school uses" is expressible without inverting the list.
	Allow []string `json:"allow,omitempty"`

	// Response is what a blocked name answers with. Empty means RespondNXDOMAIN.
	Response BlockResponse `json:"response,omitempty"`
}

// BlockResponse is how a refusal is spelled on the wire.
type BlockResponse string

const (
	// RespondNXDOMAIN says the name does not exist. The honest answer in DNS
	// terms, and the one clients cache and back off from.
	RespondNXDOMAIN BlockResponse = "nxdomain"

	// RespondZero answers 0.0.0.0 and ::. Less honest, and chosen anyway on
	// some networks because an app that treats NXDOMAIN as "the network is
	// down" will retry forever, where a connection refused to 0.0.0.0 fails
	// immediately and visibly.
	RespondZero BlockResponse = "zero"
)

// BlockResponses lists the vocabulary, for flag help and schema enums.
func BlockResponses() []BlockResponse { return []BlockResponse{RespondNXDOMAIN, RespondZero} }

// Valid reports whether r is a known response. The empty string is valid and
// means RespondNXDOMAIN.
func (r BlockResponse) Valid() bool { return r == "" || slices.Contains(BlockResponses(), r) }

// OrDefault resolves the empty string.
func (r BlockResponse) OrDefault() BlockResponse {
	if r == "" {
		return RespondNXDOMAIN
	}
	return r
}

// Hijack makes the resolver leg true rather than merely advertised.
//
// DHCP option 6 is advice; a device is free to ignore it and hardcode a public
// resolver, and the common case is not even adversarial — browsers and Apple
// devices upgrade themselves to DoH by default (docs/dns.md §2.2). A device
// that resolves elsewhere is not merely unlogged: the proxy that was supposed
// to route it by domain never receives a name it recognises, so domain policy
// silently does not apply. That silence is the whole reason this exists.
type Hijack struct {
	// Enabled renders the nftables table at all.
	Enabled bool `json:"enabled"`

	// Interfaces are the LAN-side interfaces whose forwarded traffic is
	// captured. Kernel interface names, for the same reason internal/dhcp keys
	// pools by them: groups arrive with the link module.
	//
	// Empty with Enabled set is a configuration error rather than "everything",
	// because the everything reading would capture the WAN side too.
	Interfaces []string `json:"interfaces,omitempty"`

	// There is deliberately no "redirect to" field. The address hijacked
	// traffic is sent to is always one the relay is listening on, derived per
	// family from Listen — so the one way to get this catastrophically wrong,
	// redirecting the whole network's DNS at an address nothing answers on, is
	// not expressible.

	// BlockDoT drops DNS-over-TLS on :853.
	//
	// A drop and not a reject, deliberately: a rejected connection tells the
	// client to fall back immediately, where a black hole makes it wait out a
	// timeout and then use the resolver it was given. The rude answer is the
	// one that works.
	BlockDoT bool `json:"block_dot,omitempty"`
}

// QueryLog controls the observation half.
type QueryLog struct {
	// Enabled records answered queries.
	Enabled bool `json:"enabled"`

	// Entries is how many to keep. Zero means DefaultQueryLogEntries.
	Entries int `json:"entries,omitempty"`
}

// EntriesOrDefault resolves the zero value.
func (q QueryLog) EntriesOrDefault() int {
	if q.Entries <= 0 {
		return DefaultQueryLogEntries
	}
	return q.Entries
}

// DefaultPolicy returns the policy governing clients no other policy claims.
func (c Config) DefaultPolicy() (Policy, bool) {
	i := slices.IndexFunc(c.Policies, func(p Policy) bool { return len(p.Clients) == 0 })
	if i < 0 {
		return Policy{}, false
	}
	return c.Policies[i], true
}

// Policy returns a policy by name.
func (c Config) Policy(name string) (Policy, bool) {
	i := slices.IndexFunc(c.Policies, func(p Policy) bool { return p.Name == name })
	if i < 0 {
		return Policy{}, false
	}
	return c.Policies[i], true
}

// SetPolicy adds or replaces a policy, keyed by name.
func (c *Config) SetPolicy(p Policy) {
	if i := slices.IndexFunc(c.Policies, func(e Policy) bool { return e.Name == p.Name }); i >= 0 {
		c.Policies[i] = p
		c.Normalize()
		return
	}
	c.Policies = append(c.Policies, p)
	c.Normalize()
}

// RemovePolicy drops a policy by name, reporting whether there was one.
func (c *Config) RemovePolicy(name string) bool {
	i := slices.IndexFunc(c.Policies, func(p Policy) bool { return p.Name == name })
	if i < 0 {
		return false
	}
	c.Policies = slices.Delete(c.Policies, i, i+1)
	return true
}

// RedirectTarget resolves where hijacked :53 is sent for one address family.
//
// Per family, because a v4-only redirect on a dual-stack network leaks every
// query a client chooses to send over IPv6 — the same failure the routing model
// records for a v4-only exit, and just as invisible.
func (c Config) RedirectTarget(v6 bool) (netip.AddrPort, bool) {
	for _, l := range c.Listen {
		if l.Addr().Is4() == !v6 {
			return l, true
		}
	}
	return netip.AddrPort{}, false
}

// NormalizeName puts a blocklist pattern in canonical form: lowercased, with
// the trailing root dot and any leading "*." removed.
//
// The wildcard is stripped rather than honoured because it does not mean
// anything different — a pattern already covers everything beneath it — and
// keeping both spellings would make "example.com" and "*.example.com" two
// entries that block the same names and diff against each other forever.
func NormalizeName(s string) string {
	n := strings.ToLower(strings.TrimSpace(s))
	n = strings.TrimSuffix(n, ".")
	n = strings.TrimPrefix(n, "*.")
	return n
}

// Normalize puts the config in canonical form.
//
// Everything downstream depends on this. Rendering is deterministic only if the
// input order is, and drift detection compares rendered bytes — so without a
// canonical order, reordering a JSON array would read as drift.
//
// It deliberately does not validate; a malformed name is left as-is for
// Validate to report with a proper path.
func (c *Config) Normalize() {
	c.Upstream.TLSName = strings.TrimSpace(c.Upstream.TLSName)
	slices.SortStableFunc(c.Upstream.Servers, compareAddrPort)

	slices.SortStableFunc(c.Listen, compareAddrPort)
	slices.SortStableFunc(c.AllowFrom, comparePrefix)
	c.AllowFrom = slices.CompactFunc(c.AllowFrom, func(a, b netip.Prefix) bool { return a == b })

	for i := range c.Policies {
		p := &c.Policies[i]
		p.Name = strings.TrimSpace(p.Name)
		p.Block = normalizeNames(p.Block)
		p.Allow = normalizeNames(p.Allow)
		slices.SortStableFunc(p.Clients, comparePrefix)
		p.Clients = slices.CompactFunc(p.Clients, func(a, b netip.Prefix) bool { return a == b })
	}
	slices.SortStableFunc(c.Policies, func(a, b Policy) int { return strings.Compare(a.Name, b.Name) })

	for i := range c.Hijack.Interfaces {
		c.Hijack.Interfaces[i] = strings.TrimSpace(c.Hijack.Interfaces[i])
	}
	slices.Sort(c.Hijack.Interfaces)
	c.Hijack.Interfaces = slices.Compact(c.Hijack.Interfaces)
}

// normalizeNames canonicalises, sorts and de-duplicates a name list, dropping
// empties.
func normalizeNames(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, n := range in {
		if norm := NormalizeName(n); norm != "" {
			out = append(out, norm)
		}
	}
	slices.Sort(out)
	out = slices.Compact(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

// compareAddrPort orders addresses then ports, so a sort is total.
func compareAddrPort(a, b netip.AddrPort) int {
	if n := a.Addr().Compare(b.Addr()); n != 0 {
		return n
	}
	return int(a.Port()) - int(b.Port())
}

// comparePrefix orders by address then by mask length.
func comparePrefix(a, b netip.Prefix) int {
	if n := a.Addr().Compare(b.Addr()); n != 0 {
		return n
	}
	return a.Bits() - b.Bits()
}

// Clone deep-copies the config, so that a proposed change can be diffed against
// the current one without either sharing backing arrays.
func (c Config) Clone() Config {
	out := c
	out.Listen = slices.Clone(c.Listen)
	out.AllowFrom = slices.Clone(c.AllowFrom)
	out.Upstream.Servers = slices.Clone(c.Upstream.Servers)
	out.Hijack.Interfaces = slices.Clone(c.Hijack.Interfaces)
	out.Policies = make([]Policy, len(c.Policies))
	for i, p := range c.Policies {
		p.Clients = slices.Clone(p.Clients)
		p.Block = slices.Clone(p.Block)
		p.Allow = slices.Clone(p.Allow)
		out.Policies[i] = p
	}
	if len(c.Policies) == 0 {
		out.Policies = nil
	}
	return out
}

// MarshalConfig renders the config as the module's subtree of the document.
//
// It does not stamp a "$schema" key: the document has exactly one, written by
// the store (core.SchemaURL). Indentation is likewise not ours — the store
// re-indents the whole document in one pass.
func MarshalConfig(c Config) ([]byte, error) {
	c.Normalize()
	return json.Marshal(c)
}

// UnmarshalConfig parses a config, rejecting unknown fields.
//
// Strictness is deliberate: a typo'd key that is silently ignored produces a
// resolver that is quietly not doing what its config says. For this module that
// is worse than for most — a blocklist that did not load looks exactly like a
// blocklist with nothing on it.
func UnmarshalConfig(data []byte) (Config, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("parsing dns config: %w", err)
	}
	c.Normalize()
	return c, nil
}

// FromDocument reads this module's subtree out of the configuration document.
//
// A document without a "dns" key is not an error — it means the module has
// never been configured, which is a legitimate state and exactly what a fresh
// install looks like.
func FromDocument(d core.Document) (Config, error) {
	raw, ok := d.Raw(ModuleName)
	if !ok {
		return Config{}, nil
	}
	c, err := UnmarshalConfig(raw)
	if err != nil {
		return Config{}, fmt.Errorf("%s configuration: %w", ModuleName, err)
	}
	return c, nil
}
