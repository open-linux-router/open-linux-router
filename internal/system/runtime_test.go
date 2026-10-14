package system

import (
	"github.com/open-linux-router/open-linux-router/internal/runtimedb"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestSQLiteSessionMigrationRevocationAndFailure(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "sessions.json")
	s, err := OpenSessions(legacy)
	if err != nil {
		t.Fatal(err)
	}
	p := &Password{Hash: "credential"}
	r := httptest.NewRequest("POST", "/api/login", nil)
	w := httptest.NewRecorder()
	if err := s.Issue(w, r, p); err != nil {
		t.Fatal(err)
	}
	r.AddCookie(w.Result().Cookies()[0])
	db, err := runtimedb.Open(filepath.Join(dir, "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	s, err = OpenSessionsDB(db, legacy)
	if err != nil || !s.Valid(r, p) {
		t.Fatal("legacy session lost", err)
	}
	if err := s.Logout(httptest.NewRecorder(), r); err != nil {
		t.Fatal(err)
	}
	s, err = OpenSessionsDB(db, legacy)
	if err != nil || s.Valid(r, p) {
		t.Fatal("revoked legacy session resurrected", err)
	}
	db.Close()
	w = httptest.NewRecorder()
	if err := s.Issue(w, r, p); err == nil || len(w.Result().Cookies()) != 0 {
		t.Fatal("failed commit issued credential")
	}
}
