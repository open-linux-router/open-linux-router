package daemon

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/dhcp"
	"github.com/open-linux-router/open-linux-router/internal/link"
)

// Migrating a 0.2.0 document.
//
// The failure this guards against is not subtle: 0.2.0 shipped, so there are
// boxes in the field whose `olr.json` keys pools by interface. Getting this
// wrong means a router that stops serving DHCP after an upgrade, which nobody
// notices until the leases expire and then everything breaks at once.

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func storeWith(t *testing.T, document string) *core.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "olr.json")
	if document != "" {
		if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return core.NewStore(path, link.ModuleName, dhcp.ModuleName)
}

// The 0.2.0 shape: pools keyed by interface, range at the top level, IPv6 as a
// sibling `ra` field, and no networks anywhere.
const legacyDocument = `{
  "link": {"adopted": ["lan0"]},
  "dhcp": {
    "enabled": true,
    "pools": [{
      "interface": "lan0",
      "start": "192.168.1.100",
      "end": "192.168.1.200",
      "lease_time": "6h",
      "domain": "lan",
      "ra": "slaac"
    }],
    "reservations": [{"mac": "aa:bb:cc:dd:ee:ff", "ip": "192.168.1.50"}]
  }
}`

func TestMigrateCreatesANetworkForEachLegacyPool(t *testing.T) {
	store := storeWith(t, legacyDocument)
	facts := testFacts(t, "")
	facts.Store = store

	changed, err := migrateDocument(store, facts, quietLogger())
	if err != nil {
		t.Fatalf("migrateDocument: %v", err)
	}
	if !changed {
		t.Fatal("reported no change for a document in the old shape")
	}

	doc, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	linkCfg, err := link.FromDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	dhcpCfg, err := dhcp.FromDocument(doc)
	if err != nil {
		t.Fatal(err)
	}

	g, ok := linkCfg.Group("lan0")
	if !ok {
		t.Fatalf("no network was created; groups = %+v", linkCfg.Groups)
	}
	if len(g.Members) != 1 || g.Members[0] != "lan0" {
		t.Errorf("Members = %v, want [lan0]", g.Members)
	}

	// The subnet the interface *already has*, so applying the migrated config
	// is a no-op on a box that was working. Anything else renumbers a network
	// because olr was upgraded.
	if g.IPv4 == nil || g.IPv4.Subnet.String() != "192.168.1.0/24" {
		t.Fatalf("IPv4 = %+v, want the interface's own 192.168.1.0/24", g.IPv4)
	}
	if g.IPv4.RouterAddr().String() != "192.168.1.2" {
		t.Errorf("router = %s, want the address lan0 already holds (192.168.1.2)",
			g.IPv4.RouterAddr())
	}

	if len(dhcpCfg.Pools) != 1 {
		t.Fatalf("pools = %+v", dhcpCfg.Pools)
	}
	p := dhcpCfg.Pools[0]
	switch {
	case p.Group != "lan0":
		t.Errorf("Group = %q, want lan0", p.Group)
	case p.IPv4 == nil:
		t.Fatal("the IPv4 range was dropped")
	// Carried across explicitly rather than left to be derived: devices hold
	// addresses from this range right now, and re-deriving would renumber a
	// working network.
	case p.IPv4.Start.String() != "192.168.1.100" || p.IPv4.End.String() != "192.168.1.200":
		t.Errorf("range = %s-%s, want the range that was stored", p.IPv4.Start, p.IPv4.End)
	case p.RA() != dhcp.RASLAAC:
		t.Errorf("ipv6 mode = %q, want the `ra` field's slaac", p.RA())
	case p.LeaseTime.String() != "6h":
		t.Errorf("lease = %s, want 6h", p.LeaseTime)
	case p.Domain != "lan":
		t.Errorf("domain = %q", p.Domain)
	}

	if len(dhcpCfg.Reservations) != 1 {
		t.Errorf("reservations were lost: %+v", dhcpCfg.Reservations)
	}
}

// The migrated document has to validate, or the box comes up refusing to serve
// the configuration it just rewrote — which is worse than not migrating at all.
func TestMigratedConfigValidates(t *testing.T) {
	store := storeWith(t, legacyDocument)
	facts := testFacts(t, "")
	facts.Store = store

	if _, err := migrateDocument(store, facts, quietLogger()); err != nil {
		t.Fatal(err)
	}

	doc, _ := store.Load()
	dhcpCfg, _ := dhcp.FromDocument(doc)
	if res := dhcp.Validate(dhcpCfg, dhcpGroupView{facts: facts}); !res.OK() {
		t.Errorf("the migrated config does not validate: %v", res.Errors)
	}
}

// Running twice must not create a second network or re-key anything: a document
// already in the current shape parses as one, so there is nothing to convert.
func TestMigrateIsIdempotent(t *testing.T) {
	store := storeWith(t, legacyDocument)
	facts := testFacts(t, "")
	facts.Store = store

	if _, err := migrateDocument(store, facts, quietLogger()); err != nil {
		t.Fatal(err)
	}
	first, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}

	changed, err := migrateDocument(store, facts, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("reported a change on the second run")
	}

	second, _ := store.Load()
	firstDhcp, _ := dhcp.FromDocument(first)
	secondDhcp, _ := dhcp.FromDocument(second)
	if len(firstDhcp.Pools) != len(secondDhcp.Pools) {
		t.Errorf("pools changed on the second run: %d then %d",
			len(firstDhcp.Pools), len(secondDhcp.Pools))
	}
}

func TestMigrateLeavesACurrentDocumentAlone(t *testing.T) {
	const current = `{"link":{"adopted":["lan0"],` +
		`"groups":[{"name":"lan","members":["lan0"],"ipv4":{"subnet":"192.168.1.0/24"}}]},` +
		`"dhcp":{"enabled":true,"pools":[{"group":"lan"}]}}`

	store := storeWith(t, current)
	facts := testFacts(t, "")
	facts.Store = store

	changed, err := migrateDocument(store, facts, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("rewrote a document that was already in the current shape")
	}
}

// A pool on an interface with no IPv4 address still migrates. The network is
// created without a subnet, which validates as a warning and leaves a row the
// operator can fix — rather than a parse error naming a field they never typed.
func TestMigrateKeepsAPoolWhoseInterfaceHasNoAddress(t *testing.T) {
	const onAbsent = `{"dhcp":{"enabled":true,"pools":[{"interface":"nosuch0",` +
		`"start":"10.0.0.10","end":"10.0.0.20"}]}}`

	store := storeWith(t, onAbsent)
	facts := testFacts(t, "")
	facts.Store = store

	if _, err := migrateDocument(store, facts, quietLogger()); err != nil {
		t.Fatalf("refused to migrate a pool on an absent interface: %v", err)
	}

	doc, _ := store.Load()
	linkCfg, _ := link.FromDocument(doc)
	g, ok := linkCfg.Group("nosuch0")
	if !ok {
		t.Fatal("no network was created for the pool")
	}
	if g.IPv4 != nil {
		t.Errorf("invented a subnet for an interface with no address: %+v", g.IPv4)
	}
	// Adoption comes along: the pool could not have been applied without it,
	// and dropping it turns a merely-invalid config into a confusingly-invalid
	// one.
	if !linkCfg.IsAdopted("nosuch0") {
		t.Error("the interface was not adopted, so the network cannot validate")
	}
}

// Both modules are written in one store.Save. The store is a single document
// and a single rename, so there is no window where the pools are keyed by
// network and the networks do not exist yet.
func TestMigrateWritesBothModulesAtOnce(t *testing.T) {
	store := storeWith(t, legacyDocument)
	facts := testFacts(t, "")
	facts.Store = store

	if _, err := migrateDocument(store, facts, quietLogger()); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"groups"`) || !strings.Contains(text, `"group"`) {
		t.Errorf("the file has one half of the migration but not the other:\n%s", text)
	}
	if strings.Contains(text, `"interface"`) {
		t.Errorf("the old key survived the rewrite:\n%s", text)
	}
}
