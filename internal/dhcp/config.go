package dhcp

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// DefaultLeaseTime applies to any pool that does not set one. dnsmasq's own
// default is one hour, which is short enough that a busy LAN renews constantly.
const DefaultLeaseTime = Duration(12 * time.Hour)

// MinLeaseTime is dnsmasq's floor, not ours: it rejects anything shorter.
const MinLeaseTime = Duration(2 * time.Minute)

// Config is the dhcp module's intent — what the operator asked for, not what is
// running. It is the single source for the CLI flags, the REST body, the UI
// form, and the MCP tool definition (design.md §3.2 rule 3), so a field added
// here appears on every surface without further work.
//
// Fields without `omitempty` are reflected as schema-required (design.md §10,
// config format), so the tags are load-bearing.
type Config struct {
	// Enabled controls whether the DHCP server runs at all. Disabling stops the
	// service but keeps the configuration, so it can be turned back on without
	// retyping it.
	Enabled bool `json:"enabled"`

	// Pools holds at most one address pool per interface (design.md — the
	// interface is the pool's identity).
	Pools []Pool `json:"pools,omitempty"`

	// Reservations are deliberately global rather than nested under a pool.
	// dnsmasq matches a dhcp-host by MAC across every range at once, so
	// modelling them per-pool would invent an ownership that the daemon does
	// not actually have.
	Reservations []Reservation `json:"reservations,omitempty"`

	// ExtraConf is the module's declared escape hatch (design.md §3.2 rule 5):
	// appended verbatim to the rendered dnsmasq config. It is revisioned with
	// everything else, which is the whole point — editing the daemon's file out
	// of band is what this exists to make unnecessary.
	//
	// It may not set directives the module renders itself; see validateExtra.
	ExtraConf string `json:"extra_dnsmasq_conf,omitempty"`
}

// Pool is one interface's dynamic address range.
type Pool struct {
	// Interface is the pool's primary key. It must name an interface that has
	// been adopted (design.md §7) — we never serve DHCP on something the
	// operator did not hand us.
	Interface string `json:"interface"`

	// Start and End bound the dynamic range, inclusive. Both must fall inside
	// one of the interface's own prefixes; validate.go enforces that against
	// the link module rather than trusting what is typed here.
	Start netip.Addr `json:"start"`
	End   netip.Addr `json:"end"`

	// LeaseTime is zero for DefaultLeaseTime.
	LeaseTime Duration `json:"lease_time,omitempty"`

	// Gateway is nil for "the router's own address on this interface", which is
	// what almost every deployment wants. Set it only to hand clients a
	// different next hop.
	Gateway *netip.Addr `json:"gateway,omitempty"`

	// DNS is nil for "the router itself". Note that the router answering DNS is
	// the dns module's job, not ours (design.md §4.2) — we only advertise the
	// address.
	DNS []netip.Addr `json:"dns,omitempty"`

	// Domain is DHCP option 15, the client's search domain.
	Domain string `json:"domain,omitempty"`

	// NTP is DHCP option 42.
	NTP []netip.Addr `json:"ntp,omitempty"`

	// RA controls IPv6 router advertisement and DHCPv6 on this interface.
	// Empty means RAOff.
	RA RAMode `json:"ra,omitempty"`

	// Options are additional DHCP options, the per-pool escape hatch. Rendered
	// into the reloadable options directory, so changing one does not restart
	// the daemon.
	Options []Option `json:"options,omitempty"`
}

// LeaseTimeOrDefault resolves the zero value.
func (p Pool) LeaseTimeOrDefault() Duration {
	if p.LeaseTime <= 0 {
		return DefaultLeaseTime
	}
	return p.LeaseTime
}

// Reservation pins one client to one address.
type Reservation struct {
	// MAC is the client's hardware address. Normalize lowercases it so that
	// diffs do not churn on capitalisation.
	MAC string `json:"mac"`

	// IP must sit in the same subnet as some pool, though dnsmasq explicitly
	// allows it to fall outside that pool's dynamic range — and outside is the
	// better habit, since it cannot then collide with a dynamic allocation.
	IP netip.Addr `json:"ip"`

	// Hostname is optional. When set it is also the name dnsmasq will use for
	// the client, which is what the dns module will later publish.
	Hostname string `json:"hostname,omitempty"`

	// LeaseTime is zero for the enclosing pool's lease time.
	LeaseTime Duration `json:"lease_time,omitempty"`
}

// Option is one raw DHCP option.
type Option struct {
	// Option is either a number ("42") or one of dnsmasq's option names
	// ("ntp-server"). We pass it through rather than maintaining our own table
	// of the ~100 DHCP options, which would go stale and add nothing.
	Option string `json:"option"`

	// Value is passed through verbatim, commas and all, so multi-value options
	// work without special-casing.
	Value string `json:"value"`
}

// RAMode selects IPv6 behaviour on a pool's interface.
//
// IPv6 is a dimension, not a module (design.md §4.3), so it lives here as a
// field rather than anywhere more ceremonious.
type RAMode string

// Each mode names a documented dnsmasq behaviour rather than a synonym for one,
// so there is no gap between what the field promises and what the daemon does.
const (
	// RAOff serves no IPv6 at all.
	RAOff RAMode = "off"

	// RASLAAC advertises the prefix, lets clients self-assign an address from
	// it, and answers DHCPv6 information requests so they still get DNS and
	// NTP from us. dnsmasq's ra-stateless: the O and A bits.
	//
	// This is the mode most networks want, and it needs nothing from the dial
	// module — dnsmasq's constructor: syntax derives the prefix from the
	// interface's own address, so a delegated prefix that changes is followed
	// automatically instead of having to be re-rendered.
	RASLAAC RAMode = "slaac"

	// RAStateful additionally hands out addresses over DHCPv6, for networks
	// that want the same address-to-client record IPv4 has. Clients end up with
	// both a DHCPv6 and a SLAAC address.
	RAStateful RAMode = "stateful"
)

// RAModes lists the vocabulary, for flag help and schema enums.
func RAModes() []RAMode { return []RAMode{RAOff, RASLAAC, RAStateful} }

// Valid reports whether m is a known mode. The empty string is valid and means
// RAOff.
func (m RAMode) Valid() bool { return m == "" || slices.Contains(RAModes(), m) }

// OrDefault resolves the empty string.
func (m RAMode) OrDefault() RAMode {
	if m == "" {
		return RAOff
	}
	return m
}

// Duration is a time.Duration that round-trips through JSON as a string.
//
// Without the wrapper a 12 hour lease serialises as 43200000000000, which is
// unreadable in a config file and hostile in an API response (design.md §10,
// config format).
type Duration time.Duration

// Duration converts back to the stdlib type.
func (d Duration) Duration() time.Duration { return time.Duration(d) }

// durationUnits covers the units dnsmasq itself accepts for a lease time.
// time.ParseDuration knows nothing of days or weeks, and a two-day lease is an
// entirely ordinary thing to want.
var durationUnits = map[byte]time.Duration{
	's': time.Second,
	'm': time.Minute,
	'h': time.Hour,
	'd': 24 * time.Hour,
	'w': 7 * 24 * time.Hour,
}

// ParseDuration reads "12h", "45m", "2d", "1w30m", or a bare number of seconds.
func ParseDuration(s string) (Duration, error) {
	rest := strings.TrimSpace(s)
	if rest == "" {
		return 0, fmt.Errorf("empty duration")
	}
	var total time.Duration
	for rest != "" {
		i := 0
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			i++
		}
		if i == 0 {
			return 0, fmt.Errorf("invalid duration %q: expected a number at %q", s, rest)
		}
		n, err := strconv.ParseInt(rest[:i], 10, 63)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		rest = rest[i:]
		if rest == "" {
			// A unitless lease time means seconds to dnsmasq, so it means
			// seconds here too rather than being an error an operator has to
			// learn about.
			total += time.Duration(n) * time.Second
			break
		}
		unit, ok := durationUnits[rest[0]]
		if !ok {
			return 0, fmt.Errorf("invalid duration %q: unknown unit %q (want s, m, h, d or w)", s, rest[:1])
		}
		total += time.Duration(n) * unit
		rest = rest[1:]
	}
	return Duration(total), nil
}

// String renders the duration the way an operator would write it — "12h", not
// time.Duration's "12h0m0s".
func (d Duration) String() string {
	td := time.Duration(d)
	if td <= 0 {
		return "0s"
	}
	var b strings.Builder
	for _, u := range []struct {
		suffix string
		size   time.Duration
	}{{"w", 7 * 24 * time.Hour}, {"d", 24 * time.Hour}, {"h", time.Hour}, {"m", time.Minute}, {"s", time.Second}} {
		if n := td / u.size; n > 0 {
			fmt.Fprintf(&b, "%d%s", n, u.suffix)
			td -= n * u.size
		}
	}
	if td > 0 {
		// Sub-second precision is meaningless for a lease but we would rather
		// print something odd than silently round it away.
		b.WriteString(td.String())
	}
	return b.String()
}

// Seconds renders the duration as dnsmasq wants it in a config file.
func (d Duration) Seconds() string {
	return strconv.FormatInt(int64(time.Duration(d)/time.Second), 10)
}

// MarshalText makes Duration a JSON string.
func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// UnmarshalText parses that string back.
func (d *Duration) UnmarshalText(text []byte) error {
	parsed, err := ParseDuration(string(text))
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// NormalizeMAC lowercases and canonicalises a hardware address, so that
// "AA-BB-CC-DD-EE-FF" and "aa:bb:cc:dd:ee:ff" are one reservation rather than
// two. It returns an error for anything net.ParseMAC rejects.
func NormalizeMAC(s string) (string, error) {
	hw, err := net.ParseMAC(strings.TrimSpace(s))
	if err != nil {
		return "", fmt.Errorf("invalid MAC address %q", s)
	}
	return strings.ToLower(hw.String()), nil
}

// Normalize puts the config in canonical form: MACs lowercased, pools sorted by
// interface, reservations sorted by MAC.
//
// Everything downstream depends on this. Rendering is deterministic only if the
// input order is, and drift detection compares rendered bytes — so without a
// canonical order, reordering a JSON array would read as drift.
//
// It deliberately does not validate; a malformed MAC is left as-is for
// Validate to report with a proper path.
func (c *Config) Normalize() {
	for i, r := range c.Reservations {
		if mac, err := NormalizeMAC(r.MAC); err == nil {
			c.Reservations[i].MAC = mac
		}
	}
	slices.SortStableFunc(c.Pools, func(a, b Pool) int {
		return strings.Compare(a.Interface, b.Interface)
	})
	slices.SortStableFunc(c.Reservations, func(a, b Reservation) int {
		if n := strings.Compare(a.MAC, b.MAC); n != 0 {
			return n
		}
		return a.IP.Compare(b.IP)
	})
}

// Clone deep-copies the config, so that a proposed change can be diffed against
// the current one without either sharing backing arrays.
func (c Config) Clone() Config {
	out := c
	out.Pools = make([]Pool, len(c.Pools))
	for i, p := range c.Pools {
		p.DNS = slices.Clone(p.DNS)
		p.NTP = slices.Clone(p.NTP)
		p.Options = slices.Clone(p.Options)
		if p.Gateway != nil {
			gw := *p.Gateway
			p.Gateway = &gw
		}
		out.Pools[i] = p
	}
	out.Reservations = slices.Clone(c.Reservations)
	return out
}

// Pool returns the pool for an interface.
func (c Config) Pool(iface string) (Pool, bool) {
	i := slices.IndexFunc(c.Pools, func(p Pool) bool { return p.Interface == iface })
	if i < 0 {
		return Pool{}, false
	}
	return c.Pools[i], true
}

// SetPool adds or replaces the pool for p.Interface.
func (c *Config) SetPool(p Pool) {
	if i := slices.IndexFunc(c.Pools, func(e Pool) bool { return e.Interface == p.Interface }); i >= 0 {
		c.Pools[i] = p
		return
	}
	c.Pools = append(c.Pools, p)
	c.Normalize()
}

// RemovePool drops an interface's pool, reporting whether there was one.
func (c *Config) RemovePool(iface string) bool {
	i := slices.IndexFunc(c.Pools, func(p Pool) bool { return p.Interface == iface })
	if i < 0 {
		return false
	}
	c.Pools = slices.Delete(c.Pools, i, i+1)
	return true
}

// Reservation returns the reservation for a MAC, which need not be normalized.
func (c Config) Reservation(mac string) (Reservation, bool) {
	norm, err := NormalizeMAC(mac)
	if err != nil {
		return Reservation{}, false
	}
	i := slices.IndexFunc(c.Reservations, func(r Reservation) bool { return r.MAC == norm })
	if i < 0 {
		return Reservation{}, false
	}
	return c.Reservations[i], true
}

// SetReservation adds or replaces a reservation, keyed by MAC.
func (c *Config) SetReservation(r Reservation) {
	if mac, err := NormalizeMAC(r.MAC); err == nil {
		r.MAC = mac
	}
	if i := slices.IndexFunc(c.Reservations, func(e Reservation) bool { return e.MAC == r.MAC }); i >= 0 {
		c.Reservations[i] = r
		return
	}
	c.Reservations = append(c.Reservations, r)
	c.Normalize()
}

// RemoveReservation drops a reservation by MAC, reporting whether there was one.
func (c *Config) RemoveReservation(mac string) bool {
	norm, err := NormalizeMAC(mac)
	if err != nil {
		return false
	}
	i := slices.IndexFunc(c.Reservations, func(r Reservation) bool { return r.MAC == norm })
	if i < 0 {
		return false
	}
	c.Reservations = slices.Delete(c.Reservations, i, i+1)
	return true
}

// MarshalConfig renders the config as the module's subtree of the document.
//
// It does not stamp a "$schema" key: the document has exactly one, written by
// the store (core.SchemaURL), and a second one nested inside a module's subtree
// would be a claim this module is not in a position to make. Indentation is
// likewise not ours — the store re-indents the whole document in one pass, so
// what matters here is only the values and their canonical order.
func MarshalConfig(c Config) ([]byte, error) {
	c.Normalize()
	return json.Marshal(c)
}

// UnmarshalConfig parses a config, rejecting unknown fields.
//
// Strictness is deliberate: a typo'd key that is silently ignored produces a
// router that is quietly not doing what its config says, which is the worst
// failure mode this module has.
func UnmarshalConfig(data []byte) (Config, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("parsing dhcp config: %w", err)
	}
	c.Normalize()
	return c, nil
}

// FromDocument reads this module's subtree out of the configuration document.
//
// A document without a "dhcp" key is not an error — it means the module has
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
