package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Moving the tunnel moves an address another module was rendered to answer on.
//
// The resolver is that module (internal/dns/dialin.go): its listen set contains
// the dial-in address because every client configuration names it, so a change
// here has to re-apply it. Nothing in dns's own intent changed, which is
// exactly the case design.md §4.1 says cannot be noticed from the far end.
func TestApplyingTheTunnelReappliesWhatWasBuiltFromIt(t *testing.T) {
	applier, _ := testApplier(t)
	var followed int
	applier.Follow = func(context.Context) []core.Step {
		followed++
		return []core.Step{{Description: "re-apply dns for this change", Done: true}}
	}
	h := HTTP{Tunnel: applier, Lock: core.NewLock(), Events: core.NewEvents()}.Handler()

	// turnOn's second request is the first thing that reaches the kernel, so it
	// is the first apply with a follow.
	turnOn(t, h)
	if followed == 0 {
		t.Fatal("turning the tunnel on did not re-apply what is built from its address")
	}

	// And the step is reported on the response, not only in the journal: the
	// resolver is another module's work, and an operator reading the answer
	// should see that it happened.
	w := do(t, h, http.MethodPatch, "/wireguard/config?confirm=true", `{"listen_port":51821}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	var body tunnelApplyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding %s: %v", w.Body, err)
	}
	if !slices.ContainsFunc(body.Steps, func(s Step) bool {
		return s.Description == "re-apply dns for this change"
	}) {
		t.Errorf("steps = %v, want the dependent's step among them", body.Steps)
	}
}
