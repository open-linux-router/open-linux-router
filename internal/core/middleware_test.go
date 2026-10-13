package core

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecorderHijackUnsupported(t *testing.T) {
	r := &recorder{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK}
	_, _, err := r.Hijack()
	if !errors.Is(err, http.ErrNotSupported) {
		t.Fatalf("Hijack error = %v, want ErrNotSupported", err)
	}
	if r.wrote || r.status != http.StatusOK {
		t.Fatalf("failed hijack changed response state: wrote=%v status=%d", r.wrote, r.status)
	}
}
