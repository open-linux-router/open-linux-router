package link

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// testHTTP wires the module's surface over a temp store and an injected
// interface list, so nothing here needs root, netlink or /etc.
func testHTTP(t *testing.T, document string) (http.Handler, Applier) {
	t.Helper()
	applier := Applier{
		Store:  storeWith(t, document),
		Source: staticSource(testInterfaces(t)...),
	}
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

func TestConfigRoundTripsThroughHTTP(t *testing.T) {
	h, _ := testHTTP(t, "")

	if w := do(t, h, http.MethodPut, "/config", `{"adopted":["lan0"]}`); w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", w.Code, w.Body)
	}

	w := do(t, h, http.MethodGet, "/config", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d", w.Code)
	}
	var got Config
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.IsAdopted("lan0") {
		t.Errorf("stored config did not survive the round trip: %+v", got)
	}
}

// A PATCH replaces the whole list rather than merging into it (RFC 7386), which
// is what makes a release expressible at all.
func TestPatchReplacesTheList(t *testing.T) {
	h, _ := testHTTP(t, `{"link":{"adopted":["lan0","lan1"]}}`)

	if w := do(t, h, http.MethodPatch, "/config", `{"adopted":["lan1"]}`); w.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d, body %s", w.Code, w.Body)
	}

	var got Config
	if err := json.Unmarshal(do(t, h, http.MethodGet, "/config", "").Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.IsAdopted("lan0") || !got.IsAdopted("lan1") {
		t.Errorf("Adopted = %v, want only lan1", got.Adopted)
	}
}

func TestPutRefusesTheLoopbackInterface(t *testing.T) {
	h, _ := testHTTP(t, "")

	w := do(t, h, http.MethodPut, "/config", `{"adopted":["lo"]}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("PUT status = %d, want 422; body %s", w.Code, w.Body)
	}
}

// Writing another module's section must not be possible from here, and reading
// one must not disturb it: the store is shared, so a save is a read-modify-write
// of the whole document.
func TestApplyKeepsOtherModulesSections(t *testing.T) {
	h, applier := testHTTP(t, `{"dhcp":{"enabled":true},"link":{"adopted":["lan1"]}}`)

	if w := do(t, h, http.MethodPut, "/config", `{"adopted":["lan0"]}`); w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", w.Code, w.Body)
	}

	doc, err := applier.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := doc.Raw("dhcp")
	if !ok {
		t.Fatal("the dhcp section was dropped by a link write")
	}
	// Parsed rather than compared as bytes: the store re-indents the whole
	// document on every save, so the section's formatting is legitimately not
	// what was written even though its content is.
	var section struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(raw, &section); err != nil {
		t.Fatal(err)
	}
	if !section.Enabled {
		t.Errorf("dhcp section = %s, want enabled still true", raw)
	}
}

func TestPlanDoesNotWrite(t *testing.T) {
	h, applier := testHTTP(t, "")

	w := do(t, h, http.MethodPost, "/plan", `{"adopted":["lan0"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /plan status = %d, body %s", w.Code, w.Body)
	}
	var plan planView
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Empty || len(plan.Changes) != 1 {
		t.Errorf("plan = %+v, want one change", plan)
	}

	cfg, err := applier.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Empty() {
		t.Errorf("a plan wrote the document: %v", cfg.Adopted)
	}
}

func TestInterfacesListsWhatTheMachineHas(t *testing.T) {
	h, _ := testHTTP(t, `{"link":{"adopted":["lan0"]}}`)

	w := do(t, h, http.MethodGet, "/interfaces", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /interfaces status = %d, body %s", w.Code, w.Body)
	}

	var resp listResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Interfaces) != 3 {
		t.Fatalf("got %d interfaces, want 3: %+v", len(resp.Interfaces), resp.Interfaces)
	}
	if resp.AsOf.IsZero() {
		t.Error("the reply is not stamped; every observed object carries its freshness (§4.5)")
	}

	var lan0 interfaceView
	for _, iface := range resp.Interfaces {
		if iface.Name == "lan0" {
			lan0 = iface
		}
	}
	switch {
	case !lan0.Adopted:
		t.Error("lan0 is adopted in the document but not in the list")
	case !lan0.Present || !lan0.Up:
		t.Errorf("lan0 = %+v, want present and up", lan0)
	case lan0.SuggestedStart == "":
		t.Error("lan0 has an address but no suggested range")
	}
}
