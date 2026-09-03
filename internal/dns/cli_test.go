package dns

import (
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/cli"
)

// The module must speak only the shared verb vocabulary (design.md §3.2 rule 4).
// This is what stops each module from quietly growing its own dialect.
func TestModuleUsesOnlySharedVerbs(t *testing.T) {
	shared := cli.Verbs()
	for _, sub := range Command().Commands() {
		if !slices.Contains(shared, sub.Name()) {
			t.Errorf("dns verb %q is outside the shared vocabulary %v", sub.Name(), shared)
		}
	}
}

func TestModuleIsGroupedAsAModule(t *testing.T) {
	if got := Command().GroupID; got != cli.GroupModules {
		t.Errorf("GroupID = %q, want %q", got, cli.GroupModules)
	}
}

// A bare address should not require typing :53 or /32. The operator is naming a
// router and a device, not filling in a form.
func TestParsersAcceptTheShortForms(t *testing.T) {
	got, err := parseAddrPorts("--listen", []string{"192.168.1.1", "192.168.1.1:5300"}, DNSPort)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Port() != DNSPort || got[1].Port() != 5300 {
		t.Errorf("parseAddrPorts = %v", got)
	}

	prefixes, err := parsePrefixes("--client", []string{"192.168.1.0/24", "192.168.1.50", "fd00::1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Prefix{
		netip.MustParsePrefix("192.168.1.0/24"),
		netip.MustParsePrefix("192.168.1.50/32"),
		netip.MustParsePrefix("fd00::1/128"),
	}
	for i := range want {
		if prefixes[i] != want[i] {
			t.Errorf("parsePrefixes[%d] = %s, want %s", i, prefixes[i], want[i])
		}
	}

	if _, err := parseAddrPorts("--listen", []string{"not an address"}, DNSPort); err == nil {
		t.Error("nonsense was accepted as an address")
	}
}

// Naming a policy is only necessary once there is more than one, which keeps
// the simple case simple: a house with a single blocklist never types --policy.
func TestTargetPolicy(t *testing.T) {
	t.Run("a fresh box invents the default", func(t *testing.T) {
		var cfg Config
		p, err := targetPolicy(&cfg, "")
		if err != nil {
			t.Fatalf("a fresh box should not need --policy: %v", err)
		}
		if p.Name != "default" {
			t.Errorf("name = %q, want default", p.Name)
		}
	})

	t.Run("one policy needs no naming", func(t *testing.T) {
		cfg := Config{Policies: []Policy{{
			Name: "kids", Clients: []netip.Prefix{netip.MustParsePrefix("192.168.1.50/32")},
		}}}
		p, err := targetPolicy(&cfg, "")
		if err != nil || p.Name != "kids" {
			t.Errorf("targetPolicy = %+v, %v", p, err)
		}
	})

	t.Run("several policies fall back to the default one", func(t *testing.T) {
		cfg := Config{Policies: []Policy{
			{Name: "everyone"},
			{Name: "kids", Clients: []netip.Prefix{netip.MustParsePrefix("192.168.1.50/32")}},
		}}
		p, err := targetPolicy(&cfg, "")
		if err != nil || p.Name != "everyone" {
			t.Errorf("targetPolicy = %+v, %v", p, err)
		}
	})

	t.Run("several policies and no default is ambiguous", func(t *testing.T) {
		cfg := Config{Policies: []Policy{
			{Name: "kids", Clients: []netip.Prefix{netip.MustParsePrefix("192.168.1.50/32")}},
			{Name: "work", Clients: []netip.Prefix{netip.MustParsePrefix("192.168.1.60/32")}},
		}}
		_, err := targetPolicy(&cfg, "")
		if err == nil {
			t.Fatal("an ambiguous edit was accepted")
		}
		// The message has to list what is available, or --policy is a guess.
		for _, want := range []string{"kids", "work", "--policy"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the error does not mention %q: %v", want, err)
			}
		}
	})

	t.Run("an unknown name", func(t *testing.T) {
		var cfg Config
		if _, err := targetPolicy(&cfg, "nope"); err == nil {
			t.Error("an unknown policy name was accepted")
		}
	})
}

func TestRemoveName(t *testing.T) {
	list := []string{"a.example", "b.example"}
	if !removeName(&list, "a.example") {
		t.Fatal("removeName did not report the removal")
	}
	if len(list) != 1 || list[0] != "b.example" {
		t.Errorf("list = %v", list)
	}
	if removeName(&list, "a.example") {
		t.Error("removeName reported removing something that was not there")
	}
}

// applyFlags drives the real `olr dns set` path: register the flags on a bare
// command, set the ones the caller named, and let apply() read Changed() the
// way cobra would.
//
// A bare command rather than setCommand(), which registers its own flags
// against its own struct — registering a second time would collide.
func applyFlags(t *testing.T, cfg *Config, set map[string]string) {
	t.Helper()
	var f configFlags
	c := &cobra.Command{Use: "set"}
	f.register(c)
	for name, value := range set {
		if err := c.Flags().Set(name, value); err != nil {
			t.Fatalf("setting --%s=%s: %v", name, value, err)
		}
	}
	if err := f.apply(c, cfg); err != nil {
		t.Fatalf("apply: %v", err)
	}
}

// Listing forwarders and leaving the mode at its default would be a config that
// quietly ignores them, so the obvious reading wins.
func TestSettingUpstreamImpliesForwarding(t *testing.T) {
	var cfg Config
	applyFlags(t, &cfg, map[string]string{"upstream": "1.1.1.1"})

	if cfg.Upstream.Mode != ModeForward {
		t.Errorf("mode = %q, want %q", cfg.Upstream.Mode, ModeForward)
	}
	if len(cfg.Upstream.Servers) != 1 || cfg.Upstream.Servers[0].Port() != 53 {
		t.Errorf("servers = %v, want one on port 53", cfg.Upstream.Servers)
	}
}

// With --tls a bare forwarder has to default to 853, or the config silently
// tries DoT against a plaintext port.
func TestUpstreamWithTLSDefaultsToTheDoTPort(t *testing.T) {
	var cfg Config
	applyFlags(t, &cfg, map[string]string{"upstream": "1.1.1.1", "tls": "true"})

	if len(cfg.Upstream.Servers) != 1 || cfg.Upstream.Servers[0].Port() != DefaultDoTPort {
		t.Errorf("servers = %v, want port %d", cfg.Upstream.Servers, DefaultDoTPort)
	}
}

// A flag nobody passed must not overwrite a stored value with its zero.
func TestUnsetFlagsLeaveTheConfigAlone(t *testing.T) {
	cfg := Config{
		Enabled:  true,
		Listen:   []netip.AddrPort{netip.MustParseAddrPort("192.168.1.1:53")},
		QueryLog: QueryLog{Enabled: true, Entries: 42},
		Hijack:   Hijack{Enabled: true, Interfaces: []string{"lan0"}, BlockDoT: true},
	}
	applyFlags(t, &cfg, map[string]string{"allow-from": "192.168.1.0/24"})

	if !cfg.QueryLog.Enabled || cfg.QueryLog.Entries != 42 {
		t.Errorf("the query log was reset by an unrelated flag: %+v", cfg.QueryLog)
	}
	if !cfg.Hijack.Enabled || !cfg.Hijack.BlockDoT {
		t.Errorf("the redirect was reset by an unrelated flag: %+v", cfg.Hijack)
	}
	if len(cfg.Listen) != 1 {
		t.Errorf("the listen address was reset by an unrelated flag: %v", cfg.Listen)
	}
	if len(cfg.AllowFrom) != 1 {
		t.Errorf("the flag that was set did not take: %v", cfg.AllowFrom)
	}
}
