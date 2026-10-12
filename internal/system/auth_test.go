package system

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func authFixture(t *testing.T) (Applier, *Sessions, *core.Lock, http.Handler) {
	t.Helper()
	a := Applier{Store: core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName)}
	sessions, lock := NewSessions(), core.NewLock()
	module := HTTP{Applier: a, Sessions: sessions, Lock: lock}.Handler()
	mux := http.NewServeMux()
	mux.Handle("/api/system/", http.StripPrefix("/api/system", module))
	mux.HandleFunc("POST /api/write", func(w http.ResponseWriter, r *http.Request) {
		if err := lock.Do(r.Context(), func() error { core.WriteJSON(w, 200, true); return nil }); err != nil {
			core.WriteError(w, 503, err.Error())
		}
	})
	return a, sessions, lock, Authenticate(a, sessions, "", mux)
}

func authRequest(h http.Handler, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "192.168.1.2:9999"
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestPasswordClaimLoginExpiryRotationAndLogout(t *testing.T) {
	a, sessions, _, h := authFixture(t)
	const password = "correct horse battery staple"
	claim := authRequest(h, "POST", "/api/system/access/claim", `{"password":"`+password+`"}`, nil)
	if claim.Code != 200 {
		t.Fatal(claim.Code, claim.Body)
	}
	data, _ := a.Store.Load()
	raw, _ := data.Raw(ModuleName)
	if strings.Contains(string(raw), password) {
		t.Fatal("plaintext password stored")
	}
	if w := authRequest(h, "POST", "/api/write", "", nil); w.Code != 401 {
		t.Fatal("password bypassed", w.Code)
	}
	if w := authRequest(h, "POST", "/api/system/access/login", `{"password":"wrong"}`, nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	login := authRequest(h, "POST", "/api/system/access/login", `{"password":"`+password+`"}`, nil)
	if login.Code != 200 {
		t.Fatal(login.Code, login.Body)
	}
	cookie := login.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/api" {
		t.Fatal("unsafe session cookie", cookie)
	}
	if w := authRequest(h, "POST", "/api/write", "", cookie); w.Code != 200 {
		t.Fatal(w.Code)
	}
	status := authRequest(h, "GET", "/api/system/access", "", cookie)
	var view AccessView
	if err := json.Unmarshal(status.Body.Bytes(), &view); err != nil || !view.Authenticated {
		t.Fatal(view, err)
	}
	sessions.Now = func() time.Time { return time.Now().Add(sessionLifetime + time.Second) }
	if w := authRequest(h, "POST", "/api/write", "", cookie); w.Code != 401 {
		t.Fatal("expired session accepted", w.Code)
	}
	sessions.Now = nil
	login = authRequest(h, "POST", "/api/system/access/login", `{"password":"`+password+`"}`, nil)
	cookie = login.Result().Cookies()[0]
	rotate := authRequest(h, "PUT", "/api/system/access/password", `{"password":"replacement password value"}`, cookie)
	if rotate.Code != 200 {
		t.Fatal(rotate.Code, rotate.Body)
	}
	if w := authRequest(h, "POST", "/api/write", "", cookie); w.Code != 401 {
		t.Fatal("rotation kept old session")
	}
	sessions.Now = func() time.Time { return time.Now().Add(61 * time.Second) }
	login = authRequest(h, "POST", "/api/system/access/login", `{"password":"replacement password value"}`, nil)
	if login.Code != 200 {
		t.Fatal(login.Code, login.Body)
	}
	cookie = login.Result().Cookies()[0]
	logout := authRequest(h, "POST", "/api/system/access/logout", "", cookie)
	if logout.Code != 200 || logout.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal(logout.Code, logout.Body)
	}
	if w := authRequest(h, "POST", "/api/write", "", cookie); w.Code != 401 {
		t.Fatal("logout kept old session")
	}
}

func TestHashValidationAndBoundedLoginAttempts(t *testing.T) {
	for _, value := range []string{"short", strings.Repeat("a", 1025)} {
		if _, err := HashPassword(value); err == nil {
			t.Fatal("invalid password accepted")
		}
	}
	for _, hash := range []string{"broken", "pbkdf2-sha256$999999999$a$b", "pbkdf2-sha256$600000$invalid$invalid"} {
		if (&Password{Hash: hash}).Matches("any") {
			t.Fatal("malformed hash accepted")
		}
	}
	s := NewSessions()
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "192.168.1.2:5555"
	for range 5 {
		done, ok := s.Begin(r)
		if !ok {
			t.Fatal("too early")
		}
		done()
	}
	if _, ok := s.Begin(r); ok {
		t.Fatal("unbounded guessing")
	}
	s.Now = func() time.Time { return time.Now().Add(61 * time.Second) }
	done, ok := s.Begin(r)
	if !ok {
		t.Fatal("expired limit did not recover")
	}
	done()
	p := &Password{Hash: "a hash"}
	w := httptest.NewRecorder()
	r.TLS = &tls.ConnectionState{}
	if err := s.Issue(w, r, p); err != nil {
		t.Fatal(err)
	}
	if !w.Result().Cookies()[0].Secure {
		t.Fatal("HTTPS cookie missing Secure")
	}
}

func TestQueuedMutationRechecksPasswordAfterAcquiringLock(t *testing.T) {
	a, _, lock, h := authFixture(t)
	if _, err := a.Claim(nil); err != nil {
		t.Fatal(err)
	}
	held, release, queued := make(chan struct{}), make(chan struct{}), make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_ = lock.Do(context.Background(), func() error {
			close(held)
			<-release
			_, err := a.SetPassword(&Password{Hash: "rotation"})
			return err
		})
	}()
	<-held
	// Place a notification after initial authorization but before the handler
	// takes the lock, so the race is deterministic without sleeps.
	module := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(queued)
		if err := lock.Do(r.Context(), func() error { w.WriteHeader(200); return nil }); err != nil {
			core.WriteError(w, 503, err.Error())
		}
	})
	h = Authenticate(a, NewSessions(), "", module)
	response := make(chan *httptest.ResponseRecorder, 1)
	go func() { response <- authRequest(h, "POST", "/api/write", "", nil) }()
	<-queued
	close(release)
	<-finished
	if w := <-response; w.Code == 200 {
		t.Fatal("queued unauthenticated write bypassed new password")
	}
}

func TestBearerAndPasswordSessionsCoexistAndFailClosed(t *testing.T) {
	a, sessions, _, _ := authFixture(t)
	password := &Password{Hash: "test credential"}
	if _, err := a.Claim(password); err != nil {
		t.Fatal(err)
	}
	const token = "automation-token"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	h := Authenticate(a, sessions, token, next)
	issued := httptest.NewRecorder()
	if err := sessions.Issue(issued, httptest.NewRequest("POST", "/", nil), password); err != nil {
		t.Fatal(err)
	}
	cookie := issued.Result().Cookies()[0]
	for _, tc := range []struct {
		bearer string
		cookie *http.Cookie
		code   int
	}{
		{"", nil, 401},
		{"wrong", nil, 401},
		{token, nil, 200},
		{"", cookie, 200},
		{"wrong", cookie, 200},
	} {
		r := httptest.NewRequest("POST", "/api/write", nil)
		if tc.bearer != "" {
			r.Header.Set("Authorization", "Bearer "+tc.bearer)
		}
		if tc.cookie != nil {
			r.AddCookie(tc.cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("bearer=%q cookie=%v: got %d, want %d", tc.bearer, tc.cookie != nil, w.Code, tc.code)
		}
	}
	if _, err := a.SetPassword(nil); err != nil {
		t.Fatal(err)
	}
	if w := authRequest(h, "POST", "/api/write", "", cookie); w.Code != 401 {
		t.Fatal("password-free mode bypassed required API token", w.Code)
	}
	if err := os.WriteFile(a.Store.Path(), []byte(`{"system":{"access":"invalid"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/write", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatal("unreadable access configuration did not fail closed", w.Code)
	}
}
