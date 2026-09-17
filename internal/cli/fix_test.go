package cli

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// A module that is switched on and not what is on the box gets said out loud.
//
// The bug: `olr enable` cleared blockers and printed its success block, and
// blockers are only ever "a package is missing" or "something else holds the
// port". A resolver that was switched on, installed, and exiting every two
// seconds had neither, so the last thing the operator read before going to look
// for the fault was a line saying nothing on the machine had changed. §5.4's
// drift flag is the module telling us it is not what it says it is, and it was
// there in the same reply, unread.
func TestEnableWarnsAboutADriftedModule(t *testing.T) {
	c := serveSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case core.APIPrefix + "/modules":
			core.WriteJSON(w, http.StatusOK, map[string]any{"modules": []string{"dns"}})
		case core.APIPrefix + "/dns/status":
			core.WriteJSON(w, http.StatusOK, map[string]any{"enabled": true, "drifted": true})
		default:
			t.Errorf("unexpected request for %s", r.URL.Path)
		}
	}))

	var out bytes.Buffer
	clearBlockers(context.Background(), c, &out)

	got := out.String()
	if !strings.Contains(got, "warning") {
		t.Errorf("an enabled, drifted module produced no warning:\n%s", got)
	}
	if !strings.Contains(got, "dns") {
		t.Errorf("the warning does not name the module:\n%s", got)
	}
}

// A module that is switched off is not drifted at anybody — it is off, which is
// what the operator asked for. Saying so on every install would be the kind of
// warning that teaches people to stop reading them.
func TestEnableIsQuietWhenTheModuleIsOff(t *testing.T) {
	c := serveSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case core.APIPrefix + "/modules":
			core.WriteJSON(w, http.StatusOK, map[string]any{"modules": []string{"dns"}})
		case core.APIPrefix + "/dns/status":
			core.WriteJSON(w, http.StatusOK, map[string]any{"enabled": false, "drifted": true})
		default:
			t.Errorf("unexpected request for %s", r.URL.Path)
		}
	}))

	var out bytes.Buffer
	clearBlockers(context.Background(), c, &out)
	if out.Len() != 0 {
		t.Errorf("a module that is switched off was reported as something in the way:\n%s", out.String())
	}
}

// And a module that is switched on and matching the box says nothing, which is
// the ordinary case: this is the path every install takes.
func TestEnableIsQuietWhenTheBoxMatches(t *testing.T) {
	c := serveSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case core.APIPrefix + "/modules":
			core.WriteJSON(w, http.StatusOK, map[string]any{"modules": []string{"dns"}})
		case core.APIPrefix + "/dns/status":
			core.WriteJSON(w, http.StatusOK, map[string]any{"enabled": true, "drifted": false})
		default:
			t.Errorf("unexpected request for %s", r.URL.Path)
		}
	}))

	var out bytes.Buffer
	clearBlockers(context.Background(), c, &out)
	if out.Len() != 0 {
		t.Errorf("a module that matches the box produced output:\n%s", out.String())
	}
}
