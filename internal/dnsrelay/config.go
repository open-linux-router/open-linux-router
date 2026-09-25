// Package dnsrelay is the DNS relay that owns :53.
//
// It is a backend, not a module. design.md §3.5 is explicit that backends are
// separate processes even when we write them — "if olr-dhcpd ever exists it is
// a binary and a unit, never a goroutine" — and gives the deciding test:
// does it have to keep running while olrd is stopped? DNS does. A control
// plane that blips the whole house's name resolution every time it restarts,
// or takes it down with an unrelated panic in an HTTP handler, is the failure
// docs/dns.md §5 spends its length on.
//
// So this package is driven the way dnsmasq is driven: a rendered config file
// and a signal. internal/dns renders the files; internal/dnsd reads them. There
// is deliberately no RPC back to olrd — §3.5's corollary is that a private
// channel would cost the backend its ability to be run and debugged on its own,
// which is exactly what makes a resolver diagnosable at 2am.
//
// The package is kept free of Linux-only imports so it builds and tests on any
// host.
package dnsrelay

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

// The rendered configuration, split into two files for the same reason
// internal/dhcp splits dnsmasq's: one of them can change without a restart.
//
//	relay.json      listen addresses, upstream, limits    → restart
//	policy.d/*.json one file per policy                   → SIGHUP
//
// Rebinding a socket needs a restart; a blocklist does not. Since editing a
// blocklist is the common operation and rebinding is close to never, the split
// is what makes the common case free.

// Config is relay.json: everything that costs a restart to change.
type Config struct {
	// Listen is where queries are answered, on both UDP and TCP. Empty — the
	// ordinary case — means the wildcard on DefaultPort, in both families.
	//
	// The wildcard is the default because the alternative is a copy of
	// something the kernel owns. internal/dns.Config.Listen carries the full
	// reasoning; what matters on this side is the failure it removes. An
	// address that no longer exists cannot be bound, Run returned that error,
	// and Restart=always turned a renumbered LAN into a crash loop that took
	// DNS down for the whole building. Nothing on this side could have known
	// the address had moved.
	//
	// It does not widen who gets answered. AllowFrom is checked on every
	// datagram regardless of which socket received it, so a query arriving from
	// outside those prefixes is dropped whether the socket holds one address or
	// all of them.
	Listen []netip.AddrPort `json:"listen"`

	// AllowFrom are the source prefixes permitted to ask. Empty denies
	// everybody, which is the safe direction to fail: a relay that answers
	// nobody is a visible outage, where one that answers the internet is an
	// amplifier nobody notices until it is used (docs/dns.md §5).
	AllowFrom []netip.Prefix `json:"allow_from"`

	// Upstream is the resolver doing the actual work — unbound, on loopback.
	Upstream netip.AddrPort `json:"upstream"`

	// PolicyDir holds one JSON file per policy, re-read on SIGHUP.
	PolicyDir string `json:"policy_dir,omitempty"`

	// PublishedFile names the names this box answers for itself
	// (published.go), re-read on SIGHUP with the policies. A missing file
	// means none.
	PublishedFile string `json:"published_file,omitempty"`

	// ObserveSocket is the unix socket serving the query log and name map.
	// Empty disables it, and then nothing can read what the relay saw.
	ObserveSocket string `json:"observe_socket,omitempty"`

	// QueryLogEntries is how many answered queries to keep in memory. Zero
	// means no query log at all.
	QueryLogEntries int `json:"query_log_entries,omitempty"`

	// UpstreamTimeout bounds a single forwarded query. Zero means
	// DefaultUpstreamTimeout.
	UpstreamTimeout Duration `json:"upstream_timeout,omitempty"`
}

// Policy is one policy.d/<name>.json file: what a set of clients may look up.
type Policy struct {
	Name string `json:"name"`

	// Clients are the source prefixes this policy governs, most specific
	// first-past-the-post. Empty makes it the default policy.
	Clients []netip.Prefix `json:"clients,omitempty"`

	// Block and Allow are canonical names — lowercased, no trailing dot, no
	// leading wildcard. The renderer guarantees that, so the matcher does not
	// have to normalise on the hot path.
	Block []string `json:"block,omitempty"`
	Allow []string `json:"allow,omitempty"`

	// Response is "nxdomain" or "zero". Empty means nxdomain.
	Response string `json:"response,omitempty"`
}

// Block response spellings, matching internal/dns.BlockResponse. Duplicated as
// bare strings rather than imported because the dependency runs the other way:
// internal/dns renders these files, so this package must not know about it.
const (
	RespondNXDOMAIN = "nxdomain"
	RespondZero     = "zero"
)

// DefaultPort is where DNS is served when Listen does not say.
const DefaultPort = 53

// ListenOrWildcard resolves Listen to the addresses to bind.
//
// Both families, because an empty list means "wherever this box can be
// reached". Two sockets rather than one dual-stack socket: Go resolves a
// wildcard "udp" listen to AF_INET6 with v4-mapped delivery, and every address
// downstream — the allow check, the query log, the client table — would then
// have to unmap. Pinning the family at bind time keeps addresses in the shape
// they are written in.
func (c Config) ListenOrWildcard() []netip.AddrPort {
	if len(c.Listen) > 0 {
		return c.Listen
	}
	return WildcardListen(DefaultPort)
}

// WildcardListen is the pair of unspecified addresses for a port.
func WildcardListen(port uint16) []netip.AddrPort {
	return []netip.AddrPort{
		netip.AddrPortFrom(netip.IPv4Unspecified(), port),
		netip.AddrPortFrom(netip.IPv6Unspecified(), port),
	}
}

// Port is the port queries are answered on.
func (c Config) Port() uint16 {
	if len(c.Listen) == 0 {
		return DefaultPort
	}
	return c.Listen[0].Port()
}

// DefaultUpstreamTimeout bounds one forwarded query.
//
// Generous rather than tight. A recursive resolver chasing a cold delegation
// through a slow authoritative server legitimately takes seconds, and a client
// that gets SERVFAIL because we gave up early will not retry any faster than it
// would have waited.
const DefaultUpstreamTimeout = 5 * time.Second

// Timeout resolves the zero value.
func (c Config) Timeout() time.Duration {
	if c.UpstreamTimeout <= 0 {
		return DefaultUpstreamTimeout
	}
	return time.Duration(c.UpstreamTimeout)
}

// Duration is a time.Duration that round-trips through JSON as a string, so a
// rendered file says "5s" rather than 5000000000.
//
// The same wrapper internal/dhcp carries, and for the same reason design.md §10
// gives: the integer form is unreadable in a config file and hostile in an API
// response.
type Duration time.Duration

// MarshalText makes Duration a JSON string.
func (d Duration) MarshalText() ([]byte, error) {
	return []byte(time.Duration(d).String()), nil
}

// UnmarshalText parses that string back.
func (d *Duration) UnmarshalText(text []byte) error {
	parsed, err := time.ParseDuration(strings.TrimSpace(string(text)))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", text, err)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalConfig renders relay.json.
func MarshalConfig(c Config) ([]byte, error) { return json.MarshalIndent(c, "", "  ") }

// MarshalPolicy renders one policy.d file.
func MarshalPolicy(p Policy) ([]byte, error) { return json.MarshalIndent(p, "", "  ") }

// UnmarshalConfig parses relay.json strictly.
//
// Unknown fields are an error here for the same reason they are in every olr
// config parser, with one extra edge: this file is written by a *newer* olrd
// than the binary reading it might be, if a partial upgrade left them out of
// step. Failing loudly at startup beats a relay that silently ignored the
// access-control list it did not recognise.
func UnmarshalConfig(data []byte) (Config, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("parsing relay configuration: %w", err)
	}
	return c, nil
}

// UnmarshalPolicy parses one policy file strictly.
func UnmarshalPolicy(data []byte) (Policy, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var p Policy
	if err := dec.Decode(&p); err != nil {
		return Policy{}, fmt.Errorf("parsing policy: %w", err)
	}
	return p, nil
}
