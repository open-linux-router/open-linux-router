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

func testHTTP(t *testing.T) (http.Handler, TunnelApplier) {
	t.Helper()
	applier, _ := testApplier(t)
	h := HTTP{Tunnel: applier, Lock: core.NewLock(), Events: core.NewEvents()}
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
//
// Two requests, and the split is the module's shape rather than an awkwardness:
// where devices dial belongs to the box and is set at the module level, while
// turning the tunnel on belongs to the tunnel. A single route that did both
// would be the composite the two objects exist not to be.
func turnOn(t *testing.T, h http.Handler) {
	t.Helper()
	if w := do(t, h, http.MethodPatch, "/config?confirm=true",
		`{"endpoint":"home.example.net"}`); w.Code != http.StatusOK {
		t.Fatalf("setup endpoint PATCH = %d: %s", w.Code, w.Body)
	}
	if w := do(t, h, http.MethodPatch, "/wireguard/config?confirm=true",
		`{"enabled":true}`); w.Code != http.StatusOK {
		t.Fatalf("setup tunnel PATCH = %d: %s", w.Code, w.Body)
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
	if w := do(t, h, http.MethodPatch, "/wireguard/config?confirm=true",
		`{"listen_port":51821}`); w.Code != http.StatusOK {
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

	w := do(t, h, http.MethodPut, "/wireguard/peers/phone?confirm=true", `{"routes":"home"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}

	resp := decode[tunnelApplyResponse](t, w)
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
	w = do(t, h, http.MethodPut, "/wireguard/peers/phone?confirm=true", `{"routes":"everything"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("edit = %d: %s", w.Code, w.Body)
	}
	edit := decode[tunnelApplyResponse](t, w)
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
	w := do(t, h, http.MethodPut, "/wireguard/peers/work?confirm=true", `{"public_key":"`+key+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}

	resp := decode[tunnelApplyResponse](t, w)
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
	if w := do(t, h, http.MethodPut, "/wireguard/peers/phone?confirm=true", ""); w.Code != http.StatusOK {
		t.Fatalf("setup PUT = %d: %s", w.Code, w.Body)
	}

	w := do(t, h, http.MethodDelete, "/wireguard/peers/phone", "")
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body)
	}

	resp := decode[tunnelApplyResponse](t, w)
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
	if resp.Config.WireGuard.PrivateKey != RedactedSecret {
		t.Errorf("the refusal returned the key as %q", resp.Config.WireGuard.PrivateKey)
	}

	if w := do(t, h, http.MethodDelete, "/wireguard/peers/phone?confirm=true", ""); w.Code != http.StatusOK {
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

	if w := do(t, h, http.MethodDelete, "/wireguard/peers/nobody?confirm=true", ""); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body)
	}
}

// A rename is the one thing an operator can change about a device that the
// device never hears about, so the test is about everything staying put: the
// address, the key, what the device sends home, and the kernel.
func TestRenamingADeviceLeavesEverythingElseAlone(t *testing.T) {
	h, applier := testHTTP(t)
	turnOn(t, h)
	if w := do(t, h, http.MethodPut, "/wireguard/peers/phone?confirm=true",
		`{"routes":"everything"}`); w.Code != http.StatusOK {
		t.Fatalf("setup PUT = %d: %s", w.Code, w.Body)
	}
	before, _ := applier.Load()
	was, _ := before.WireGuard.Peer("phone")

	w := do(t, h, http.MethodPatch, "/wireguard/peers/phone?confirm=true", `{"name":"handset"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	resp := decode[tunnelApplyResponse](t, w)
	if resp.Peer == nil || resp.Peer.Name != "handset" {
		t.Fatalf("the response does not say what the device is called now: %+v", resp.Peer)
	}
	// Nothing in the kernel knows a name, so nothing in the kernel moved — and
	// that is the whole reason this edit is safe on a device olr cannot reach.
	if !resp.Plan.Empty {
		t.Errorf("a rename produced kernel work: %+v", resp.Plan.Changes)
	}
	if !strings.Contains(resp.Peer.Note, "keeps working") {
		t.Errorf("nothing says the device's file is still good: %q", resp.Peer.Note)
	}
	if resp.Peer.ClientConfig != "" {
		t.Error("a rename re-issued a client configuration, which would mean the key had changed")
	}

	after, _ := applier.Load()
	got, ok := after.WireGuard.Peer("handset")
	if !ok {
		t.Fatal("the device is not under its new name")
	}
	if _, still := after.WireGuard.Peer("phone"); still {
		t.Error("the old name is still held by a device")
	}
	if got.Address != was.Address || got.PublicKey != was.PublicKey {
		t.Errorf("the rename moved the device: %+v, was %+v", got, was)
	}
	// The trap a rename sent as a PUT would fall into: a body naming one field
	// and resetting the others.
	if got.Routes != RouteEverything {
		t.Errorf("routes = %q, want what the device sends to have survived", got.Routes)
	}
}

func TestRenamingRefusesWhatItCannotDo(t *testing.T) {
	h, applier := testHTTP(t)
	turnOn(t, h)
	for _, name := range []string{"phone", "laptop"} {
		if w := do(t, h, http.MethodPut, "/wireguard/peers/"+name+"?confirm=true", ""); w.Code != http.StatusOK {
			t.Fatalf("setup PUT %s = %d: %s", name, w.Code, w.Body)
		}
	}

	// A name another device holds is a conflict, not a malformed request: the
	// caller can pick again without changing anything about how it asked.
	if w := do(t, h, http.MethodPatch, "/wireguard/peers/phone?confirm=true", `{"name":"laptop"}`); w.Code != http.StatusConflict {
		t.Errorf("rename onto a taken name = %d, want 409: %s", w.Code, w.Body)
	}
	if w := do(t, h, http.MethodPatch, "/wireguard/peers/nobody?confirm=true", `{"name":"handset"}`); w.Code != http.StatusNotFound {
		t.Errorf("rename of an unknown device = %d, want 404: %s", w.Code, w.Body)
	}
	// A patch carrying nothing to change, and one carrying a field this route
	// does not have. Both are refused rather than silently ignored: a caller
	// that meant to change routes and reached this route must be told.
	if w := do(t, h, http.MethodPatch, "/wireguard/peers/phone?confirm=true", `{}`); w.Code != http.StatusBadRequest {
		t.Errorf("empty patch = %d, want 400: %s", w.Code, w.Body)
	}
	if w := do(t, h, http.MethodPatch, "/wireguard/peers/phone?confirm=true", `{"routes":"home"}`); w.Code != http.StatusBadRequest {
		t.Errorf("patch of another field = %d, want 400: %s", w.Code, w.Body)
	}
	// A name the validator refuses arrives as a field path, the same as it
	// would through a PUT — one rule, one place.
	if w := do(t, h, http.MethodPatch, "/wireguard/peers/phone?confirm=true", `{"name":"my phone"}`); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid name = %d, want 422: %s", w.Code, w.Body)
	}

	after, _ := applier.Load()
	if _, ok := after.WireGuard.Peer("phone"); !ok {
		t.Fatal("a refused rename lost the device")
	}
	if _, ok := after.WireGuard.Peer("laptop"); !ok {
		t.Fatal("a refused rename lost the device it collided with")
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
	// Scoped to the tunnel's own section, which is what its route accepts.
	section, err := json.Marshal(decode[Config](t, w).WireGuard)
	if err != nil {
		t.Fatal(err)
	}
	if w := do(t, h, http.MethodPut, "/wireguard/config?confirm=true", string(section)); w.Code != http.StatusOK {
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

	w := do(t, h, http.MethodPut, "/wireguard/peers/phone?dry_run=true", "")
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

	w := do(t, h, http.MethodGet, "/wireguard/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	resp := decode[tunnelStatus](t, w)

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

// A picture is a label, exactly like a name: nothing in the kernel knows it, so
// choosing one cannot move a device or re-issue a configuration. The two
// vocabularies belong to `devices`, which is why the refusal arrives as a field
// path rather than as a bare 422.
func TestAPeerIsDrawnWithoutTouchingTheTunnel(t *testing.T) {
	h, applier := testHTTP(t)
	turnOn(t, h)
	if w := do(t, h, http.MethodPut, "/wireguard/peers/phone?confirm=true", ""); w.Code != http.StatusOK {
		t.Fatalf("setup PUT = %d: %s", w.Code, w.Body)
	}
	before, _ := applier.Load()
	was, _ := before.WireGuard.Peer("phone")

	// Stored as sent, but normalized on the way in — a form that sends the
	// vocabulary in another case must not store a picture nothing can find.
	w := do(t, h, http.MethodPatch, "/wireguard/peers/phone?confirm=true",
		`{"category":" Server ","icon":" OS/Debian "}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	resp := decode[tunnelApplyResponse](t, w)
	if !resp.Plan.Empty {
		t.Errorf("a picture produced kernel work: %+v", resp.Plan.Changes)
	}
	if resp.Peer == nil || resp.Peer.Name != "phone" {
		t.Fatalf("the response does not say which device it drew: %+v", resp.Peer)
	}

	after, _ := applier.Load()
	got, _ := after.WireGuard.Peer("phone")
	if got.Category != "server" || got.Icon != "os/debian" {
		t.Errorf("picture = %q %q, want it lower-cased and trimmed", got.Category, got.Icon)
	}
	if got.Address != was.Address || got.PublicKey != was.PublicKey || got.Routes != was.Routes {
		t.Errorf("choosing a picture moved the device: %+v, was %+v", got, was)
	}

	// The trap a routes-only PUT would fall into: it says what the device's
	// settings are, and the dialog that sends it has never heard of a picture.
	if w := do(t, h, http.MethodPut, "/wireguard/peers/phone?confirm=true", `{"routes":"everything"}`); w.Code != http.StatusOK {
		t.Fatalf("routes PUT = %d: %s", w.Code, w.Body)
	}
	after, _ = applier.Load()
	if got, _ := after.WireGuard.Peer("phone"); got.Icon != "os/debian" {
		t.Error("a routes-only PUT cleared a picture somebody chose")
	}

	// A rename carries it, because it is the same label on the same entry.
	if w := do(t, h, http.MethodPatch, "/wireguard/peers/phone?confirm=true", `{"name":"handset"}`); w.Code != http.StatusOK {
		t.Fatalf("rename = %d: %s", w.Code, w.Body)
	}
	after, _ = applier.Load()
	handset, ok := after.WireGuard.Peer("handset")
	if !ok {
		t.Fatal("the device is not under its new name")
	}
	if handset.Icon != "os/debian" || handset.Category != "server" {
		t.Errorf("the rename dropped the picture: %+v", handset)
	}

	// The read path reports it, or the page would draw a device nobody
	// described every time it reloaded.
	if w := do(t, h, http.MethodGet, "/wireguard/peers", ""); w.Code != http.StatusOK {
		t.Fatalf("peers = %d: %s", w.Code, w.Body)
	} else if list := decode[peersResponse](t, w); list.Peers[0].Icon != "os/debian" {
		t.Errorf("the list reported no picture: %+v", list.Peers[0])
	}

	// Empty is a request, not an omission: that is how a picture goes back to
	// the one its category picks, and it leaves the category alone.
	if w := do(t, h, http.MethodPatch, "/wireguard/peers/handset?confirm=true", `{"icon":""}`); w.Code != http.StatusOK {
		t.Fatalf("clearing = %d: %s", w.Code, w.Body)
	}
	after, _ = applier.Load()
	if got, _ := after.WireGuard.Peer("handset"); got.Icon != "" || got.Category != "server" {
		t.Errorf("clearing the icon left %+v, want the category kept and no picture", got)
	}

	for _, tc := range []struct {
		body, field string
	}{
		{`{"category":"nonsense"}`, "wireguard.peers[0].category"},
		{`{"icon":"../../etc/passwd"}`, "wireguard.peers[0].icon"},
		{`{"icon":"thesvg:Not A Slug"}`, "wireguard.peers[0].icon"},
	} {
		w := do(t, h, http.MethodPatch, "/wireguard/peers/handset?confirm=true", tc.body)
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s = %d, want 422: %s", tc.body, w.Code, w.Body)
			continue
		}
		var body struct {
			Error core.ErrorBody `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decoding %s: %v", w.Body, err)
		}
		if len(body.Error.Problems) != 1 || body.Error.Problems[0].Path != tc.field {
			t.Errorf("%s reported %+v, want one problem at %s", tc.body, body.Error.Problems, tc.field)
		}
	}

	// A device that does not exist is a 404 for every field, including the ones
	// that reach nothing.
	if w := do(t, h, http.MethodPatch, "/wireguard/peers/nobody?confirm=true", `{"icon":"os/debian"}`); w.Code != http.StatusNotFound {
		t.Errorf("picture for an unknown device = %d, want 404: %s", w.Code, w.Body)
	}
}

// A device is called what the operator calls it, which is not always what has
// to be typed. The name stays a DNS label because every command and the dns
// suggestion use it; the label is the operator's words and reaches nothing.
func TestADeviceCanBeCalledWhatItIsNotNamed(t *testing.T) {
	h, applier := testHTTP(t)
	turnOn(t, h)

	// Given at creation, which is the one moment the CLI has it in hand.
	if w := do(t, h, http.MethodPut, "/wireguard/peers/li-mbp?confirm=true",
		`{"display_name":"Li's Macbook Pro"}`); w.Code != http.StatusOK {
		t.Fatalf("setup PUT = %d: %s", w.Code, w.Body)
	}
	before, _ := applier.Load()
	was, _ := before.WireGuard.Peer("li-mbp")
	if was.DisplayName != "Li's Macbook Pro" {
		t.Fatalf("the label was not stored as written: %q", was.DisplayName)
	}

	// What the dialog sends to change what a device does: it has never heard of
	// a label and must not clear one.
	if w := do(t, h, http.MethodPut, "/wireguard/peers/li-mbp?confirm=true", `{"routes":"everything"}`); w.Code != http.StatusOK {
		t.Fatalf("routes PUT = %d: %s", w.Code, w.Body)
	}
	after, _ := applier.Load()
	got, _ := after.WireGuard.Peer("li-mbp")
	if got.DisplayName != "Li's Macbook Pro" {
		t.Errorf("a routes-only PUT cleared the label: %+v", got)
	}
	if got.Address != was.Address || got.PublicKey != was.PublicKey {
		t.Error("changing the route list re-issued the device")
	}

	// Changing only the label is a stored write and no kernel work, exactly like
	// a rename — the whole point of it being a second field.
	w := do(t, h, http.MethodPatch, "/wireguard/peers/li-mbp?confirm=true",
		`{"display_name":"  Li’s laptop  "}`)
	if w.Code != http.StatusOK {
		t.Fatalf("label PATCH = %d: %s", w.Code, w.Body)
	}
	if resp := decode[tunnelApplyResponse](t, w); !resp.Plan.Empty {
		t.Errorf("a label produced kernel work: %+v", resp.Plan.Changes)
	}
	after, _ = applier.Load()
	if got, _ := after.WireGuard.Peer("li-mbp"); got.DisplayName != "Li’s laptop" {
		t.Errorf("label = %q, want it trimmed and otherwise untouched", got.DisplayName)
	}

	// The label follows the name, because it is the same entry's label.
	if w := do(t, h, http.MethodPatch, "/wireguard/peers/li-mbp?confirm=true", `{"name":"handset"}`); w.Code != http.StatusOK {
		t.Fatalf("rename = %d: %s", w.Code, w.Body)
	}
	after, _ = applier.Load()
	if got, _ := after.WireGuard.Peer("handset"); got.DisplayName != "Li’s laptop" {
		t.Errorf("the rename dropped the label: %+v", got)
	}

	// The read path reports it, and the name stays the key.
	if w := do(t, h, http.MethodGet, "/wireguard/peers", ""); w.Code != http.StatusOK {
		t.Fatalf("peers = %d: %s", w.Code, w.Body)
	} else if list := decode[peersResponse](t, w); list.Peers[0].DisplayName != "Li’s laptop" || list.Peers[0].Name != "handset" {
		t.Errorf("the list reported %+v", list.Peers[0])
	}

	// Empty is how the name comes back, and it is not an error.
	if w := do(t, h, http.MethodPatch, "/wireguard/peers/handset?confirm=true", `{"display_name":""}`); w.Code != http.StatusOK {
		t.Fatalf("clearing = %d: %s", w.Code, w.Body)
	}
	after, _ = applier.Load()
	if got, _ := after.WireGuard.Peer("handset"); got.DisplayName != "" {
		t.Errorf("label = %q, want it cleared", got.DisplayName)
	}

	// Both labels in one request, which is what the dialog sends when somebody
	// renamed a device and gave it a readable name in the same edit.
	if w := do(t, h, http.MethodPatch, "/wireguard/peers/handset?confirm=true",
		`{"name":"work","display_name":"Work phone"}`); w.Code != http.StatusOK {
		t.Fatalf("combined patch = %d: %s", w.Code, w.Body)
	}
	after, _ = applier.Load()
	if got, ok := after.WireGuard.Peer("work"); !ok || got.DisplayName != "Work phone" {
		t.Errorf("the combined patch left %+v", got)
	}
	if _, still := after.WireGuard.Peer("handset"); still {
		t.Error("the old name survived the rename")
	}

	for _, tc := range []struct{ body, why string }{
		{`{"display_name":"` + strings.Repeat("x", MaxPeerDisplayNameLen+1) + `"}`, "longer than the limit"},
		{`{"display_name":"two\nlines"}`, "a control character"},
	} {
		w := do(t, h, http.MethodPatch, "/wireguard/peers/work?confirm=true", tc.body)
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("a label %s = %d, want 422: %s", tc.why, w.Code, w.Body)
			continue
		}
		var body struct {
			Error core.ErrorBody `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decoding %s: %v", w.Body, err)
		}
		if len(body.Error.Problems) == 0 || body.Error.Problems[0].Path != "wireguard.peers[0].display_name" {
			t.Errorf("a label %s reported %+v", tc.why, body.Error.Problems)
		}
	}
}
