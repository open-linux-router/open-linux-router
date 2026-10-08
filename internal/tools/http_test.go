package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func TestSpeedtestReturnsResult(t *testing.T) {
	h := &HTTP{Run: func(context.Context) (SpeedResult, error) {
		return SpeedResult{Server: "Test", Download: 123.4, Upload: 56.7}, nil
	}}
	w := httptest.NewRecorder()
	core.RouteTable(h.Routes()).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/speedtest", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var result SpeedResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Server != "Test" || result.Download != 123.4 || result.Upload != 56.7 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestSpeedtestRejectsConcurrentRun(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	h := &HTTP{Run: func(context.Context) (SpeedResult, error) {
		close(started)
		<-release
		return SpeedResult{}, nil
	}}
	routes := core.RouteTable(h.Routes())
	done := make(chan struct{})
	go func() {
		defer close(done)
		routes.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/speedtest", nil))
	}()
	<-started
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/speedtest", nil))
	close(release)
	<-done
	if w.Code != http.StatusConflict {
		t.Fatalf("concurrent request returned %d, want 409", w.Code)
	}
}
