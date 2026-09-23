package dial

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/link"
)

// fakeWriter is the kernel this module programs, minus the kernel: an apply
// that always lands, and a box that already has what was asked for.
type fakeWriter struct{ observed Observed }

func (w fakeWriter) Apply(context.Context, Desired) ([]Step, error) {
	return []Step{{Description: "set the uplink", Done: true}}, nil
}

func (w fakeWriter) Observe(context.Context, string) (Observed, error) {
	return w.observed, nil
}

func put(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/uplink", strings.NewReader(body)))
	return w
}

func testStore(t *testing.T, document string) *core.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "olr.json")
	if document != "" {
		if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return core.NewStore(path, link.ModuleName, ModuleName)
}

// The uplink moving, or going away, rebuilds what was built from it — here the
// NAT table, whose egress masquerade names this module's interface (design.md
// §4.1's `dial → gateway` arrow). Without this, a LAN keeps being translated on
// its way out of an interface that is no longer the way out.
func TestAChangedUplinkTakesItsDependentsWithIt(t *testing.T) {
	var calls int
	h := HTTP{
		Applier: Applier{
			Store: testStore(t, ""),
			// A box that already has what the uplink below asks for, so that
			// setting it twice is a change and then a no-op.
			Writer: fakeWriter{observed: Observed{
				Present:    true,
				Up:         true,
				Addrs:      []netip.Prefix{netip.MustParsePrefix("192.168.1.2/24")},
				Gateway:    netip.MustParseAddr("192.168.1.1"),
				GatewayDev: "wan0",
			}},
		},
		Lock:   core.NewLock(),
		Events: core.NewEvents(),
		Dependents: func(context.Context) []core.Step {
			calls++
			return []core.Step{{Description: "re-applied gateway NAT for this change", Done: true}}
		},
	}.Handler()

	const uplink = `{"interface":"wan0","ipv4":{"address":"192.168.1.2/24","gateway":"192.168.1.1"}}`

	w := put(t, h, uplink)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", w.Code, w.Body)
	}

	var got struct {
		Steps []Step `json:"steps"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("dependents ran %d times for one applied change", calls)
	}
	if !slices.ContainsFunc(got.Steps, func(s Step) bool {
		return strings.HasPrefix(s.Description, "re-applied gateway NAT") && s.Done
	}) {
		t.Errorf("a dependent's steps are missing from the response: %+v", got.Steps)
	}

	// The same uplink again changes nothing, and nothing follows it.
	if w := put(t, h, uplink); w.Code != http.StatusOK {
		t.Fatalf("second PUT status = %d, body %s", w.Code, w.Body)
	}
	if calls != 1 {
		t.Errorf("dependents ran %d times; a no-op apply must not take them along", calls)
	}
}
