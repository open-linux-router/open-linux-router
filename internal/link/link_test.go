package link

import (
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Fixtures shared across this package's tests.
//
// The observed half is always injected. Asserting against a machine's real NICs
// would make the result depend on which machine ran the test, and CI has none
// worth serving DHCP on — which is the whole reason Source is a function.

// storeWith returns a store over a temp file holding the given document.
// An empty body writes no file at all, which is the fresh-install case.
func storeWith(t *testing.T, document string) *core.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "olr.json")
	if document != "" {
		if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return core.NewStore(path, ModuleName, "dhcp")
}

// documentOf parses a document through a store, since core.Document has no
// exported constructor — reading one is how every caller gets one.
func documentOf(t *testing.T, body string) core.Document {
	t.Helper()
	doc, err := storeWith(t, body).Load()
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// prefixes parses a list, failing the test rather than the assertion.
func prefixes(t *testing.T, in ...string) []netip.Prefix {
	t.Helper()
	out := make([]netip.Prefix, 0, len(in))
	for _, s := range in {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// staticSource is a Source over a fixed list.
func staticSource(ifaces ...Interface) Source {
	return func() ([]Interface, error) { return ifaces, nil }
}

// testInterfaces is the shape of an ordinary home box: one wired interface with
// an address, one down, and loopback.
func testInterfaces(t *testing.T) []Interface {
	t.Helper()
	return []Interface{
		{
			Name: "lan0", Index: 2, Up: true, Running: true,
			HardwareAddr: "aa:bb:cc:dd:ee:ff",
			Prefixes:     prefixes(t, "192.168.1.2/24"),
		},
		{Name: "lan1", Index: 3},
		{Name: "lo", Index: 1, Up: true, Running: true, Loopback: true,
			Prefixes: prefixes(t, "127.0.0.1/8")},
	}
}
