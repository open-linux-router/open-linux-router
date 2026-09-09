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
// These tests assert the two halves of the fix through the adapter olrd
// actually uses: the interface's addresses come from the machine, and its
// adoption comes from the document an operator can write.

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

// poolOn is an ordinary range on lan0, well inside the interface's subnet and
// clear of its own address — so the only thing that can make it invalid is
// adoption.
func poolOn(iface string) dhcp.Config {
	return dhcp.Config{
		Enabled: true,
		Pools: []dhcp.Pool{{
			Interface: iface,
			Start:     netip.MustParseAddr("192.168.1.100"),
			End:       netip.MustParseAddr("192.168.1.200"),
		}},
	}
}

func TestAPoolIsRefusedUntilTheInterfaceIsAdopted(t *testing.T) {
	links := dhcpLinkView{facts: testFacts(t, "")}

	res := dhcp.Validate(poolOn("lan0"), links)
	if res.OK() {
		t.Fatal("dhcp accepted a pool on an interface nobody adopted")
	}

	var mentionsAdoption bool
	for _, p := range res.Errors {
		if strings.Contains(p.Message, "not adopted") {
			mentionsAdoption = true
		}
	}
	if !mentionsAdoption {
		t.Errorf("errors = %v, want one saying the interface is not adopted", res.Errors)
	}
}

func TestAPoolValidatesOnceTheInterfaceIsAdopted(t *testing.T) {
	links := dhcpLinkView{facts: testFacts(t, `{"link":{"adopted":["lan0"]}}`)}

	if res := dhcp.Validate(poolOn("lan0"), links); !res.OK() {
		t.Fatalf("dhcp refused a pool on an adopted interface: %v", res.Errors)
	}
}

// The other half: an interface olrd was never told about still resolves,
// because the facts come from the machine rather than from a file somebody had
// to write. Before this, an empty --links made every interface unknown.
func TestInterfaceFactsComeFromTheMachineNotAFile(t *testing.T) {
	links := dhcpLinkView{facts: testFacts(t, "")}

	info, err := links.Interface("lan0")
	if err != nil {
		t.Fatalf("lan0 exists on the machine but the adapter reports: %v", err)
	}
	if len(info.Prefixes) != 1 || info.Prefixes[0].String() != "192.168.1.2/24" {
		t.Errorf("Prefixes = %v, want the observed 192.168.1.2/24", info.Prefixes)
	}
	if !info.Up {
		t.Error("Up is false for an interface the machine reports as up")
	}
}

// The sentinel is re-wrapped as the consumer's own, so a caller testing
// errors.Is against dhcp.ErrNoSuchInterface still gets a true answer.
func TestUnknownInterfaceReportsTheConsumersSentinel(t *testing.T) {
	links := dhcpLinkView{facts: testFacts(t, "")}

	_, err := links.Interface("nope")
	if err == nil {
		t.Fatal("the adapter accepted an interface the machine does not have")
	}
	if !errors.Is(err, dhcp.ErrNoSuchInterface) {
		t.Errorf("err = %v, want dhcp.ErrNoSuchInterface", err)
	}
}

// All three consumers read the same Facts, so adoption reaching one of them and
// not another would be a silent, module-specific failure.
func TestEveryConsumerSeesTheSameAdoption(t *testing.T) {
	facts := testFacts(t, `{"link":{"adopted":["lan0"]}}`)

	dhcpInfo, err := dhcpLinkView{facts: facts}.Interface("lan0")
	if err != nil {
		t.Fatal(err)
	}
	dnsInfo, err := dnsLinkView{facts: facts}.Interface("lan0")
	if err != nil {
		t.Fatal(err)
	}
	gatewayInfo, err := gatewayLinkView{facts: facts}.Interface("lan0")
	if err != nil {
		t.Fatal(err)
	}

	if !dhcpInfo.Adopted || !dnsInfo.Adopted || !gatewayInfo.Adopted {
		t.Errorf("adoption reached dhcp=%v dns=%v gateway=%v; want all three",
			dhcpInfo.Adopted, dnsInfo.Adopted, gatewayInfo.Adopted)
	}
}
