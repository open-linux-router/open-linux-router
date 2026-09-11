package ingress

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// ModuleName is the path segment, config section and event label for this
// module.
//
// Neither `proxy` nor `gateway` was available: docs/gateway.md uses both for
// traffic on its way *out* — an exit, and the operator's mihomo/clash box. This
// module is the other direction, so it needed a third word.
const ModuleName = "ingress"

// Config is the ingress module's intent: which internal services are reachable
// by name, and what it takes to serve them over HTTPS.
//
// It is the single source for the CLI flags, the REST body, the UI form and the
// MCP tool definition (design.md §3.2 rule 3).
//
// Note what is *not* here: the domain. Published names live under the same
// suffix as everything else this network answers for, and that suffix is
// `dns.LocalDomain` — a fact the `dns` module owns. §4.1 forbids the copy, so
// we read it through DNSView and this struct has no `domain` field to drift
// from it. See docs/ingress.md §3.
type Config struct {
	// Enabled controls whether the proxy runs at all. Disabling stops the
	// service and leaves the configuration in place, so published services can
	// be turned back on without retyping them.
	Enabled bool `json:"enabled"`

	// Certificate is how the wildcard is obtained. One certificate serves every
	// published name, which is what makes adding a service a two-field
	// operation (docs/ingress.md §1).
	Certificate Certificate `json:"certificate"`

	// Services are the published names, keyed by Name.
	Services []Service `json:"services,omitempty"`

	// ExtraConf is the module's declared escape hatch (design.md §3.2 rule 5):
	// appended verbatim to the rendered Caddyfile. Caddy can do vastly more
	// than publish an internal service, and this field plus the upstream
	// documentation is the whole of our answer to all of it.
	ExtraConf string `json:"raw_caddyfile,omitempty"`
}

// Certificate holds what ACME needs and nothing else.
//
// DNS-01 is the only challenge that works for a name which does not resolve
// from outside, and docs/ingress.md §4 records why the other two are dead:
// HTTP-01 would mean exposing the router to get a certificate for something
// that is not exposed, and a local CA would mean installing a root certificate
// on every phone, TV and console on the network.
type Certificate struct {
	// Provider names the DNS provider whose API Caddy writes the challenge
	// record through. The vocabulary is Caddy's, not ours — see schema.go.
	Provider string `json:"provider,omitempty"`

	// Token is the provider's API credential.
	//
	// This is the first credential for a third-party account olr has ever
	// held, and there is no secrets store to put it in (docs/ingress.md §4.1).
	// It therefore lives in the config document like any other field, with two
	// obligations that the rest of this module has to keep:
	//
	//   - it is redacted on every surface that prints config or a plan diff,
	//     because `olr ingress plan` showing an operator their own API token in
	//     a terminal is how tokens end up in bug reports; and
	//   - it is rendered into an environment file the unit reads, never into
	//     the Caddyfile, so that the file we might reasonably show somebody
	//     never contains it in the first place.
	//
	// Scope it to the one zone if the provider supports it. The setup copy
	// should say so, because the blast radius of an account-wide key here is
	// the operator's whole DNS.
	Token string `json:"provider_token,omitempty"`

	// Email is the ACME account contact. Optional; Let's Encrypt uses it only
	// to warn about expiry, which is a warning we would rather they received.
	Email string `json:"acme_email,omitempty"`

	// Resolvers are the DNS servers Caddy uses to confirm the challenge record
	// has propagated. **Leaving this empty is a mistake this module must not
	// let an operator make**, and it is the subtlest thing in the file.
	//
	// `dns` renders the local suffix as an unbound `local-zone ... static`
	// zone, which means this box answers the whole zone itself and forwards
	// none of it. A `_acme-challenge` record written into the *public* zone by
	// the provider API is therefore invisible to anything asking our resolver:
	// it gets an authoritative NXDOMAIN. Caddy's propagation check would then
	// wait for a record it can never see, and certificate issuance would hang
	// forever on a box whose DNS is working exactly as designed.
	//
	// So the check has to ask somebody else. DefaultResolvers is applied when
	// this is empty rather than leaving it to Caddy's default of the system
	// resolver, which on this box is precisely the resolver that cannot answer.
	Resolvers []string `json:"resolvers,omitempty"`
}

// DefaultResolvers are public resolvers used only for ACME propagation checks.
//
// Two, from different operators, because this is the one lookup on the box that
// must not depend on our own resolver (see Certificate.Resolvers) and a single
// third party being down should not stop a renewal.
//
// This is not a privacy leak worth worrying about: the only names asked are
// `_acme-challenge` records the operator is simultaneously publishing to the
// public DNS, and the CA is about to query them from the outside anyway.
var DefaultResolvers = []string{"1.1.1.1", "9.9.9.9"}

// ResolversOrDefault resolves the empty case.
func (c Certificate) ResolversOrDefault() []string {
	if len(c.Resolvers) == 0 {
		return slices.Clone(DefaultResolvers)
	}
	return slices.Clone(c.Resolvers)
}

// Service is one published name.
//
// Two fields, and that is the module's entire claim: the certificate, the DNS
// record, the proxy stanza and the `:80` redirect are all derived from things
// established once when the module was enabled (docs/ingress.md §1).
type Service struct {
	// Name is relative to the local domain: "grafana", not
	// "grafana.home.example.com". Normalize strips the suffix if it was typed,
	// so both spellings are one entry rather than two that diff against each
	// other forever. This mirrors dns.Host, deliberately — they are names in
	// the same namespace and must behave the same way.
	Name string `json:"name"`

	// Upstream is where a request for this name is sent.
	Upstream Upstream `json:"upstream"`
}

// Upstream is the target of a published service.
//
// Device and Host are alternatives and exactly one must be set. The pair exists
// because most targets are devices this network already knows by name, and some
// are not: a container on the router itself, or something behind another
// router.
type Upstream struct {
	// Device names a device in the `devices` module (design.md §4.4). It is the
	// preferred form and the reason is §4.1: a device's address belongs to
	// `devices` and `dhcp`, so referencing the device reads it per request
	// instead of copying it here where it would rot the next time the lease
	// changed.
	//
	// Choosing a device that has no fixed address is refused rather than
	// rendered — see validate.go, and docs/ingress.md §1.2.
	Device string `json:"device,omitempty"`

	// Host is a literal address or hostname, for targets that are not devices.
	Host string `json:"host,omitempty"`

	// Port is the upstream's port. Required in both forms: a device knows its
	// address, never which of its ports somebody meant.
	Port uint16 `json:"port"`

	// Scheme is how to speak to the upstream. Empty means SchemeHTTP.
	//
	// SchemeHTTPS exists for the NAS and hypervisor UIs that only listen on
	// HTTPS with a self-signed certificate. Verification is deliberately not
	// attempted for those (render.go) — the connection is over the operator's
	// own LAN to a device they named, and demanding a valid certificate there
	// would make the common case impossible while protecting against an
	// attacker who is already inside the network.
	Scheme Scheme `json:"scheme,omitempty"`
}

// Target renders the upstream as Caddy's dial address, given a resolved
// address for the device form.
func (u Upstream) Target(resolved string) string {
	host := u.Host
	if u.Device != "" {
		host = resolved
	}
	return fmt.Sprintf("%s:%d", host, u.Port)
}

// Scheme is how olr speaks to an upstream.
type Scheme string

// The upstream schemes.
const (
	SchemeHTTP  Scheme = "http"
	SchemeHTTPS Scheme = "https"
)

// Schemes lists the vocabulary. One source, so the enum published in the schema
// and the check in the validator cannot disagree.
func Schemes() []Scheme { return []Scheme{SchemeHTTP, SchemeHTTPS} }

// Valid reports whether s is a known scheme. The empty string is valid and
// means SchemeHTTP.
func (s Scheme) Valid() bool { return s == "" || slices.Contains(Schemes(), s) }

// OrDefault resolves the empty value.
func (s Scheme) OrDefault() Scheme {
	if s == "" {
		return SchemeHTTP
	}
	return s
}

// Service returns a published service by name, given relative or in full.
func (c Config) Service(name string, domain string) (Service, bool) {
	name = normalizeName(name, domain)
	i := slices.IndexFunc(c.Services, func(s Service) bool { return s.Name == name })
	if i < 0 {
		return Service{}, false
	}
	return c.Services[i], true
}

// SetService adds or replaces a published service, keyed by name.
func (c *Config) SetService(s Service, domain string) {
	s.Name = normalizeName(s.Name, domain)
	if i := slices.IndexFunc(c.Services, func(e Service) bool { return e.Name == s.Name }); i >= 0 {
		c.Services[i] = s
		c.Normalize()
		return
	}
	c.Services = append(c.Services, s)
	c.Normalize()
}

// RemoveService drops a published service, reporting whether there was one.
func (c *Config) RemoveService(name string, domain string) bool {
	name = normalizeName(name, domain)
	i := slices.IndexFunc(c.Services, func(s Service) bool { return s.Name == name })
	if i < 0 {
		return false
	}
	c.Services = slices.Delete(c.Services, i, i+1)
	return true
}

// normalizeName reduces the two spellings of one name to the stored one.
//
// The operator may type "grafana" or "grafana.home.example.com" and mean the
// same service. Storing whichever they typed would let both exist at once, and
// then the rendered Caddyfile would carry two matchers for one hostname with
// the second silently unreachable.
func normalizeName(name, domain string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.TrimSuffix(name, ".")
	if domain != "" {
		name = strings.TrimSuffix(name, "."+strings.ToLower(domain))
	}
	return name
}

// Normalize puts the config in its canonical form.
//
// Sorting is not cosmetic: the rendered Caddyfile is compared against what is
// on disk to detect drift (§5.4), so a config that reorders itself between two
// identical edits would report a change that is not one.
func (c *Config) Normalize() {
	for i := range c.Services {
		s := &c.Services[i]
		s.Name = strings.TrimSpace(strings.ToLower(s.Name))
		s.Upstream.Host = strings.TrimSpace(s.Upstream.Host)
		s.Upstream.Device = strings.TrimSpace(s.Upstream.Device)
	}
	slices.SortStableFunc(c.Services, func(a, b Service) int {
		return strings.Compare(a.Name, b.Name)
	})

	c.Certificate.Provider = strings.TrimSpace(strings.ToLower(c.Certificate.Provider))
	c.Certificate.Email = strings.TrimSpace(c.Certificate.Email)
	// The token is not trimmed or cased. It is an opaque credential and
	// "helpfully" altering it would produce an authentication failure whose
	// cause is invisible in every diff, because the diff would look right.
}

// Clone returns a deep copy, so a caller may edit a config without disturbing
// the one held by the store.
func (c Config) Clone() Config {
	out := c
	out.Services = slices.Clone(c.Services)
	out.Certificate.Resolvers = slices.Clone(c.Certificate.Resolvers)
	return out
}

// Redacted returns a copy safe to print, log, or return over the API.
//
// Used by every surface rather than by the ones that looked risky, because the
// failure mode is silent: nothing breaks when a token is printed, it just ends
// up in a scrollback buffer and then in a bug report.
func (c Config) Redacted() Config {
	out := c.Clone()
	if out.Certificate.Token != "" {
		out.Certificate.Token = RedactedToken
	}
	return out
}

// RedactedToken is what stands in for a credential on a printed surface. A
// fixed string rather than a length-preserving mask, so it cannot be mistaken
// for the real value and gives away nothing about it.
const RedactedToken = "********"

// MarshalConfig encodes a config for the store, normalising first so that two
// equivalent configs produce identical bytes.
func MarshalConfig(c Config) ([]byte, error) {
	c.Normalize()
	return json.Marshal(c)
}

// UnmarshalConfig parses a config, rejecting unknown fields.
//
// Strictness is deliberate: a typo'd key that is silently ignored produces a
// router that is quietly not doing what its config says. Here that means a
// service the operator believes is published and is not, or — worse — a
// `provider_token` misspelled into oblivion while the old credential keeps
// working until it is rotated.
func UnmarshalConfig(data []byte) (Config, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("%s configuration: %w", ModuleName, err)
	}
	c.Normalize()
	return c, nil
}

// FromDocument reads this module's section out of the store's document.
func FromDocument(d core.Document) (Config, error) {
	raw, ok := d.Raw(ModuleName)
	if !ok {
		return Config{}, nil
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return Config{}, fmt.Errorf("%s configuration: %w", ModuleName, err)
	}
	c.Normalize()
	return c, nil
}
