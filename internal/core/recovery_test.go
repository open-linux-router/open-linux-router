package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testRecovery(t *testing.T) *Recovery {
	t.Helper()
	s := tempStore(t)
	d := Document{}
	d.Set("link", []byte(`{"networks":[]}`))
	if err := s.Save(d); err != nil {
		t.Fatal(err)
	}
	r := &Recovery{Store: s, Lock: NewLock(), Path: filepath.Join(t.TempDir(), "recovery.json")}
	r.Rollback = func(ctx context.Context, from, before Document, runtime json.RawMessage) error { return s.Save(before) }
	return r
}

func TestRecoveryRestartAndConfirmation(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unconfirmed", true: "confirmed"}[confirmed], func(t *testing.T) {
			r := testRecovery(t)
			if err := r.Begin(); err != nil {
				t.Fatal(err)
			}
			info, _ := os.Stat(r.Path)
			if info.Mode().Perm() != 0o600 {
				t.Fatal("recovery journal exposes credentials")
			}
			d, _ := r.Store.Load()
			d.Set("link", []byte(`{"networks":["trial"]}`))
			_ = r.Store.Save(d)
			if confirmed {
				w := httptest.NewRecorder()
				r.finish(false)(w, httptest.NewRequest("POST", "/confirm", strings.NewReader(`{"id":"`+r.View().ID+`"}`)))
				if w.Code != 200 {
					t.Fatal(w.Body.String())
				}
			}
			restarted := &Recovery{Store: r.Store, Lock: r.Lock, Path: r.Path, Rollback: r.Rollback}
			if err := restarted.Load(context.Background()); err != nil {
				t.Fatal(err)
			}
			d, _ = r.Store.Load()
			raw, _ := d.Raw("link")
			if strings.Contains(string(raw), "trial") != confirmed {
				t.Fatalf("unexpected recovered configuration: %s", raw)
			}
			if restarted.View().State != "idle" {
				t.Fatal(restarted.View())
			}
		})
	}
}

func TestRecoveryRetriesOriginalTrialAndBlocksEdits(t *testing.T) {
	r := testRecovery(t)
	if err := r.Begin(); err != nil {
		t.Fatal(err)
	}
	d, _ := r.Store.Load()
	d.Set("link", []byte(`{"networks":["trial"]}`))
	_ = r.Store.Save(d)
	called := 0
	r.Rollback = func(ctx context.Context, from, before Document, runtime json.RawMessage) error {
		raw, _ := from.Raw("link")
		if !strings.Contains(string(raw), "trial") {
			t.Fatal("retry forgot trial resources")
		}
		if err := r.Store.Save(before); err != nil {
			return err
		}
		called++
		if called == 1 {
			return errors.New("backend temporarily unavailable")
		}
		return nil
	}
	if err := r.revert(context.Background()); err == nil {
		t.Fatal("failure hidden")
	}
	if r.View().State != "failed" {
		t.Fatal(r.View())
	}
	var ran bool
	route := r.Wrap(Route{Method: "PUT", Path: "/config", Mutating: true, Handler: func(w http.ResponseWriter, req *http.Request) {
		if err := r.Lock.Do(req.Context(), func() error { ran = true; return nil }); err == nil {
			t.Fatal("pending recovery did not block mutation")
		}
	}})
	route.Handler(httptest.NewRecorder(), httptest.NewRequest("PUT", "/config", nil))
	if ran {
		t.Fatal("mutation ran")
	}
	restarted := &Recovery{Store: r.Store, Lock: r.Lock, Path: r.Path, Rollback: r.Rollback}
	if err := restarted.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if called != 2 || restarted.View().State != "idle" {
		t.Fatal("recovery did not retry")
	}
}

func TestRecoveryRejectsStaleAndExpiredConfirmation(t *testing.T) {
	r := testRecovery(t)
	r.Timeout = -time.Second
	if err := r.Begin(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"stale", r.View().ID} {
		w := httptest.NewRecorder()
		r.finish(false)(w, httptest.NewRequest("POST", "/confirm", strings.NewReader(`{"id":"`+id+`"}`)))
		if w.Code != 409 || r.View().State != "pending" {
			t.Fatal(w.Code, r.View())
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx, nil)
	deadline := time.Now().Add(3 * time.Second)
	for r.View().State != "idle" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if r.View().State != "idle" {
		t.Fatal("expired trial was not reverted")
	}
}

func TestMutationGuardsCompose(t *testing.T) {
	called := false
	ctx := WithMutationGuard(context.Background(), func() error { return errors.New("authorization revoked") })
	ctx = WithMutationGuard(ctx, func() error { called = true; return nil })
	if err := NewLock().Do(ctx, func() error { t.Fatal("unauthorized write"); return nil }); err == nil || called {
		t.Fatal("authorization guard was replaced")
	}
}

func TestRecoveryContinuationKeepsOriginalDeadline(t *testing.T) {
	r := testRecovery(t)
	if err := r.Begin(); err != nil {
		t.Fatal(err)
	}
	before := r.View()
	route := r.Wrap(Route{Method: "PUT", Path: "/config", Mutating: true, Handler: func(w http.ResponseWriter, req *http.Request) {
		err := r.Lock.Do(req.Context(), func() error { return ProtectConnectivity(req.Context(), true) })
		if err != nil {
			WriteError(w, 409, err.Error())
			return
		}
		WriteJSON(w, 200, map[string]bool{"applied": true})
	}})
	for _, id := range []string{"", before.ID} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("PUT", "/config", nil)
		req.Header.Set(RecoveryHeader, id)
		route.Handler(w, req)
		if id == "" {
			if w.Code != 409 || w.Header().Get(RecoveryHeader) != "" {
				t.Fatal("unrelated edit joined trial")
			}
		} else if w.Code != 200 || w.Header().Get(RecoveryHeader) != id {
			t.Fatal("continuation refused", w.Body)
		}
		if r.View() != before {
			t.Fatal("continuation extended or replaced recovery point")
		}
	}
}
