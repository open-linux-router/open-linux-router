package system

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func TestConcurrentClaimsRecordOnlyOneDecision(t *testing.T) {
	a := Applier{Store: core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName)}
	h := HTTP{Applier: a, Lock: core.NewLock()}.Handler()
	var wg sync.WaitGroup
	codes := make(chan int, 20)
	for range 20 {
		wg.Go(func() {
			r := httptest.NewRequest(http.MethodPost, "/access/claim", strings.NewReader(`{"no_password":true}`))
			r.RemoteAddr = "127.0.0.1:9000"
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			codes <- w.Code
		})
	}
	wg.Wait()
	close(codes)
	success := 0
	for code := range codes {
		if code == http.StatusOK {
			success++
		} else if code != http.StatusConflict {
			t.Fatal(code)
		}
	}
	if success != 1 {
		t.Fatalf("successful claims = %d, want 1", success)
	}
}

func TestClaimCannotBypassHeldApplyLock(t *testing.T) {
	a := Applier{Store: core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName)}
	lock := core.NewLock()
	h := HTTP{Applier: a, Lock: lock}.Handler()
	if err := lock.Do(context.Background(), func() error {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r := httptest.NewRequest(http.MethodPost, "/access/claim", strings.NewReader(`{"no_password":true}`)).WithContext(ctx)
		r.RemoteAddr = "127.0.0.1:9000"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("claim bypassed lock: %d %s", w.Code, w.Body)
		}
		cfg, err := a.Load()
		if err != nil || cfg.Claimed() {
			t.Fatalf("canceled claim stored intent: %v %v", cfg, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
