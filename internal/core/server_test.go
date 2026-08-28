package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{"path": r.URL.Path, "body": body})
	})
}

// A module matches its own routes without repeating its name, so core has to
// strip the prefix before handing the request over.
func TestModuleRoutesAreMountedWithThePrefixStripped(t *testing.T) {
	s := New()
	s.Mount("dhcp", testHandler("dhcp"), struct{}{})

	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/dhcp/config", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body)
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["path"] != "/config" {
		t.Errorf("module saw path %q, want /config", got["path"])
	}
}

// A client should never have to parse two error formats.
func TestUnknownAPIPathUsesTheJSONErrorShape(t *testing.T) {
	s := New()
	s.Mount("dhcp", testHandler("dhcp"), struct{}{})

	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/nope", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d", w.Code)
	}
	var body struct {
		Error ErrorBody `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("404 body was not our error shape: %v (%s)", err, w.Body)
	}
	if body.Error.Message == "" {
		t.Error("404 carried no message")
	}
}

// Core serves /api and nothing else; the SPA is composed alongside it by
// cmd/olrd, so core must not answer for it.
func TestCoreDoesNotServeOutsideTheAPIPrefix(t *testing.T) {
	s := New()
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/dhcp", nil))

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestSchemaEndpointPublishesBothProjections(t *testing.T) {
	s := New()
	s.Mount("test", testHandler("test"), testConfig{})

	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/schema/test", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body)
	}
	var got Projections
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Full == nil || got.Relaxed == nil {
		t.Fatal("a projection was missing")
	}
	if len(got.Full.Required) == 0 {
		t.Error("full projection has no required fields")
	}
	if len(got.Relaxed.Required) != 0 {
		t.Error("relaxed projection still has required fields")
	}
}

func TestUnknownModuleSchemaIs404(t *testing.T) {
	s := New()
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/schema/nope", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestModulesEndpointListsMountedModules(t *testing.T) {
	s := New()
	s.Mount("dns", testHandler("dns"), struct{}{})
	s.Mount("dhcp", testHandler("dhcp"), struct{}{})

	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/modules", nil))

	var got struct {
		Modules []string `json:"modules"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	// Sorted, so the list does not depend on mount order.
	if len(got.Modules) != 2 || got.Modules[0] != "dhcp" || got.Modules[1] != "dns" {
		t.Errorf("modules = %v, want [dhcp dns]", got.Modules)
	}
}

// Mounting mistakes are startup-time programming errors, caught the same way
// cli.Verb catches a verb outside the shared vocabulary: loudly, immediately.
func TestMountPanicsOnProgrammingErrors(t *testing.T) {
	tests := []struct {
		name  string
		mount func(*Server)
	}{
		{"empty name", func(s *Server) { s.Mount("", testHandler("x"), struct{}{}) }},
		{"path separator", func(s *Server) { s.Mount("a/b", testHandler("x"), struct{}{}) }},
		{"uppercase", func(s *Server) { s.Mount("Dhcp", testHandler("x"), struct{}{}) }},
		{"nil handler", func(s *Server) { s.Mount("dhcp", nil, struct{}{}) }},
		{"duplicate", func(s *Server) {
			s.Mount("dhcp", testHandler("x"), struct{}{})
			s.Mount("dhcp", testHandler("y"), struct{}{})
		}},
		{"after the handler is built", func(s *Server) {
			s.Handler()
			s.Mount("dhcp", testHandler("x"), struct{}{})
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("want a panic")
				}
			}()
			tt.mount(New())
		})
	}
}

func TestHandlerIsStable(t *testing.T) {
	s := New()
	s.Mount("dhcp", testHandler("dhcp"), struct{}{})
	if s.Handler() != s.Handler() {
		t.Error("Handler returned a different handler on the second call")
	}
}
