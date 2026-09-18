package remote

import (
	"net/http"
	"testing"
)

// This module answers the three generic questions at the top level of its
// status, like every other module that can be switched on.
//
// The bug this holds shut was invisible by construction. internal/cli's
// blocker-clearing and drift warning decode `enabled`, `drifted` and `blockers`
// out of whatever module they are pointed at — a deliberate fragment, because
// internal/cli cannot import the modules that import it. This module is two
// objects, a tunnel and a proxy, and it carried all three fields one level down
// under `tunnel` and `proxy` with nothing above them. The fragment therefore
// decoded three zero values, which is exactly what a module that is switched
// off looks like, and `olr enable` skipped `remote` in silence: no blockers
// cleared, no drift reported, and nothing anywhere saying it had been passed
// over.
//
// Built on testProxyHTTP rather than testHTTP, and that is not incidental.
// testHTTP leaves the proxy applier zero-valued, which leaves its Paths empty,
// which makes its observer walk the process's working directory and plan a
// delete for every file it finds there — this package's own source. A status
// test on that handler sees `proxy.drifted` true on a box where nothing is
// configured, and would have been written to expect it.
type statusShape struct {
	Enabled  bool `json:"enabled"`
	Drifted  bool `json:"drifted"`
	Blockers []struct {
		ID string `json:"id"`
	} `json:"blockers"`

	Tunnel struct {
		Enabled bool `json:"enabled"`
		Drifted bool `json:"drifted"`
	} `json:"tunnel"`
	Proxy struct {
		Enabled bool `json:"enabled"`
		Drifted bool `json:"drifted"`
	} `json:"proxy"`
}

func moduleStatusOf(t *testing.T, h http.Handler) statusShape {
	t.Helper()
	w := do(t, h, http.MethodGet, "/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /status = %d: %s", w.Code, w.Body)
	}
	return decode[statusShape](t, w)
}

// A module nobody has configured is off and matching, and says so.
//
// This is the case that has to stay quiet: it is the state of `remote` on every
// box that installs olr for DHCP and a resolver, and a drift warning here would
// fire on each one of those installs. A warning that fires on a correct box is
// how operators learn to stop reading warnings, which would cost the drift
// warning the thing it was added for.
func TestModuleStatusReportsAFreshModuleOffAndClean(t *testing.T) {
	h, _ := testProxyHTTP(t)
	s := moduleStatusOf(t, h)

	if s.Enabled {
		t.Errorf("a module with neither half switched on reported itself on: %+v", s)
	}
	if s.Drifted {
		t.Errorf("a module nobody has configured reported drift; `olr enable` would warn on every install: %+v", s)
	}
}

// Either half being on makes the module on. The operator-facing sentence is
// "something you switched on is not what is on the box", and one of two is
// enough for that; `olr remote status` is where they find out which.
func TestModuleStatusIsOnWhenTheTunnelIsOn(t *testing.T) {
	h, _ := testProxyHTTP(t)
	turnOn(t, h)

	s := moduleStatusOf(t, h)
	if !s.Tunnel.Enabled {
		t.Fatalf("setup did not switch the tunnel on: %+v", s)
	}
	if !s.Enabled {
		t.Error("the tunnel was switched on and the module did not say so at the top level; `olr enable` would skip this module as if it were off")
	}
}

// The other half, switched on by its own route, reaches the same top-level
// answer. Worth its own case because the two halves are separate objects with
// separate config and separate appliers — nothing but this aggregation makes
// them one module to a generic client.
func TestModuleStatusIsOnWhenOnlyTheProxyIsOn(t *testing.T) {
	h, _ := testProxyHTTP(t)
	if w := do(t, h, http.MethodPatch, "/config?confirm=true",
		`{"endpoint":"home.example.net"}`); w.Code != http.StatusOK {
		t.Fatalf("setup endpoint PATCH = %d: %s", w.Code, w.Body)
	}
	if w := do(t, h, http.MethodPatch, "/shadowsocks/config?confirm=true",
		`{"enabled":true}`); w.Code != http.StatusOK {
		t.Fatalf("setup proxy PATCH = %d: %s", w.Code, w.Body)
	}

	s := moduleStatusOf(t, h)
	if !s.Proxy.Enabled {
		t.Fatalf("setup did not switch the proxy on: %+v", s)
	}
	if s.Tunnel.Enabled {
		t.Fatalf("setup switched the tunnel on too; this case is meant to be the proxy alone: %+v", s)
	}
	if !s.Enabled {
		t.Error("the proxy was switched on and the module did not say so at the top level")
	}
}

// The top level must never disagree with the halves it is summarising — in
// either direction. A module reporting itself clean over a drifted half is the
// original bug; one reporting drift with both halves clean would cry wolf.
func TestModuleStatusAgreesWithItsHalves(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, h http.Handler)
	}{
		{"fresh", func(*testing.T, http.Handler) {}},
		{"tunnel on", turnOn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := testProxyHTTP(t)
			tc.setup(t, h)
			s := moduleStatusOf(t, h)

			if want := s.Tunnel.Enabled || s.Proxy.Enabled; s.Enabled != want {
				t.Errorf("enabled = %v, but tunnel=%v proxy=%v", s.Enabled, s.Tunnel.Enabled, s.Proxy.Enabled)
			}
			if want := s.Tunnel.Drifted || s.Proxy.Drifted; s.Drifted != want {
				t.Errorf("drifted = %v, but tunnel=%v proxy=%v", s.Drifted, s.Tunnel.Drifted, s.Proxy.Drifted)
			}
		})
	}
}
