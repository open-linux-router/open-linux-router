package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfigPreconditions(t *testing.T) {
	before := map[string]string{"token": "old"}
	after := map[string]string{"token": "new"}
	get := httptest.NewRecorder()
	WriteConfig(get, before, map[string]string{"token": "***"})
	if strings.Contains(get.Body.String(), "old") || get.Header().Get("ETag") == "" {
		t.Fatal("config must expose a revision without exposing the secret")
	}
	for _, tc := range []struct {
		match   string
		config  any
		allowed bool
	}{
		{"", after, true},
		{get.Header().Get("ETag"), before, true},
		{get.Header().Get("ETag"), after, false},
		{"W/" + get.Header().Get("ETag"), before, false},
		{`"other", ` + get.Header().Get("ETag"), before, true},
	} {
		r := httptest.NewRequest(http.MethodPut, "/config", nil)
		r.Header.Set("If-Match", tc.match)
		w := httptest.NewRecorder()
		if got := MatchConfig(w, r, tc.config); got != tc.allowed {
			t.Fatalf("If-Match %s: allowed=%v, want %v", tc.match, got, tc.allowed)
		}
		if !tc.allowed && w.Code != http.StatusPreconditionFailed {
			t.Fatal(w.Code)
		}
	}
}
