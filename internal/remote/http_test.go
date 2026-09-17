package remote

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The three things on this surface that break silently, and so are the three
// worth testing here rather than trusting:
//
//   - the one-shot client configuration. It is the only response in olr that
//     cannot be reproduced, so a bug that drops it costs the operator a device
//     and a bug that repeats it means the key was stored after all.
//   - the disruptive gate. A UI with a delete button relies on the daemon
//     refusing, not on the button having asked.
//   - the key round trip. GET redacts, and a client that saves what it read
//     must not store the mask — the tunnel would come up with a key that is
//     eight asterisks and every configuration ever issued would be wrong.
//
// Everything else is covered where the decision lives — validate_test.go,
// plan_test.go, render_test.go.

func testHTTP(t *testing.T) (http.Handler, Applier) {
	t.Helper()
	applier, _ := testApplier(t)
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

// turnOn gets the tunnel configured and running, which is what makes a later
// removal disruptive.
func turnOn(t *testing.T, h http.Handler) {
	t.Helper()
	body := `{"wireguard":{"enabled":true,"endpoint":"home.example.net"}}`
	if w := do(t, h, http.MethodPatch, "/config?confirm=true", body); w.Code != http.StatusOK {
		t.Fatalf("setup PATCH = %d: %s", w.Code, w.Body)
	}
}

// Enabling generates the key. Without this an operator would have to know that
// a tunnel needs one, and there is nothing they could usefully choose.
func TestEnablingGeneratesTheKey(t *testing.T) {
	h, applier := testHTTP(t)
	turnOn(t, h)

	cfg, err := applier.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidKey(cfg.WireGuard.PrivateKey); err != nil {
		t.Fatalf("no usable key after enabling: %v", err)
	}

	// And it is stable: a second edit must not replace it, or every
	// configuration issued before it would name a public key the box no longer
	// has.
	if w := do(t, h, http.MethodPatch, "/config?confirm=true",
		`{"wireguard":{"listen_port":51821}}`); w.Code != http.StatusOK {
		t.Fatalf("second PATCH = %d: %s", w.Code, w.Body)
	}
	after, _ := applier.Load()
	if after.WireGuard.PrivateKey != cfg.WireGuard.PrivateKey {
		t.Error("the box's key was replaced by an unrelated edit")
	}
}

func TestAddingAPeerReturnsAConfigurationOnce(t *testing.T) {
	h, applier := testHTTP(t)
	turnOn(t, h)

	w := do(t, h, http.MethodPut, "/peers/phone?confirm=true", `{"routes":"home"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}

	resp := decode[applyResponse](t, w)
	if resp.Peer == nil || resp.Peer.ClientConfig == "" {
		t.Fatal("no client configuration came back, so the device cannot be set up at all")
	}
	if !strings.Contains(resp.Peer.ClientConfig, "PrivateKey = ") {
		t.Errorf("the configuration has no key in it:\n%s", resp.Peer.ClientConfig)
	}
	if !strings.Contains(resp.Peer.Note, "only time") {
		t.Errorf("nothing warns that this cannot be shown again: %q", resp.Peer.Note)
	}

	// The half that makes the promise real: what is stored is the public key
	// and nothing else, so there is no route that could produce the file again.
	cfg, _ := applier.Load()
	peer, ok := cfg.WireGuard.Peer("phone")
	if !ok {
		t.Fatal("the peer was not stored")
	}
	if strings.Contains(string(mustMarshal(t, cfg)), privateKeyOf(t, resp.Peer.ClientConfig)) {
		t.Fatal("the device's private key was stored; it must exist only in that one response")
	}
	if err := ValidKey(peer.PublicKey); err != nil {
		t.Errorf("stored public key is not one: %v", err)
	}

	// A local name is `dns`'s to store, so the one thing this module owes is
	// the command — with the address already in it, at the moment the operator
	// is looking at that address (docs/remote-access.md §3).
	if len(resp.Peer.NextSteps) == 0 ||
		!strings.Contains(resp.Peer.NextSteps[0], "olr dns add host phone --address 10.6.0.2") {
		t.Errorf("no pointer at how to name the device: %v", resp.Peer.NextSteps)
	}

	// Editing the peer afterwards is not a re-issue — there is no private key
	// left to write one with.
	w = do(t, h, http.MethodPut, "/peers/phone?confirm=true", `{"routes":"everything"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("edit = %d: %s", w.Code, w.Body)
	}
	edit := decode[applyResponse](t, w)
	if edit.Peer == nil {
		t.Fatal("changing what a device sends said nothing, so the operator would believe olr had done it")
	}
	if edit.Peer.ClientConfig != "" {
		t.Error("editing a device produced a second client configuration")
	}
	// What the device sends is decided by the file on the device, so the only
	// useful thing this response can carry is the line to change there.
	if !strings.Contains(edit.Peer.Note, "AllowedIPs = 0.0.0.0/0") {
		t.Errorf("the note does not give the line to edit: %q", edit.Peer.Note)
	}
	// And nothing in the kernel moved, because nothing in the kernel knows.
	if !edit.Plan.Empty {
		t.Errorf("a client-side change produced kernel work: %+v", edit.Plan.Changes)
	}
}

// The operator generated the pair themselves, so olr has no private key and
// must not pretend otherwise.
func TestASuppliedPublicKeyGetsNoPrivateOne(t *testing.T) {
	h, _ := testHTTP(t)
	turnOn(t, h)

	key := mustKey().Public
	w := do(t, h, http.MethodPut, "/peers/work?confirm=true", `{"public_key":"`+key+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}

	resp := decode[applyResponse](t, w)
	if resp.Peer == nil {
		t.Fatal("nothing came back about the device that was just added")
	}
	if strings.Contains(resp.Peer.ClientConfig, "PrivateKey = "+key) {
		t.Error("olr invented a private key")
	}
	if !strings.Contains(resp.Peer.Note, "supplied") {
		t.Errorf("the note does not explain why there is no key: %q", resp.Peer.Note)
	}
}

func TestRemovingAPeerIsRefusedWithoutConfirm(t *testing.T) {
	h, applier := testHTTP(t)
	turnOn(t, h)
	if w := do(t, h, http.MethodPut, "/peers/phone?confirm=true", ""); w.Code != http.StatusOK {
		t.Fatalf("setup PUT = %d: %s", w.Code, w.Body)
	}

	w := do(t, h, http.MethodDelete, "/peers/phone", "")
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body)
	}

	resp := decode[applyResponse](t, w)
	if resp.Plan.Impact != ImpactDisruptive {
		t.Errorf("impact = %s, want disruptive", resp.Plan.Impact)
	}
	// The refusal has to name what would be lost, or the dialog built on it
	// cannot say anything useful.
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "phone") {
		t.Errorf("the refusal must name the device, got %+v", resp.Error)
	}
	if !strings.Contains(resp.Error.Message, "confirm=true") {
		t.Errorf("the refusal must say how to go ahead, got %q", resp.Error.Message)
	}

	// Nothing was written. This is the half that matters: a 409 that had
	// already revoked the device would be worse than no gate at all.
	cfg, _ := applier.Load()
	if _, ok := cfg.WireGuard.Peer("phone"); !ok {
		t.Fatal("the device was removed by a request that was refused")
	}

	// And the refusal says so, rather than leaving a client to ask again
	// against a box whose lock it no longer holds. Redacted like every other
	// config on the way out.
	if resp.Config == nil {
		t.Fatal("the refusal does not report what is still stored")
	}
	if _, ok := resp.Config.WireGuard.Peer("phone"); !ok {
		t.Error("the refusal reports a document the device has already left")
	}
	if resp.Config.WireGuard.PrivateKey != RedactedKey {
		t.Errorf("the refusal returned the key as %q", resp.Config.WireGuard.PrivateKey)
	}

	if w := do(t, h, http.MethodDelete, "/peers/phone?confirm=true", ""); w.Code != http.StatusOK {
		t.Fatalf("confirmed delete = %d: %s", w.Code, w.Body)
	}
	after, _ := applier.Load()
	if _, ok := after.WireGuard.Peer("phone"); ok {
		t.Fatal("a confirmed delete left the device in place")
	}
}

func TestDeletingAnUnknownPeerIsNotFound(t *testing.T) {
	h, _ := testHTTP(t)
	turnOn(t, h)

	if w := do(t, h, http.MethodDelete, "/peers/nobody?confirm=true", ""); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body)
	}
}

func TestTheKeyIsNeverReturnedAndTheMaskMeansUnchanged(t *testing.T) {
	h, applier := testHTTP(t)
	turnOn(t, h)

	stored, _ := applier.Load()

	w := do(t, h, http.MethodGet, "/config", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), stored.WireGuard.PrivateKey) {
		t.Fatal("GET /config returned the private key")
	}

	// The round trip redaction creates: a UI that renders the mask into a form
	// and sends the form back must not store eight asterisks as the key.
	body := w.Body.String()
	if w := do(t, h, http.MethodPut, "/config?confirm=true", body); w.Code != http.StatusOK {
		t.Fatalf("PUT of what GET returned = %d: %s", w.Code, w.Body)
	}
	after, _ := applier.Load()
	if after.WireGuard.PrivateKey != stored.WireGuard.PrivateKey {
		t.Fatalf("the key became %q after a round trip", after.WireGuard.PrivateKey)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	h, applier := testHTTP(t)
	turnOn(t, h)

	w := do(t, h, http.MethodPut, "/peers/phone?dry_run=true", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	// A dry run answers with the plan alone, the same shape POST /plan gives.
	plan := decode[planView](t, w)
	if plan.Empty {
		t.Error("adding a device was planned as no change")
	}

	cfg, _ := applier.Load()
	if _, ok := cfg.WireGuard.Peer("phone"); ok {
		t.Fatal("a dry run stored the device")
	}
}

func TestStatusReportsTheTunnelAndItsBlockers(t *testing.T) {
	h, _ := testHTTP(t)
	turnOn(t, h)

	w := do(t, h, http.MethodGet, "/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	resp := decode[statusResponse](t, w)

	if !resp.Enabled || resp.Interface != DefaultInterface {
		t.Errorf("status does not describe the tunnel: %+v", resp)
	}
	// The public key is not a secret — it is in every client configuration —
	// and it is the one value an operator completing a file by hand needs.
	if resp.PublicKey == "" {
		t.Error("status does not report this box's public key")
	}
	if resp.AsOf.IsZero() {
		t.Error("an observed response with no as_of (design.md §4.5)")
	}
}

func mustMarshal(t *testing.T, c Config) []byte {
	t.Helper()
	data, err := MarshalConfig(c)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// privateKeyOf pulls the key out of a rendered client configuration, so the
// test can assert it appears nowhere else.
func privateKeyOf(t *testing.T, conf string) string {
	t.Helper()
	for _, line := range strings.Split(conf, "\n") {
		if key, ok := strings.CutPrefix(strings.TrimSpace(line), "PrivateKey = "); ok {
			return strings.TrimSpace(key)
		}
	}
	t.Fatalf("no PrivateKey line in:\n%s", conf)
	return ""
}
