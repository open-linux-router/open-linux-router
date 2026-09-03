package dns

import (
	"net/netip"
	"strings"
	"testing"
)

// testLinks is the interface picture every test in this package validates
// against: one adopted LAN interface, one adopted WAN, one the operator never
// handed us.
func testLinks() StaticLinks {
	return StaticLinks{
		"lan0": {Name: "lan0", Adopted: true, Up: true,
			Prefixes: []netip.Prefix{netip.MustParsePrefix("192.168.1.1/24"), netip.MustParsePrefix("fd00::1/64")}},
		"wan0": {Name: "wan0", Adopted: true, Up: true,
			Prefixes: []netip.Prefix{netip.MustParsePrefix("203.0.113.7/24")}},
		"guest0": {Name: "guest0", Adopted: false, Up: true,
			Prefixes: []netip.Prefix{netip.MustParsePrefix("192.168.30.1/24")}},
	}
}

// validConfig is the smallest configuration that should pass cleanly, so every
// test below can state exactly the one thing it is breaking.
func validConfig() Config {
	c := Config{
		Enabled:   true,
		Listen:    []netip.AddrPort{netip.MustParseAddrPort("192.168.1.1:53")},
		AllowFrom: []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")},
	}
	c.Normalize()
	return c
}

func errorPaths(r Result) []string {
	out := make([]string, 0, len(r.Errors))
	for _, p := range r.Errors {
		out = append(out, p.Path)
	}
	return out
}

func hasProblem(problems []Problem, pathPrefix string) bool {
	for _, p := range problems {
		if strings.HasPrefix(p.Path, pathPrefix) {
			return true
		}
	}
	return false
}

func TestValidateAcceptsAMinimalConfig(t *testing.T) {
	if res := Validate(validConfig(), testLinks()); !res.OK() {
		t.Errorf("a minimal valid config was rejected: %v", errorPaths(res))
	}
}

func TestValidateListen(t *testing.T) {
	tests := []struct {
		name    string
		edit    func(*Config)
		wantErr string // a path prefix; empty means "must pass"
	}{
		{
			name:    "enabled with nowhere to listen",
			edit:    func(c *Config) { c.Listen = nil },
			wantErr: "listen",
		},
		{
			// A wildcard would answer on the WAN too, and the difference
			// between that and a LAN address is one missing firewall rule.
			name:    "wildcard address",
			edit:    func(c *Config) { c.Listen = []netip.AddrPort{netip.MustParseAddrPort("0.0.0.0:53")} },
			wantErr: "listen[0]",
		},
		{
			name: "no port",
			edit: func(c *Config) {
				c.Listen = []netip.AddrPort{netip.AddrPortFrom(netip.MustParseAddr("192.168.1.1"), 0)}
			},
			wantErr: "listen[0]",
		},
		{
			name: "an address that is on no interface",
			edit: func(c *Config) {
				c.Listen = []netip.AddrPort{netip.MustParseAddrPort("10.9.9.9:53")}
				c.AllowFrom = []netip.Prefix{netip.MustParsePrefix("10.9.9.0/24")}
			},
			wantErr: "listen[0]",
		},
		{
			// design.md §3.4 is adopt-only. A resolver appearing on an
			// interface nobody handed us is the exact surprise that forbids.
			name: "an unadopted interface",
			edit: func(c *Config) {
				c.Listen = []netip.AddrPort{netip.MustParseAddrPort("192.168.30.1:53")}
				c.AllowFrom = []netip.Prefix{netip.MustParsePrefix("192.168.30.0/24")}
			},
			wantErr: "listen[0]",
		},
		{
			// unbound's port. Two things cannot hold it.
			name: "colliding with the resolver",
			edit: func(c *Config) {
				c.Listen = []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:5353")}
				c.AllowFrom = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}
			},
			wantErr: "listen[0]",
		},
		{
			name: "listed twice",
			edit: func(c *Config) {
				c.Listen = append(c.Listen, netip.MustParseAddrPort("192.168.1.1:53"))
			},
			wantErr: "listen[1]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			tc.edit(&cfg)
			res := Validate(cfg, testLinks())
			if tc.wantErr == "" {
				if !res.OK() {
					t.Errorf("unexpected errors: %v", errorPaths(res))
				}
				return
			}
			if !hasProblem(res.Errors, tc.wantErr) {
				t.Errorf("want an error at %q, got %v", tc.wantErr, errorPaths(res))
			}
		})
	}
}

// An open resolver is not a risk to the operator's own network. It is a
// reflector pointed at somebody else's, and they find out when their uplink is
// saturated — so this refuses rather than warns.
func TestValidateRefusesAnOpenResolver(t *testing.T) {
	for _, open := range []string{"0.0.0.0/0", "::/0"} {
		cfg := validConfig()
		cfg.AllowFrom = []netip.Prefix{netip.MustParsePrefix(open)}
		res := Validate(cfg, testLinks())
		if !hasProblem(res.Errors, "allow_from[0]") {
			t.Errorf("%s was accepted as an allowed source: %v", open, errorPaths(res))
			continue
		}
		if !strings.Contains(res.Errors[0].Message, "amplifier") {
			t.Errorf("the refusal does not explain the consequence: %s", res.Errors[0].Message)
		}
	}
}

// Empty allow_from means "the networks I listen on". It is only a problem when
// that derivation comes up empty, because then the relay starts and answers
// nobody — a silent outage that reads as a DNS bug.
func TestValidateEmptyAllowFromIsDerived(t *testing.T) {
	cfg := validConfig()
	cfg.AllowFrom = nil
	if res := Validate(cfg, testLinks()); !res.OK() {
		t.Errorf("an empty allow_from with a derivable network was rejected: %v", errorPaths(res))
	}

	// Loopback is on no interface link knows about, so nothing can be derived.
	cfg.Listen = []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:53")}
	if res := Validate(cfg, testLinks()); !hasProblem(res.Errors, "allow_from") {
		t.Errorf("a config that would answer nobody was accepted: %v", errorPaths(res))
	}
}

func TestValidateUpstream(t *testing.T) {
	t.Run("forwarding with no servers", func(t *testing.T) {
		cfg := validConfig()
		cfg.Upstream.Mode = ModeForward
		if res := Validate(cfg, testLinks()); !hasProblem(res.Errors, "upstream.servers") {
			t.Errorf("want an error, got %v", errorPaths(res))
		}
	})

	t.Run("forwarding to ourselves", func(t *testing.T) {
		cfg := validConfig()
		cfg.Upstream = Upstream{
			Mode:    ModeForward,
			Servers: []netip.AddrPort{netip.MustParseAddrPort("192.168.1.1:53")},
		}
		if res := Validate(cfg, testLinks()); !hasProblem(res.Errors, "upstream.servers[0]") {
			t.Errorf("a forwarding loop was accepted: %v", errorPaths(res))
		}
	})

	t.Run("TLS without a certificate name warns", func(t *testing.T) {
		cfg := validConfig()
		cfg.Upstream = Upstream{
			Mode:    ModeForward,
			Servers: []netip.AddrPort{netip.MustParseAddrPort("1.1.1.1:853")},
			TLS:     true,
		}
		res := Validate(cfg, testLinks())
		if !res.OK() {
			t.Fatalf("this should warn, not fail: %v", errorPaths(res))
		}
		if !hasProblem(res.Warnings, "upstream.tls_name") {
			t.Error("no warning that the upstream is unauthenticated")
		}
	})

	t.Run("servers listed but unused warns", func(t *testing.T) {
		cfg := validConfig()
		cfg.Upstream = Upstream{Servers: []netip.AddrPort{netip.MustParseAddrPort("1.1.1.1:53")}}
		res := Validate(cfg, testLinks())
		if !hasProblem(res.Warnings, "upstream.servers") {
			t.Error("forwarders under the recursing default were accepted silently")
		}
	})
}

func TestValidatePolicies(t *testing.T) {
	t.Run("two default policies", func(t *testing.T) {
		// Which one applies would otherwise depend on the order they happen to
		// be in, which is not a thing an operator can reason about.
		cfg := validConfig()
		cfg.Policies = []Policy{{Name: "a"}, {Name: "b"}}
		if res := Validate(cfg, testLinks()); !hasProblem(res.Errors, "policies[1].clients") {
			t.Errorf("two default policies were accepted: %v", errorPaths(res))
		}
	})

	t.Run("duplicate names", func(t *testing.T) {
		cfg := validConfig()
		cfg.Policies = []Policy{
			{Name: "kids", Clients: []netip.Prefix{netip.MustParsePrefix("192.168.1.50/32")}},
			{Name: "kids", Clients: []netip.Prefix{netip.MustParsePrefix("192.168.1.51/32")}},
		}
		if res := Validate(cfg, testLinks()); !hasProblem(res.Errors, "policies[1].name") {
			t.Errorf("a duplicate policy name was accepted: %v", errorPaths(res))
		}
	})

	t.Run("one prefix claimed twice", func(t *testing.T) {
		// The most-specific rule cannot break this tie, so which one wins would
		// be an accident.
		cfg := validConfig()
		cfg.Policies = []Policy{
			{Name: "a", Clients: []netip.Prefix{netip.MustParsePrefix("192.168.1.50/32")}},
			{Name: "b", Clients: []netip.Prefix{netip.MustParsePrefix("192.168.1.50/32")}},
		}
		res := Validate(cfg, testLinks())
		if !hasProblem(res.Errors, "policies[1].clients[0]") {
			t.Errorf("an ambiguous client claim was accepted: %v", errorPaths(res))
		}
	})

	t.Run("a name that is a filename", func(t *testing.T) {
		// It becomes a path under policy.d, so a slash is a traversal rather
		// than a typo.
		cfg := validConfig()
		cfg.Policies = []Policy{{Name: "../../etc/passwd"}}
		if res := Validate(cfg, testLinks()); !hasProblem(res.Errors, "policies[0].name") {
			t.Errorf("a path was accepted as a policy name: %v", errorPaths(res))
		}
	})

	t.Run("a URL in a blocklist", func(t *testing.T) {
		// The actual failure mode: a pasted URL blocks nothing and says nothing.
		cfg := validConfig()
		cfg.Policies = []Policy{{Name: "kids", Block: []string{"https://example.com/ads"}}}
		cfg.Normalize()
		if res := Validate(cfg, testLinks()); !hasProblem(res.Errors, "policies[0].block[0]") {
			t.Errorf("a URL was accepted as a domain: %v", errorPaths(res))
		}
	})

	t.Run("a name in both lists warns", func(t *testing.T) {
		cfg := validConfig()
		cfg.Policies = []Policy{{
			Name: "kids", Block: []string{"example.com"}, Allow: []string{"example.com"},
		}}
		res := Validate(cfg, testLinks())
		if !res.OK() {
			t.Fatalf("this should warn, not fail: %v", errorPaths(res))
		}
		if !hasProblem(res.Warnings, "policies[0].allow") {
			t.Error("no warning that the block entry is dead")
		}
	})

	t.Run("policies without a redirect warn", func(t *testing.T) {
		// docs/dns.md §2's only dangerous misconfiguration: blocking applies to
		// clients that ask us, and nothing makes them.
		cfg := validConfig()
		cfg.Policies = []Policy{{Name: "kids", Block: []string{"example.com"}}}
		res := Validate(cfg, testLinks())
		if !hasProblem(res.Warnings, "hijack.enabled") {
			t.Error("no warning that the blocklist is advisory")
		}
	})
}

func TestValidateHijack(t *testing.T) {
	t.Run("no interfaces", func(t *testing.T) {
		// "All of them" would capture the WAN side, so it cannot be the meaning
		// of an empty list.
		cfg := validConfig()
		cfg.Hijack = Hijack{Enabled: true}
		if res := Validate(cfg, testLinks()); !hasProblem(res.Errors, "hijack.interfaces") {
			t.Errorf("want an error, got %v", errorPaths(res))
		}
	})

	t.Run("an unadopted interface", func(t *testing.T) {
		cfg := validConfig()
		cfg.Hijack = Hijack{Enabled: true, Interfaces: []string{"guest0"}}
		if res := Validate(cfg, testLinks()); !hasProblem(res.Errors, "hijack.interfaces[0]") {
			t.Errorf("want an error, got %v", errorPaths(res))
		}
	})

	t.Run("an unknown interface", func(t *testing.T) {
		cfg := validConfig()
		cfg.Hijack = Hijack{Enabled: true, Interfaces: []string{"nope0"}}
		if res := Validate(cfg, testLinks()); !hasProblem(res.Errors, "hijack.interfaces[0]") {
			t.Errorf("want an error, got %v", errorPaths(res))
		}
	})

	t.Run("without blocking DoT warns", func(t *testing.T) {
		// Ranked by cost to defeat: leaving the cheap door open makes the
		// expensive work pointless.
		cfg := validConfig()
		cfg.Hijack = Hijack{Enabled: true, Interfaces: []string{"lan0"}}
		res := Validate(cfg, testLinks())
		if !res.OK() {
			t.Fatalf("this should warn, not fail: %v", errorPaths(res))
		}
		if !hasProblem(res.Warnings, "hijack.block_dot") {
			t.Error("no warning that DNS-over-TLS routes around the redirect")
		}
	})

	t.Run("v4 only warns about the v6 gap", func(t *testing.T) {
		cfg := validConfig()
		cfg.Hijack = Hijack{Enabled: true, Interfaces: []string{"lan0"}, BlockDoT: true}
		res := Validate(cfg, testLinks())
		if !hasProblem(res.Warnings, "listen") {
			t.Error("no warning that IPv6 queries bypass the redirect")
		}
	})
}

// The escape hatch is a passthrough, not a free-for-all: a directive that
// silently overrode a rendered one would defeat the point of having it declared.
func TestValidateExtraConf(t *testing.T) {
	for _, denied := range []string{
		"interface: 0.0.0.0",
		"access-control: 0.0.0.0/0 allow",
		"forward-zone:",
		"  forward-addr: 8.8.8.8",
		"username: root",
	} {
		cfg := validConfig()
		cfg.ExtraConf = denied
		res := Validate(cfg, testLinks())
		if len(res.Errors) == 0 {
			t.Errorf("%q was accepted in the escape hatch", denied)
		}
	}

	// Anything olr does not render passes through untouched, which is the
	// whole promise of the hatch.
	cfg := validConfig()
	cfg.ExtraConf = "# a comment\nserver:\n  msg-cache-size: 8m\n"
	if res := Validate(cfg, testLinks()); !res.OK() {
		t.Errorf("a legitimate escape-hatch setting was rejected: %v", errorPaths(res))
	}
}

func TestCheckName(t *testing.T) {
	for _, ok := range []string{"example.com", "a.b.c.example.com", "xn--kbenhavn-54a.example", "_dmarc.example.com"} {
		if err := checkName(ok); err != nil {
			t.Errorf("checkName(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "example .com", "example..com", ".example.com", "-bad.example", "http://x/y"} {
		if err := checkName(bad); err == nil {
			t.Errorf("checkName(%q) = nil, want an error", bad)
		}
	}
}
