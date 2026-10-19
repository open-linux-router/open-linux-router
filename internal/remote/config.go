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
// document has to get right: WireGuard and SOCKS5 keep separate settings.
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

	// Socks is the authenticated plaintext SOCKS5 proxy. Its listen scope
	// controls whether it is reachable on all interfaces or only inside WireGuard.
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
	c.Socks.Normalize()
}

// Clone returns a deep copy, so a caller may edit a config without disturbing
// the one held by the store.
func (c Config) Clone() Config {
	out := c
	out.WireGuard = c.WireGuard.Clone()
	out.Socks = c.Socks.Clone()
	return out
}

func (c Config) Redacted() Config {
	out := c.Clone()
	if out.WireGuard.PrivateKey != "" {
		out.WireGuard.PrivateKey = RedactedSecret
	}
	if out.Socks.Password != "" {
		out.Socks.Password = RedactedSecret
	}
	return out
}

// Empty reports whether the module has been configured at all.
func (c Config) Empty() bool {
	return c.Endpoint == "" && c.WireGuard.Empty() && c.Socks.Empty()
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
