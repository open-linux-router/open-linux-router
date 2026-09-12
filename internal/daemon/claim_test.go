package daemon

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/system"
)

// docs/system.md §3 is the only thing standing between "olrd listens the moment
// it is installed" and an unauthenticated admin API appearing on a LAN with no
// human action. So it is tested as the invariant it is, rather than through the
// UI that happens to present it:
//
//	An unclaimed box cannot be configured over the network.

// claim state fixtures.
const (
	unclaimed = false
	claimed   = true
)

// newApplier builds an applier over a scratch document, claimed or not.
func newApplier(t *testing.T, isClaimed bool) system.Applier {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "olr.json")
	body := "{}\n"
	if isClaimed {
		body = `{"system":{"access":{"claimed_at":"2026-09-12T00:00:00Z","password":null}}}` + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return system.Applier{Store: core.NewStore(path, system.ModuleName)}
}

func TestAnUnclaimedBoxRefusesToBeConfiguredOverTheNetwork(t *testing.T) {
	handler := gateUnclaimed(newApplier(t, unclaimed), stub())

	// One read and one write, because a box that will not accept changes but
	// will hand out its whole configuration is still leaking a network map.
	for _, path := range []string{
		core.APIPrefix + "/dhcp/config",
		core.APIPrefix + "/modules",
		core.APIPrefix + "/link/interfaces",
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusConflict {
			t.Errorf("GET %s on an unclaimed box = %d, want 409", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "not been set up") {
			t.Errorf("GET %s does not say why:\n%s", path, rec.Body.String())
		}
	}
}

// The page has to load, or the choice cannot be offered — the v0.1.6 lesson,
// kept as a test so the gate cannot re-learn it.
func TestAnUnclaimedBoxStillServesTheUI(t *testing.T) {
	handler := gateUnclaimed(newApplier(t, unclaimed), stub())

	for _, path := range []string{"/", "/index.html", "/assets/app.js", "/devices"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s on an unclaimed box = %d, want 200", path, rec.Code)
		}
	}
}

// And the one route that ends the unclaimed state has to be reachable by
// somebody who has no credential, since acquiring one is what it is for.
func TestTheClaimRouteIsReachableOnAnUnclaimedBox(t *testing.T) {
	handler := gateUnclaimed(newApplier(t, unclaimed), stub())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, claimPath, nil))
	if rec.Code == http.StatusConflict {
		t.Fatal("the gate blocked the claim route, so an unclaimed box can never be claimed")
	}
}

func TestAClaimedBoxIsConfigurable(t *testing.T) {
	handler := gateUnclaimed(newApplier(t, claimed), stub())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, core.APIPrefix+"/modules", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET on a claimed box = %d, want 200", rec.Code)
	}
}

// A document that cannot be read must fail closed. The operator's way in is the
// unix socket, which the gate never touches, so refusing costs them nothing and
// guessing "probably claimed" would cost them everything.
func TestAnUnreadableConfigFailsClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "olr.json")
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	applier := system.Applier{Store: core.NewStore(path, system.ModuleName)}

	rec := httptest.NewRecorder()
	gateUnclaimed(applier, stub()).ServeHTTP(
		rec, httptest.NewRequest(http.MethodGet, core.APIPrefix+"/modules", nil))

	if rec.Code == http.StatusOK {
		t.Errorf("an unreadable config let a request through: %d", rec.Code)
	}
}

// Claiming is restricted by source range (docs/system.md §4). The case it
// exists for is olr on a box with a routable address, where the first thing to
// find an open port is a scanner and not its owner.
func TestOnlyTheLocalNetworkMayClaim(t *testing.T) {
	h := system.HTTP{Applier: newApplier(t, unclaimed)}

	for _, tc := range []struct {
		remote string
		allow  bool
	}{
		{"192.168.1.31:51000", true},
		{"10.0.0.5:51000", true},
		{"172.16.2.10:51000", true},
		{"127.0.0.1:51000", true},
		{"[::1]:51000", true},
		{"[fe80::1]:51000", true},
		{"[fd00::1]:51000", true},
		{"203.0.113.7:51000", false},
		{"[2001:db8::1]:51000", false},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, claimPath,
			strings.NewReader(`{"password":"","no_password":true}`))
		req.RemoteAddr = tc.remote

		core.RouteTable(h.Routes()).ServeHTTP(rec, stripPrefix(req))

		forbidden := rec.Code == http.StatusForbidden
		if tc.allow && forbidden {
			t.Errorf("%s was refused; a local client must be able to claim", tc.remote)
		}
		if !tc.allow && !forbidden {
			t.Errorf("%s was allowed to claim (%d); only the local network may",
				tc.remote, rec.Code)
		}
	}
}

// stripPrefix mimics what core does before a module sees a request.
func stripPrefix(r *http.Request) *http.Request {
	out := r.Clone(r.Context())
	out.URL.Path = strings.TrimPrefix(r.URL.Path, core.APIPrefix+"/"+system.ModuleName)
	return out
}
