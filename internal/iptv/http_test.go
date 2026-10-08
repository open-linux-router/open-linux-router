package iptv

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func TestHTTPPlanDoesNotApplyAndConfigIsStrict(t *testing.T) {
	root := t.TempDir()
	unit := &testUnit{}
	h := HTTP{Applier: Applier{Store: core.NewStore(filepath.Join(root, "olr.json"), ModuleName), Links: testLinks{}, Unit: unit, Root: root}, Lock: core.NewLock()}
	handler := core.RouteTable(h.Routes())
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewBufferString(body)))
		return w
	}
	if w := request("POST", "/plan", `{"enabled":true,"upstream":"iptv0","networks":["lan"]}`); w.Code != http.StatusOK {
		t.Fatalf("plan: %d %s", w.Code, w.Body)
	}
	if unit.active || unit.enabled {
		t.Fatal("plan changed the service")
	}
	if _, e := h.Applier.Load(); e != nil {
		t.Fatal(e)
	}
	if w := request("PUT", "/config", `{"enabled":true,"upsteam":"iptv0"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("typo: %d %s", w.Code, w.Body)
	}
	if w := request("PUT", "/config", `{"enabled":true,"upstream":"iptv0","networks":["lan"]}`); w.Code != http.StatusOK {
		t.Fatalf("apply: %d %s", w.Code, w.Body)
	}
	if !unit.active || !unit.enabled {
		t.Fatal("service did not start")
	}
	w := request("GET", "/status", "")
	var status struct {
		Enabled bool
		Plan    Plan
	}
	if e := json.Unmarshal(w.Body.Bytes(), &status); e != nil {
		t.Fatal(e)
	}
	if w.Code != http.StatusOK || !status.Enabled || !status.Plan.Empty {
		t.Fatalf("status: %d %s", w.Code, w.Body)
	}
}
