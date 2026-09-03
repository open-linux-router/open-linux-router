package dnsrelay

import (
	"net/netip"
	"testing"
)

func TestDecideOnAnEmptySetIsAlwaysAllow(t *testing.T) {
	// The ordinary state of a box that only wants the query log.
	var set *PolicySet
	if d := set.Decide(netip.MustParseAddr("192.168.1.10"), "example.com"); d.Blocked {
		t.Error("a nil policy set blocked something")
	}
	if d := Compile(nil).Decide(netip.MustParseAddr("192.168.1.10"), "example.com"); d.Blocked {
		t.Error("an empty policy set blocked something")
	}
}

// A pattern covers the name and everything under it. Exact-only matching would
// be a support question on the first day: the entry would appear to do nothing,
// because nobody resolves the bare apex.
func TestBlockCoversSubdomains(t *testing.T) {
	set := Compile([]Policy{{Name: "kids", Block: []string{"example.com"}}})
	client := netip.MustParseAddr("192.168.1.10")

	for _, blocked := range []string{"example.com", "www.example.com", "a.b.c.example.com"} {
		if d := set.Decide(client, blocked); !d.Blocked {
			t.Errorf("%q was not blocked", blocked)
		}
	}
	for _, allowed := range []string{"notexample.com", "example.com.evil.test", "com"} {
		if d := set.Decide(client, allowed); d.Blocked {
			t.Errorf("%q was blocked by a pattern that should not cover it", allowed)
		}
	}
}

// "Block social media except the one site the school uses", without inverting
// the list.
func TestAllowBeatsBlock(t *testing.T) {
	set := Compile([]Policy{{
		Name:  "kids",
		Block: []string{"example.com"},
		Allow: []string{"school.example.com"},
	}})
	client := netip.MustParseAddr("192.168.1.10")

	if d := set.Decide(client, "school.example.com"); d.Blocked {
		t.Error("the exception did not beat the block")
	}
	if d := set.Decide(client, "deep.school.example.com"); d.Blocked {
		t.Error("the exception does not cover its own subdomains")
	}
	if d := set.Decide(client, "www.example.com"); !d.Blocked {
		t.Error("the exception leaked to the rest of the blocked domain")
	}
}

// A /32 for one tablet beats a /24 for the house.
func TestMostSpecificClientWins(t *testing.T) {
	set := Compile([]Policy{
		{Name: "house", Clients: []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")},
			Block: []string{"ads.example"}},
		{Name: "kids", Clients: []netip.Prefix{netip.MustParsePrefix("192.168.1.50/32")},
			Block: []string{"games.example"}},
	})

	tablet := netip.MustParseAddr("192.168.1.50")
	if d := set.Decide(tablet, "games.example"); !d.Blocked || d.Policy != "kids" {
		t.Errorf("the specific policy did not win: %+v", d)
	}
	// And the broader policy's rules do not apply to it — policies do not
	// stack, which is the simpler model to reason about.
	if d := set.Decide(tablet, "ads.example"); d.Blocked {
		t.Error("the broader policy's blocklist leaked into the specific one")
	}

	laptop := netip.MustParseAddr("192.168.1.60")
	if d := set.Decide(laptop, "ads.example"); !d.Blocked || d.Policy != "house" {
		t.Errorf("the broader policy did not apply: %+v", d)
	}
}

// A policy with no clients governs everyone no other policy claims.
func TestDefaultPolicyCatchesEverybodyElse(t *testing.T) {
	set := Compile([]Policy{
		{Name: "everyone", Block: []string{"ads.example"}},
		{Name: "kids", Clients: []netip.Prefix{netip.MustParsePrefix("192.168.1.50/32")},
			Block: []string{"games.example"}},
	})

	if d := set.Decide(netip.MustParseAddr("10.9.9.9"), "ads.example"); !d.Blocked || d.Policy != "everyone" {
		t.Errorf("the default policy did not apply: %+v", d)
	}
	if d := set.Decide(netip.MustParseAddr("192.168.1.50"), "ads.example"); d.Blocked {
		t.Error("the default policy applied to a client another policy claims")
	}
}

// A v4 client arriving on a dual-stack socket presents as ::ffff:192.168.1.50,
// which no IPv4 prefix contains. Without unmapping, every policy silently stops
// applying the moment the relay binds a v6 address.
func TestPolicyMatchesV4MappedClients(t *testing.T) {
	set := Compile([]Policy{{
		Name:    "kids",
		Clients: []netip.Prefix{netip.MustParsePrefix("192.168.1.50/32")},
		Block:   []string{"games.example"},
	}})

	mapped := netip.MustParseAddr("::ffff:192.168.1.50")
	if d := set.Decide(mapped, "games.example"); !d.Blocked {
		t.Error("a v4-mapped client escaped its policy")
	}
}

// The response spelling has to survive compilation, or an operator who chose
// "zero" silently gets NXDOMAIN.
func TestDecideCarriesTheResponseAndTheRuleThatDecided(t *testing.T) {
	set := Compile([]Policy{{Name: "kids", Block: []string{"example.com"}, Response: RespondZero}})

	d := set.Decide(netip.MustParseAddr("192.168.1.10"), "example.com")
	if !d.Blocked {
		t.Fatal("not blocked")
	}
	if d.Response != RespondZero {
		t.Errorf("Response = %q, want %q", d.Response, RespondZero)
	}
	// Naming the rule is what turns "this was blocked" into "this was blocked
	// by the kids policy".
	if d.Policy != "kids" {
		t.Errorf("Policy = %q", d.Policy)
	}
}

func TestCompileDefaultsTheResponse(t *testing.T) {
	set := Compile([]Policy{{Name: "kids", Block: []string{"example.com"}}})
	d := set.Decide(netip.MustParseAddr("192.168.1.10"), "example.com")
	if d.Response != RespondNXDOMAIN {
		t.Errorf("Response = %q, want %q", d.Response, RespondNXDOMAIN)
	}
}

func TestPolicyForNamesTheGoverningRule(t *testing.T) {
	set := Compile([]Policy{{
		Name: "kids", Clients: []netip.Prefix{netip.MustParsePrefix("192.168.1.50/32")},
	}})
	if got := set.PolicyFor(netip.MustParseAddr("192.168.1.50")); got != "kids" {
		t.Errorf("PolicyFor = %q", got)
	}
	if got := set.PolicyFor(netip.MustParseAddr("10.0.0.1")); got != "" {
		t.Errorf("PolicyFor = %q, want empty", got)
	}
}
