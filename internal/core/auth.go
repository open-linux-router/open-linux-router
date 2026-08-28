package core

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Authentication for the TCP listener.
//
// design.md §10 folds "who may log into olr" into the unbuilt `system` module,
// so there is no user model yet and nothing to check a username against. What
// there *is* is a hard requirement not to put an unauthenticated admin API on a
// router's network. A single bearer token is the honest floor: it is real
// authentication, it is one file, and it does not pretend to be a session,
// a role, or an identity that a later `auth` module would then have to
// contradict.
//
// The unix socket is deliberately not covered here. Its access control is the
// socket's mode and group (§listen.go), which is both stronger and simpler than
// a shared secret for a local caller.

// TokenPath is where the API token lives.
const TokenPath = "/etc/open-linux-router/api-token"

// tokenBytes is the entropy behind the token. 32 bytes is far past what an
// online guessing attack could reach and costs nothing.
const tokenBytes = 32

// LoadOrCreateToken reads the API token, generating one on first start.
//
// The file is created 0600 and, being under /etc, is part of the config the
// operator can read to find out what the token is.
func LoadOrCreateToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		token := strings.TrimSpace(string(data))
		if token == "" {
			return "", fmt.Errorf("%s is empty; delete it to have a new token generated", path)
		}
		return token, nil

	case os.IsNotExist(err):
		// fall through and generate

	default:
		return "", fmt.Errorf("reading %s: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}

	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)

	// O_EXCL so that two daemons racing on first start cannot each believe they
	// wrote the token that the other is now serving.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return LoadOrCreateToken(path)
		}
		return "", fmt.Errorf("creating %s: %w", path, err)
	}
	defer f.Close()

	if _, err := f.WriteString(token + "\n"); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return token, nil
}

// BearerAuth requires `Authorization: Bearer <token>` on every request.
//
// Note for whoever wires the WebUI's live updates: the browser's EventSource
// API cannot set an Authorization header, so consuming /api/events from the SPA
// will need either a cookie session or a fetch-based reader. That is why the
// UI polls today rather than streaming — the choice is deferred, not missed.
func BearerAuth(token string, next http.Handler) http.Handler {
	want := []byte(token)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := bearerToken(r)
		// ConstantTimeCompare returns 0 for differing lengths, so the length
		// check is not a shortcut around it — subtle requires equal lengths to
		// give a meaningful answer at all.
		if !ok || len(got) != len(want) || subtle.ConstantTimeCompare(got, want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="olr"`)
			WriteError(w, http.StatusUnauthorized,
				"missing or invalid API token; see "+TokenPath)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerToken(r *http.Request) ([]byte, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return nil, false
	}
	return []byte(strings.TrimSpace(h[len(prefix):])), true
}
