package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func newTestStore(t *testing.T) *core.Store {
	t.Helper()
	return core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName)
}

func newTestHandler(t *testing.T, k Kernel) (http.Handler, Applier) {
	t.Helper()
	a := Applier{Kernel: k, Links: testLinks(), Store: newTestStore(t)}
	h := HTTP{Applier: a, Lock: core.NewLock(), Events: core.NewEvents()}
	return h.Handler(), a
}

func do(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	// The common case, and the one that must not look disruptive: a local
	// caller over the unix socket, whose connection no routing change can move.
	req.RemoteAddr = "@"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding %s: %v", w.Body.String(), err)
	}
	return out
}

func TestGetConfigOnAFreshBoxIsAnEmptyState(t *testing.T) {
	h, _ := newTestHandler(t, &StaticKernel{})

	w := do(t, h, http.MethodGet, "/config", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	cfg := decode[Config](t, w)
	if !cfg.Empty() {
		t.Errorf("a box nobody has configured should read as empty, got %+v", cfg)
	}
}

func TestPutConfigStoresAndProgramsIt(t *testing.T) {
	k := &StaticKernel{}
	h, a := newTestHandler(t, k)

	w := do(t, h, http.MethodPut, "/config", testConfig())
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}

	stored, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Exits) != 2 {
		t.Fatalf("intent not stored: %+v", stored)
	}
	if len(k.State) == 0 {
		t.Fatal("nothing reached the kernel")
	}

	// And the second identical PUT is a no-op, which is what makes re-applying
	// safe as a repair path.
	w = do(t, h, http.MethodPut, "/config", stored)
	plan := decode[applyResponse](t, w).Plan
	if !plan.Empty {
		t.Errorf("re-applying the same config should change nothing, got %v", plan.Changes)
	}
}

func TestPutRejectsAnInvalidConfigBeforeTouchingAnything(t *testing.T) {
	k := &StaticKernel{}
	h, _ := newTestHandler(t, k)

	bad := testConfig()
	bad.Default = "Nope"

	w := do(t, h, http.MethodPut, "/config", bad)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422: %s", w.Code, w.Body)
	}
	if len(k.State) != 0 {
		t.Error("nothing should have been programmed")
	}
}

func TestPutRejectsUnknownFields(t *testing.T) {
	h, _ := newTestHandler(t, &StaticKernel{})

	req := httptest.NewRequest(http.MethodPut, "/config",
		strings.NewReader(`{"enabled":true,"exitz":[]}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("a mistyped key should be a 400, got %d: %s", w.Code, w.Body)
	}
}

func TestPatchLeavesUnnamedFieldsAlone(t *testing.T) {
	h, a := newTestHandler(t, &StaticKernel{})
	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}

	w := do(t, h, http.MethodPatch, "/config", map[string]any{"enabled": false})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}

	stored, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if stored.Enabled {
		t.Error("the patched field did not take")
	}
	if len(stored.Exits) != 2 {
		t.Errorf("a patch naming one field should not drop the exits: %+v", stored)
	}
}

func TestPlanDoesNotChangeAnything(t *testing.T) {
	k := &StaticKernel{}
	h, _ := newTestHandler(t, k)

	w := do(t, h, http.MethodPost, "/plan", testConfig())
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	plan := decode[planView](t, w)
	if plan.Empty {
		t.Error("planning a config against an empty kernel should show work")
	}
	if plan.Diff == "" {
		t.Error("an impact with no diff is a scarier spinner, not an explanation")
	}
	if len(k.State) != 0 {
		t.Error("a dry run must not program anything")
	}
}

func TestPlanWithNoBodyIsTheDriftCheck(t *testing.T) {
	h, _ := newTestHandler(t, &StaticKernel{})
	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}

	w := do(t, h, http.MethodPost, "/plan", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if plan := decode[planView](t, w); !plan.Empty {
		t.Errorf("a freshly applied box should not have drifted: %v", plan.Changes)
	}
}

// §6's refusal is a conflict the operator resolves elsewhere, not something
// that went wrong here — so it is a 409, not a 500.
func TestForeignRulesAnswerWithAConflict(t *testing.T) {
	k := &StaticKernel{Foreign: []ForeignRule{
		{Priority: 9000, Family: "ip", Table: 2022, HasDefault: true},
	}}
	h, _ := newTestHandler(t, k)

	w := do(t, h, http.MethodPut, "/config", testConfig())
	if w.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409: %s", w.Code, w.Body)
	}
	resp := decode[applyResponse](t, w)
	if resp.Plan.Blocked == "" {
		t.Error("the refusal reason should be on the plan")
	}
	if len(k.State) != 0 {
		t.Error("nothing should have been programmed")
	}
}

func TestApplyRepairsDrift(t *testing.T) {
	k := &StaticKernel{}
	h, _ := newTestHandler(t, k)
	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}

	// Somebody flushed our rules by hand.
	k.State = nil

	w := do(t, h, http.MethodPost, "/apply", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if len(k.State) == 0 {
		t.Fatal("re-applying should have put it back")
	}
}

func TestStatusReportsEffectiveValuesAndTheirSource(t *testing.T) {
	h, _ := newTestHandler(t, &StaticKernel{})
	cfg := testConfig()
	cfg.Default = "Clash"
	if w := do(t, h, http.MethodPut, "/config", cfg); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}

	w := do(t, h, http.MethodGet, "/status", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	st := decode[statusView](t, w)

	if len(st.Exits) != 2 || len(st.Assignments) != 2 {
		t.Fatalf("unexpected shape: %+v", st)
	}
	for _, a := range st.Assignments {
		if a.Source == "" {
			// §2.2: inheritance is unusable if the answer's source is not
			// visible.
			t.Errorf("%s has no source", a.Interface)
		}
	}
	for _, e := range st.Exits {
		if e.Mark == "" || e.Table == 0 || e.Priority == 0 {
			t.Errorf("%s does not publish its kernel resources: %+v", e.Name, e)
		}
		if e.Probed {
			t.Errorf("%s has no probe configured but claims to be checked", e.Name)
		}
	}
	if st.AsOf.IsZero() {
		t.Error("observed data must be stamped (§4.5)")
	}
}

func TestStatusOnAnUnreadableKernelSaysSo(t *testing.T) {
	h, _ := newTestHandler(t, &StaticKernel{Unknown: true})

	w := do(t, h, http.MethodGet, "/status", nil)
	st := decode[statusView](t, w)
	if st.Known {
		t.Error("an unreadable kernel must not report itself as known")
	}
	if st.Drifted {
		t.Error("a kernel we could not read has not drifted; it is unknown")
	}
}

func TestStatusSaysWhenAnExitIsDown(t *testing.T) {
	prober := NewProber()
	prober.health["Clash"] = false

	a := Applier{
		Kernel: &StaticKernel{}, Links: testLinks(), Store: newTestStore(t), Probes: prober,
	}
	h := HTTP{Applier: a, Lock: core.NewLock(), Events: core.NewEvents()}.Handler()
	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}

	st := decode[statusView](t, do(t, h, http.MethodGet, "/status", nil))
	var reason string
	for _, as := range st.Assignments {
		if as.Interface == "br-lan" {
			reason = as.Reason
		}
	}
	// §2.2: the failure surfaces in the place the operator already looks, in
	// words they can act on.
	if !strings.Contains(reason, "no internet") || !strings.Contains(reason, "Clash") {
		t.Errorf("br-lan should say why it has no internet, got %q", reason)
	}
}

func TestApplyPublishesAnEventOnlyWhenSomethingChanged(t *testing.T) {
	events := core.NewEvents()
	a := Applier{Kernel: &StaticKernel{}, Links: testLinks(), Store: newTestStore(t)}
	h := HTTP{Applier: a, Lock: core.NewLock(), Events: events}.Handler()

	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}
	// A no-op store would otherwise wake every client to re-read identical
	// bytes.
	w := do(t, h, http.MethodPut, "/config", testConfig())
	if plan := decode[applyResponse](t, w).Plan; !plan.Empty {
		t.Errorf("the second identical apply should be empty, got %v", plan.Changes)
	}
}

func TestWatchIsCalledAfterASuccessfulApply(t *testing.T) {
	var got Config
	a := Applier{Kernel: &StaticKernel{}, Links: testLinks(), Store: newTestStore(t)}
	h := HTTP{
		Applier: a, Lock: core.NewLock(), Events: core.NewEvents(),
		Watch: func(c Config) { got = c },
	}.Handler()

	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if len(got.Exits) != 2 {
		t.Errorf("the prober was not told about the new config: %+v", got)
	}
}

func TestUnsupportedKernelRefusesRatherThanPretending(t *testing.T) {
	// The non-Linux build. Reporting an empty kernel that accepted everything
	// would let a test pass against behaviour that has never run.
	k := unsupportedStub{}
	obs, err := k.Observe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if obs.Known {
		t.Error("it should report that it does not know")
	}
	if _, err := k.Apply(context.Background(), Desired{}); err == nil {
		t.Error("applying should refuse")
	}
}

// unsupportedStub mirrors kernel_other.go so the expectation is asserted on
// every platform, including the Linux one where that file is not built.
type unsupportedStub struct{}

func (unsupportedStub) Observe(context.Context) (Observed, error) {
	return Observed{Known: false}, nil
}

func (unsupportedStub) Apply(context.Context, Desired) ([]Step, error) {
	return nil, ErrUnsupported
}
