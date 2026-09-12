package ingress

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The two things on this surface that break silently, and so are the two worth
// testing here rather than trusting:
//
//   - the disruptive gate. A UI with a delete button relies on the daemon
//     refusing, not on the button having asked. If this stops returning 409,
//     every client silently starts unpublishing addresses without a prompt.
//   - the credential round trip. GET redacts, and a client that saves what it
//     read must not store the mask. Both halves are invisible when wrong: the
//     proxy keeps running and the next renewal, weeks later, fails.
//
// Everything else here is covered where the decision lives — validate_test.go,
// plan_test.go, apply_test.go.

func testHTTP(t *testing.T) (http.Handler, Applier) {
	t.Helper()
	applier, proxy := testApplier(t)
	proxy.active = true

	h := HTTP{Applier: applier, Lock: core.NewLock(), Events: core.NewEvents()}
	return h.Handler(), applier
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding %s: %v", w.Body, err)
	}
	return out
}

// publish gets a service onto disk and serving, which is what makes a later
// delete disruptive.
func publish(t *testing.T, h http.Handler) {
	t.Helper()
	body := `{"enabled":true,"certificate":{"provider":"cloudflare","provider_token":"tok"},` +
		`"services":[{"name":"grafana","upstream":{"device":"nuc","port":3000}}]}`
	if w := do(t, h, http.MethodPut, "/config?confirm=true", body); w.Code != http.StatusOK {
		t.Fatalf("setup PUT = %d: %s", w.Code, w.Body)
	}
}

func TestDeletingAPublishedServiceIsRefusedWithoutConfirm(t *testing.T) {
	h, applier := testHTTP(t)
	publish(t, h)

	w := do(t, h, http.MethodDelete, "/services/grafana", "")
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body)
	}

	resp := decode[applyResponse](t, w)
	if resp.Plan.Impact != ImpactDisruptive {
		t.Errorf("impact = %s, want disruptive", resp.Plan.Impact)
	}
	// The refusal has to name what would be lost, or the dialog built on it
	// cannot say anything useful.
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "grafana.home.example.com") {
		t.Errorf("the refusal must name the address, got %+v", resp.Error)
	}
	if !strings.Contains(resp.Error.Message, "confirm=true") {
		t.Errorf("the refusal must say how to go ahead, got %q", resp.Error.Message)
	}

	// Nothing was written. This is the half that matters: a 409 that had already
	// applied would be worse than no gate at all.
	stored, err := applier.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Services) != 1 {
		t.Fatalf("stored services = %v, want the service untouched", stored.Services)
	}
	if resp.Config == nil || len(resp.Config.Services) != 1 {
		t.Error("the refusal should carry the config that is still stored")
	}
}

func TestDeletingWithConfirmGoesAhead(t *testing.T) {
	h, applier := testHTTP(t)
	publish(t, h)

	if w := do(t, h, http.MethodDelete, "/services/grafana?confirm=true", ""); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body)
	}
	stored, err := applier.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Services) != 0 {
		t.Errorf("stored services = %v, want none", stored.Services)
	}
}

// Bare `?confirm` is true, because that is how a flag reads in a URL.
func TestBareConfirmCounts(t *testing.T) {
	h, _ := testHTTP(t)
	publish(t, h)
	if w := do(t, h, http.MethodDelete, "/services/grafana?confirm", ""); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body)
	}
}

func TestMalformedConfirmIsRefused(t *testing.T) {
	h, _ := testHTTP(t)
	publish(t, h)
	// Refused rather than ignored: a client that meant to confirm and mistyped is
	// much better off hearing about it than having the change quietly made.
	if w := do(t, h, http.MethodDelete, "/services/grafana?confirm=yes-please", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	h, applier := testHTTP(t)
	publish(t, h)

	w := do(t, h, http.MethodDelete, "/services/grafana?dry_run=true", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	// A dry run answers with the plan alone, the same shape POST /plan gives.
	plan := decode[planView](t, w)
	if plan.Impact != ImpactDisruptive {
		t.Errorf("impact = %s, want disruptive", plan.Impact)
	}
	stored, _ := applier.Load()
	if len(stored.Services) != 1 {
		t.Errorf("a dry run must change nothing, got %v", stored.Services)
	}
}

func TestDeletingSomethingThatIsNotThereIs404(t *testing.T) {
	h, _ := testHTTP(t)
	publish(t, h)
	if w := do(t, h, http.MethodDelete, "/services/ghost?confirm=true", ""); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body)
	}
}

func TestGetConfigRedactsTheCredential(t *testing.T) {
	h, _ := testHTTP(t)
	publish(t, h)

	w := do(t, h, http.MethodGet, "/config", "")
	if strings.Contains(w.Body.String(), `"tok"`) {
		t.Fatalf("the credential reached the response:\n%s", w.Body)
	}
	cfg := decode[Config](t, w)
	if cfg.Certificate.Token != RedactedToken {
		t.Errorf("token = %q, want the mask", cfg.Certificate.Token)
	}
}

// The failure this guards against: a UI renders the mask into a form, saves the
// form, and stores eight asterisks as the credential. Nothing breaks until a
// renewal fails weeks later.
func TestSavingTheMaskKeepsTheStoredCredential(t *testing.T) {
	h, applier := testHTTP(t)
	publish(t, h)

	read := decode[Config](t, do(t, h, http.MethodGet, "/config", ""))
	back, err := json.Marshal(read)
	if err != nil {
		t.Fatal(err)
	}
	if w := do(t, h, http.MethodPut, "/config?confirm=true", string(back)); w.Code != http.StatusOK {
		t.Fatalf("PUT = %d: %s", w.Code, w.Body)
	}

	stored, err := applier.Load()
	if err != nil {
		t.Fatal(err)
	}
	if stored.Certificate.Token != "tok" {
		t.Fatalf("token = %q, want the original to have survived the round trip", stored.Certificate.Token)
	}
}

func TestANewCredentialStillReplacesTheOldOne(t *testing.T) {
	h, applier := testHTTP(t)
	publish(t, h)

	body := `{"certificate":{"provider_token":"rotated"}}`
	if w := do(t, h, http.MethodPatch, "/config?confirm=true", body); w.Code != http.StatusOK {
		t.Fatalf("PATCH = %d: %s", w.Code, w.Body)
	}
	stored, _ := applier.Load()
	if stored.Certificate.Token != "rotated" {
		t.Errorf("token = %q, want rotated", stored.Certificate.Token)
	}
	// And the patch must not have taken the services with it — the whole reason
	// services get item routes (RFC 7386 replaces an array wholesale).
	if len(stored.Services) != 1 {
		t.Errorf("services = %v, want the one that was there", stored.Services)
	}
}

func TestPlanDiffNeverCarriesTheCredential(t *testing.T) {
	h, _ := testHTTP(t)
	publish(t, h)

	w := do(t, h, http.MethodPatch, "/config?dry_run=true",
		`{"certificate":{"provider_token":"brand-new-secret"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "brand-new-secret") {
		t.Fatalf("a plan diff leaked the credential:\n%s", w.Body)
	}
	if !strings.Contains(w.Body.String(), "withheld") {
		t.Errorf("the operator still has to be told the file changed:\n%s", w.Body)
	}
}

func TestPutServicePathWinsOverBody(t *testing.T) {
	h, applier := testHTTP(t)
	publish(t, h)

	// A caller that disagrees with itself has a bug; honouring the URL is what
	// keeps PUT idempotent on the address it was sent to.
	body := `{"name":"somethingelse","upstream":{"device":"nuc","port":9000}}`
	if w := do(t, h, http.MethodPut, "/services/grafana?confirm=true", body); w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	stored, _ := applier.Load()
	if len(stored.Services) != 1 || stored.Services[0].Name != "grafana" {
		t.Fatalf("stored = %v, want one service named grafana", stored.Services)
	}
	if stored.Services[0].Upstream.Port != 9000 {
		t.Errorf("port = %d, want the body's 9000", stored.Services[0].Upstream.Port)
	}
}

// Re-applying stored intent needs no confirmation: the operator confirmed it when
// they stored it, and the drifted box is the one that most needs repairing.
func TestReapplyNeedsNoConfirm(t *testing.T) {
	h, _ := testHTTP(t)
	publish(t, h)
	if w := do(t, h, http.MethodPost, "/apply", ""); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body)
	}
}
