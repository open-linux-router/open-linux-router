package daemon

import (
	"flag"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The failure this file exists for, exactly as an operator met it: point a
// browser at the listener and get
//
//	{"error":{"message":"missing or invalid API token; see /etc/open-linux-router/api-token"}}
//
// as raw JSON instead of the app. BearerAuth wrapped the whole mux, so `/` —
// the SPA — was behind the token. The SPA is what asks for the token, so the
// documented flow could never happen: the page that would collect it could not
// load without it.

// stub stands in for the top-level mux: an API route and the SPA at the root.
func stub() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(core.APIPrefix+"/", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("api"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("<!doctype html>spa"))
	})
	return mux
}

func TestTheSPALoadsWithoutATokenSoItCanAskForOne(t *testing.T) {
	handler := authenticateAPI("sekret", stub())

	for _, path := range []string{"/", "/index.html", "/assets/app.js", "/devices"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200; the page that asks for the token cannot be behind it",
				path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "API token") {
			t.Errorf("GET %s answered with the token error instead of the app:\n%s",
				path, rec.Body.String())
		}
	}
}

// The other half: turning auth on has to actually protect the API, or the flag
// is decoration.
func TestTheAPIStillNeedsTheTokenWhenAuthIsOn(t *testing.T) {
	handler := authenticateAPI("sekret", stub())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, core.APIPrefix+"/modules", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated API request = %d, want 401", rec.Code)
	}

	// The bare prefix, with no trailing slash, must not slip past the check.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, core.APIPrefix, nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET %s = %d, want 401", core.APIPrefix, rec.Code)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, core.APIPrefix+"/modules", nil)
	req.Header.Set("Authorization", "Bearer sekret")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("authenticated API request = %d, want 200", rec.Code)
	}
}

// A path that merely begins with the same letters is not the API, and must not
// inherit its protection or its exemption.
func TestAPathThatLooksLikeTheAPIPrefixIsNotTheAPI(t *testing.T) {
	handler := authenticateAPI("sekret", stub())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, core.APIPrefix+"docs", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET %sdocs = %d, want 200 — it is a UI route, not the API",
			core.APIPrefix, rec.Code)
	}
}

// Authentication is off unless asked for, including on an address the whole
// network can reach. That is a product decision rather than an oversight, so it
// is written down as a test: a change that quietly restores the old default
// should have to delete this.
func TestTheListenerIsUnauthenticatedUnlessAuthIsRequested(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Port 0 so the kernel picks a free one; this asserts on the reported mode,
	// not on reaching the socket.
	for _, listen := range []string{"127.0.0.1:0", "0.0.0.0:0"} {
		srv, authed, err := tcpListener(
			options{listen: listen}, newApplier(t, claimed), stub(), logger)
		if err != nil {
			t.Fatalf("tcpListener(%q): %v", listen, err)
		}
		srv.listener.Close()
		if authed != "none" {
			t.Errorf("tcpListener(%q) reported %q, want %q", listen, authed, "none")
		}
	}
}

// The old default is gone but its flag is not, and it must remain harmless.
// --no-auth named the inverse of --auth; a box that still carries it in
// OLRD_ARGS has to keep starting, because failing on an unknown flag would turn
// an upgrade into an outage on the one file operators edit by hand.
func TestTheOldNoAuthFlagIsStillAccepted(t *testing.T) {
	opts, err := parseOptions([]string{"--listen", "127.0.0.1:8080", "--no-auth"}, flag.ContinueOnError)
	if err != nil {
		t.Fatalf("--no-auth is no longer accepted, so an upgrade would stop olrd: %v", err)
	}
	if opts.auth {
		t.Error("--no-auth turned authentication on")
	}
	if opts.listen != "127.0.0.1:8080" {
		t.Errorf("listen = %q, want 127.0.0.1:8080", opts.listen)
	}
}

func TestAuthIsOffUnlessAsked(t *testing.T) {
	opts, err := parseOptions(nil, flag.ContinueOnError)
	if err != nil {
		t.Fatal(err)
	}
	if opts.auth {
		t.Error("authentication defaults to on")
	}

	opts, err = parseOptions([]string{"--auth"}, flag.ContinueOnError)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.auth {
		t.Error("--auth did not turn authentication on")
	}
}
