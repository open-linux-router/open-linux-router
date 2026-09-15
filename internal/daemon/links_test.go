package daemon

import (
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/dhcp"
	"github.com/open-linux-router/open-linux-router/internal/link"
)

// The bug this whole path exists to fix, pinned.
//
// Before the link module, olrd was started by its own systemd unit with no
// --links flag, so `dhcp` was handed an empty interface list and refused every
// pool with "no such interface" — a packaged install could not configure DHCP
// at all. Even with a file, the pool needed `adopted: true`, and `olr adopt`
// was a stub, so the flag was unreachable from every supported surface.
//
// These tests assert the fix through the adapter olrd actually uses. What the
// adapter hands `dhcp` changed shape here: it used to be an interface with its
// observed addresses, and it is now a network with its stored subnet — so a
// range is checked against something an operator can change from inside olr.

func testFacts(t *testing.T, document string) link.Facts {
	t.Helper()
	path := filepath.Join(t.TempDir(), "olr.json")
	if document != "" {
		if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return link.Facts{
		Store: core.NewStore(path, link.ModuleName, dhcp.ModuleName),
		Source: func() ([]link.Interface, error) {
			return []link.Interface{{
				Name: "lan0", Index: 2, Up: true, Running: true,
				Prefixes: []netip.Prefix{netip.MustParsePrefix("192.168.1.2/24")},
			}}, nil
		},
	}
}

// poolOn is an ordinary pool on a network, with the range left to be derived.
//
// Nothing about it can be invalid except the network it names, which is the
// point: `dhcp` no longer has an opinion about interfaces at all.
func poolOn(group string) dhcp.Config {
	return dhcp.Config{
		Enabled: true,
		Pools:   []dhcp.Pool{{Group: group, IPv4: &dhcp.PoolIPv4{}}},
	}
}

// The document a working box has: lan0 adopted, and a network on it.
const lanDocument = `{"link":{"adopted":["lan0"],` +
	`"groups":[{"name":"lan","members":["lan0"],"ipv4":{"subnet":"192.168.1.0/24"}}]}}`

func TestAPoolIsRefusedUntilTheNetworkExists(t *testing.T) {
	groups := dhcpGroupView{facts: testFacts(t, "")}

	res := dhcp.Validate(poolOn("lan"), groups)
	if res.OK() {
		t.Fatal("dhcp accepted a pool on a network that does not exist")
	}

	var mentionsNetwork bool
	for _, p := range res.Errors {
		if strings.Contains(p.Message, "no such network") {
			mentionsNetwork = true
		}
	}
	if !mentionsNetwork {
		t.Errorf("errors = %v, want one saying the network does not exist", res.Errors)
	}
}

func TestAPoolValidatesAgainstTheNetworksStoredSubnet(t *testing.T) {
	groups := dhcpGroupView{facts: testFacts(t, lanDocument)}

	if res := dhcp.Validate(poolOn("lan"), groups); !res.OK() {
		t.Fatalf("dhcp refused a pool on a configured network: %v", res.Errors)
	}
}

// The property the whole change exists to create: the subnet a range is checked
// against comes from the *document*, not from the address the interface happens
// to be holding. Here they disagree — lan0 has 192.168.1.2/24 while the network
// says 172.16.1.0/24 — and the range that validates is the one in the network's
// subnet.
//
// Before this, the opposite was true, and there was no page in olr where the
// operator could change the address that was overruling them.
func TestARangeIsCheckedAgainstIntentNotTheKernel(t *testing.T) {
	const renumbered = `{"link":{"adopted":["lan0"],` +
		`"groups":[{"name":"lan","members":["lan0"],"ipv4":{"subnet":"172.16.1.0/24"}}]}}`
	groups := dhcpGroupView{facts: testFacts(t, renumbered)}

	inIntent := dhcp.Config{Enabled: true, Pools: []dhcp.Pool{{
		Group: "lan",
		IPv4: &dhcp.PoolIPv4{
			Start: netip.MustParseAddr("172.16.1.100"),
			End:   netip.MustParseAddr("172.16.1.200"),
		},
	}}}
	if res := dhcp.Validate(inIntent, groups); !res.OK() {
		t.Errorf("refused a range inside the network's own subnet: %v", res.Errors)
	}

	inKernel := dhcp.Config{Enabled: true, Pools: []dhcp.Pool{{
		Group: "lan",
		IPv4: &dhcp.PoolIPv4{
			Start: netip.MustParseAddr("192.168.1.100"),
			End:   netip.MustParseAddr("192.168.1.200"),
		},
	}}}
	if res := dhcp.Validate(inKernel, groups); res.OK() {
		t.Error("accepted a range that is in the interface's current subnet but not the network's")
	}
}

// The sentinel is re-wrapped as the consumer's own, so a caller testing
// errors.Is against dhcp.ErrNoSuchGroup still gets a true answer.
func TestUnknownNetworkReportsTheConsumersSentinel(t *testing.T) {
	groups := dhcpGroupView{facts: testFacts(t, "")}

	_, err := groups.Group("nope")
	if err == nil {
		t.Fatal("the adapter accepted a network nobody configured")
	}
	if !errors.Is(err, dhcp.ErrNoSuchGroup) {
		t.Errorf("err = %v, want dhcp.ErrNoSuchGroup", err)
	}
}

// The observed half still comes from the machine, not from a file somebody had
// to write — which is what the link module landed to fix. A network is up when
// its members are.
func TestNetworkStateComesFromTheMachine(t *testing.T) {
	groups := dhcpGroupView{facts: testFacts(t, lanDocument)}

	info, err := groups.Group("lan")
	if err != nil {
		t.Fatalf("lan is configured but the adapter reports: %v", err)
	}
	if !info.Up {
		t.Error("Up is false for a network whose only member the machine reports as up")
	}
	if info.Subnet.String() != "192.168.1.0/24" {
		t.Errorf("Subnet = %s, want the stored 192.168.1.0/24", info.Subnet)
	}
	if info.Router.String() != "192.168.1.1" {
		t.Errorf("Router = %s, want the derived 192.168.1.1", info.Router)
	}
}

// The consumers still keyed by interface read the same Facts, so adoption
// reaching one of them and not another would be a silent, module-specific
// failure. `dhcp` is no longer among them — it reads networks, and a network's
// members are adopted by construction (`link` refuses otherwise), which is why
// the adoption check left this module entirely.
func TestEveryInterfaceConsumerSeesTheSameAdoption(t *testing.T) {
	facts := testFacts(t, `{"link":{"adopted":["lan0"]}}`)

	dnsInfo, err := dnsLinkView{facts: facts}.Interface("lan0")
	if err != nil {
		t.Fatal(err)
	}
	gatewayInfo, err := gatewayLinkView{facts: facts}.Interface("lan0")
	if err != nil {
		t.Fatal(err)
	}
	firewallInfo, err := firewallLinkView{facts: facts}.Interface("lan0")
	if err != nil {
		t.Fatal(err)
	}

	if !dnsInfo.Adopted || !gatewayInfo.Adopted || !firewallInfo.Adopted {
		t.Errorf("adoption reached dns=%v gateway=%v firewall=%v; want all three",
			dnsInfo.Adopted, gatewayInfo.Adopted, firewallInfo.Adopted)
	}
}
