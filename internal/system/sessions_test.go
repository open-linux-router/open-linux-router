package system

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPersistentSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "sessions.json")
	s, err := OpenSessions(path)
	if err != nil {
		t.Fatal(err)
	}
	password := &Password{Hash: "password-hash"}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/login", nil)
	if err := s.Issue(w, r, password); err != nil {
		t.Fatal(err)
	}
	cookie := w.Result().Cookies()[0]
	if cookie.MaxAge != 90*24*60*60 {
		t.Fatal(cookie.MaxAge)
	}
	r.AddCookie(cookie)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), cookie.Value) {
		t.Fatal("raw token persisted")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	s, err = OpenSessions(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Valid(r, password) {
		t.Fatal("restart lost session")
	}
	if s.Valid(r, &Password{Hash: "changed"}) {
		t.Fatal("changed password accepted")
	}
	s, err = OpenSessions(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return time.Now().Add(89 * 24 * time.Hour) }
	if !s.Valid(r, password) {
		t.Fatal("session expired early")
	}
	s.Now = func() time.Time { return time.Now().Add(90*24*time.Hour + time.Second) }
	if s.Valid(r, password) {
		t.Fatal("expired session accepted")
	}
	s, err = OpenSessions(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Logout(httptest.NewRecorder(), r); err != nil {
		t.Fatal(err)
	}
	s, err = OpenSessions(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Valid(r, password) {
		t.Fatal("logout did not survive restart")
	}
	w = httptest.NewRecorder()
	if err := s.Issue(w, r, password); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("GET", "/api", nil)
	r.AddCookie(w.Result().Cookies()[0])
	if err := s.RevokeAll(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenSessions(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Valid(r, password) {
		t.Fatal("revocation did not survive restart")
	}
}

func TestSessionPersistenceFailure(t *testing.T) {
	s := NewSessions()
	s.path = t.TempDir() // A directory cannot be replaced by a session file.
	w := httptest.NewRecorder()
	if err := s.Issue(w, httptest.NewRequest("POST", "/api/login", nil), &Password{Hash: "hash"}); err == nil {
		t.Fatal("persistence failure reported success")
	}
	if len(w.Result().Cookies()) != 0 || len(s.sessions) != 0 {
		t.Fatal("failed issuance created a session")
	}
}
