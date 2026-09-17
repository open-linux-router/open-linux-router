// Package remote owns the ways an operator reaches their own network from
// outside it.
//
// # Three objects, not one abstraction
//
// design.md §4 reserved the name `vpn` for "wireguard — remote access,
// site-to-site". This is the remote-access half under a different name, and the
// rename is argued in docs/remote-access.md §10: two of the three objects this
// module holds are not VPNs, so the reserved word would describe a third of it
// and mis-describe the rest.
//
// The three are WireGuard, Shadowsocks and SOCKS5, and they share no field —
// peers and public keys, a port and a cipher, a listen scope and credentials.
// So they are three concrete objects rather than one abstraction with three
// backends, and the second one arriving is what settled that the parallelism
// goes all the way down: **they do not share a plan or an apply either.**
// WireGuard configures the kernel; Shadowsocks renders a file and drives a
// unit. Folding those into one plan would produce a type whose every field is
// empty half the time, describing two mechanisms that never interact.
//
// So the surfaces are parallel too — `/api/remote/wireguard/…` and
// `/api/remote/shadowsocks/…`, each with its own config, plan, apply and status
// — and the module is a namespace rather than a thing. What the module level
// still owns is this file: the document, and the one fact that belongs to the
// box rather than to a protocol.
//
// # What is built
//
// All three. SOCKS5 arrived last and needed no new argument, which is what
// docs/remote-access.md §9 predicted when it deferred it: the second object had
// already settled that the parallelism goes all the way down, so the third only
// had to follow it.
//
// It did add one thing the other two did not need — a listen *scope*. SOCKS5
// carries no encryption, so where it listens is the difference between a
// sensible thing to run and an open door. It defaults to listening inside the
// WireGuard tunnel, which is the one place a plaintext protocol is fine, and
// composing the two objects that way is cheaper than giving SOCKS5 a crypto
// layer it was never designed to have.
package remote

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// ModuleName is the path segment, config section and event label for this
// module.
const ModuleName = "remote"

// Config is the remote-access module's intent.
//
// One shared field and one section per object. The sections are what the stored
// document has to get right: adding `socks` beside the other two must not move
// a key anybody's backup contains.
type Config struct {
	// Endpoint is the public name or address clients dial — the one thing in
	// this module olr cannot work out for itself, and the one that belongs to
	// the box rather than to a protocol.
	//
	// It lives here rather than inside each object because both objects need
	// exactly this value, and a field typed twice is a field that can disagree
	// with itself: the private copy design.md §4.1 forbids, occurring inside a
	// single module rather than across two. An operator who moves house changes
	// it once.
	//
	// **Host only, never host:port.** Each object listens on its own port, so a
	// port here could only ever be right for one of them; an object whose
	// public port differs from the one it listens on says so with its own
	// `public_port`. The validator refuses a port here rather than guessing
	// which object it was meant for.
	Endpoint string `json:"endpoint,omitempty"`

	// WireGuard is the tunnel that puts a device inside the network.
	WireGuard WireGuard `json:"wireguard"`

	// Shadowsocks is the proxy that lends a device this box's way out, and
	// gives it no access to the network at all.
	Shadowsocks Shadowsocks `json:"shadowsocks"`

	// Socks is the plain SOCKS5 proxy: the same lending of a way out, with no
	// encryption of its own and therefore a default that keeps it inside the
	// tunnel (socks.go).
	Socks Socks5 `json:"socks5"`
}

// RedactedSecret is what stands in for a credential on a printed surface.
//
// A fixed string rather than a length-preserving mask, so it cannot be mistaken
// for the real value and gives away nothing about it. Same constant shape as
// ingress.RedactedToken, for the same reason.
const RedactedSecret = "********"

// EndpointHost is the endpoint, trimmed.
func (c Config) EndpointHost() string { return strings.TrimSpace(c.Endpoint) }

// DialAddress renders the endpoint as a client dials it, on a given port.
//
// Bare IPv6 is bracketed, which is the spelling both backends' clients want and
// the one an operator is least likely to produce by hand.
func (c Config) DialAddress(port uint16) string {
	host := c.EndpointHost()
	switch {
	case host == "":
		return ""
	case isIPv6(host):
		return fmt.Sprintf("[%s]:%d", host, port)
	default:
		return fmt.Sprintf("%s:%d", host, port)
	}
}

func isIPv6(host string) bool {
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.Is6()
}

// portOr resolves an optional public port against the port actually listened on.
//
// Shared by both objects, because the situation is: a router in front forwards
// 51821 to our 51820, or an ISP blocks the default and the operator moved it
// upstream only. Left empty — the ordinary case — the two are the same number.
func portOr(public, listen uint16) uint16 {
	if public != 0 {
		return public
	}
	return listen
}

// Normalize puts the config in its canonical form.
//
// Trimming and sorting are not cosmetic: the document is compared as bytes
// downstream and the rendered files are diffed against disk, so a config that
// reordered itself between two identical edits would report a change nobody
// made.
func (c *Config) Normalize() {
	c.Endpoint = strings.TrimSpace(c.Endpoint)
	c.WireGuard.Normalize()
	c.Shadowsocks.Normalize()
	c.Socks.Normalize()
}

// Clone returns a deep copy, so a caller may edit a config without disturbing
// the one held by the store.
func (c Config) Clone() Config {
	out := c
	out.WireGuard = c.WireGuard.Clone()
	out.Shadowsocks = c.Shadowsocks.Clone()
	out.Socks = c.Socks.Clone()
	return out
}

// Redacted returns a copy safe to print, log, or return over the API.
//
// Applied by every surface rather than by the ones that looked risky, because
// the failure mode is silent: nothing breaks when a credential is printed, it
// just ends up in a scrollback buffer and then in a bug report.
//
// Both objects hold one and they are not the same kind of secret. WireGuard's
// is a key an operator never sees and never needs. Shadowsocks' password *is*
// half of what a client has to be told — so it is redacted here like everything
// else, and handed over deliberately by the one route that exists to hand it
// over (ss_http.go). Redacting it and then having no way to see it would make
// the feature unusable; not redacting it would put it in every `olr remote
// show`.
func (c Config) Redacted() Config {
	out := c.Clone()
	if out.WireGuard.PrivateKey != "" {
		out.WireGuard.PrivateKey = RedactedSecret
	}
	if out.Shadowsocks.Password != "" {
		out.Shadowsocks.Password = RedactedSecret
	}
	if out.Socks.Password != "" {
		out.Socks.Password = RedactedSecret
	}
	return out
}

// Empty reports whether the module has been configured at all.
func (c Config) Empty() bool {
	return c.Endpoint == "" && c.WireGuard.Empty() && c.Shadowsocks.Empty() && c.Socks.Empty()
}

// MarshalConfig encodes a config for the store, normalising first so that two
// equivalent configs produce identical bytes.
func MarshalConfig(c Config) ([]byte, error) {
	c.Normalize()
	return json.Marshal(c)
}

// UnmarshalConfig parses a config, rejecting unknown fields.
//
// Strictness is deliberate: a typo'd key that is silently ignored produces a
// box that is quietly not doing what its config says. Here that means an
// operator who believes they narrowed a device's routes and did not, or a
// misspelled `endpoint` that leaves every client configuration pointing nowhere
// while the old value keeps working until it moves.
func UnmarshalConfig(data []byte) (Config, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("%s configuration: %w", ModuleName, err)
	}
	c.Normalize()
	return c, nil
}

// FromDocument reads this module's section out of the store's document.
//
// A document without a "remote" key is not an error — it means nobody has set
// up remote access, which is what a fresh install looks like and what the page
// must render as an empty state rather than a failure.
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

// Store is the read and write path onto the module's document, shared by both
// objects' appliers.
//
// Embedded rather than passed, so that `WireGuardApplier` and
// `ShadowsocksApplier` load the same document through the same code and cannot
// drift about what "stored intent" means — while staying two types, because
// what they do with it afterwards has nothing in common.
type Store struct {
	// Store is core's configuration document.
	Store *core.Store
}

// Load reads stored intent out of the configuration document.
func (s Store) Load() (Config, error) {
	doc, err := s.Store.Load()
	if err != nil {
		return Config{}, err
	}
	return FromDocument(doc)
}

// Save stores intent without programming anything.
//
// Read-modify-write on the shared document, so a save here cannot drop another
// module's configuration. It is safe without further locking because every
// config write in this process holds the one global apply lock (§3.6) — the
// caller takes it.
func (s Store) Save(c Config) error {
	doc, err := s.Store.Load()
	if err != nil {
		return err
	}
	data, err := MarshalConfig(c)
	if err != nil {
		return err
	}
	doc.Set(ModuleName, data)
	if err := s.Store.Save(doc); err != nil {
		return fmt.Errorf("storing configuration in %s: %w", s.Store.Path(), err)
	}
	return nil
}

// Path is where intent is stored, for the messages that name it.
func (s Store) Path() string { return s.Store.Path() }
