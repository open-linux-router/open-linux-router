package core

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateTokenGeneratesOnceAndIsReadableOnlyByOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api-token")

	first, err := LoadOrCreateToken(path)
	if err != nil {
		t.Fatalf("LoadOrCreateToken: %v", err)
	}
	if len(first) < 32 {
		t.Errorf("token is only %d characters; too short to be worth having", len(first))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// A shared secret readable by every user on the box is not a secret.
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file mode = %o, want 600", perm)
	}

	second, err := LoadOrCreateToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Error("a second call generated a new token instead of reading the stored one")
	}
}

func TestLoadOrCreateTokenRejectsAnEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api-token")
	if err := os.WriteFile(path, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Silently generating a replacement would change the credential under an
	// operator who had merely truncated the file by accident.
	if _, err := LoadOrCreateToken(path); err == nil {
		t.Error("want an error for an empty token file")
	}
}

func TestBearerAuth(t *testing.T) {
	const token = "s3cret-token-value"

	tests := []struct {
		name   string
		header string
		want   int
	}{
		{"correct token", "Bearer " + token, http.StatusOK},
		{"scheme is case-insensitive", "bearer " + token, http.StatusOK},
		{"wrong token", "Bearer nope", http.StatusUnauthorized},
		{"no header", "", http.StatusUnauthorized},
		{"missing scheme", token, http.StatusUnauthorized},
		{"empty token", "Bearer ", http.StatusUnauthorized},
		// A prefix of the real token must not be accepted; the length check
		// exists because ConstantTimeCompare is meaningless on unequal lengths.
		{"prefix of the token", "Bearer s3cret", http.StatusUnauthorized},
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/modules", nil)
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}
			w := httptest.NewRecorder()
			BearerAuth(token, next).ServeHTTP(w, r)

			if w.Code != tt.want {
				t.Errorf("status = %d, want %d", w.Code, tt.want)
			}
			if tt.want == http.StatusUnauthorized && w.Header().Get("WWW-Authenticate") == "" {
				t.Error("a 401 should say how to authenticate")
			}
		})
	}
}

func TestIsLoopback(t *testing.T) {
	tests := map[string]bool{
		"127.0.0.1:8080": true,
		"localhost:8080": true,
		"[::1]:8080":     true,
		// The cases that matter: --no-auth must never be reachable off the box.
		"0.0.0.0:8080":      false,
		":8080":             false,
		"192.168.1.1:8080":  false,
		"[2001:db8::1]:443": false,
		"garbage":           false,
	}

	for addr, want := range tests {
		if got := IsLoopback(addr); got != want {
			t.Errorf("IsLoopback(%q) = %v, want %v", addr, got, want)
		}
	}
}
