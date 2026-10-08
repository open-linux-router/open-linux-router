package tools

import (
	"encoding/json"
	"errors"
	"github.com/open-linux-router/open-linux-router/internal/core"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLANSession(t *testing.T) {
	h := &HTTP{LANAddresses: func() ([]string, error) { return []string{"192.0.2.1"}, nil }, LANProbe: func(string) (string, error) { return "", nil }}
	routes := h.Routes()
	_ = routes
	handler := h.lanStatus
	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest(http.MethodGet, "/lan-test", nil))
	var view lanView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Port != 5201 || len(view.Addresses) != 1 {
		t.Fatalf("view = %+v", view)
	}
	for _, body := range []string{`{"address":"127.0.0.1"}`, `{"address":"192.0.2.2"}`, `{"address":"192.0.2.1; touch /tmp/foo"}`} {
		w = httptest.NewRecorder()
		h.startLAN(w, httptest.NewRequest(http.MethodPost, "/lan-test", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest && w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("body %s: status %d", body, w.Code)
		}
	}
}

func TestLANStopOnlyCurrentSession(t *testing.T) {
	h := &HTTP{}
	stopped := 0
	first := &lanSession{stop: func() { stopped++ }}
	second := &lanSession{stop: func() { stopped++ }}
	h.lan = second
	h.endLAN(first)
	if stopped != 0 || h.lan != second {
		t.Fatal("stopped replacement session")
	}
	h.endLAN(second)
	h.Close()
	if stopped != 1 || h.lan != nil {
		t.Fatal("did not stop once")
	}
}

func TestLANStartStopAndExpiry(t *testing.T) {
	done := make(chan struct{})
	stopped := 0
	h := &HTTP{
		LANAddresses: func() ([]string, error) { return []string{"192.0.2.1"}, nil },
		LANProbe:     func(string) (string, error) { return "", nil },
		StartIperf: func(address string, port int) (func(), <-chan struct{}, error) {
			if address != "192.0.2.1" || port != 5201 {
				t.Fatalf("listener %s:%d", address, port)
			}
			return func() { stopped++ }, done, nil
		},
	}
	routes := core.RouteTable(h.Routes())
	request := func(method, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, httptest.NewRequest(method, "/lan-test", strings.NewReader(body)))
		return w
	}
	first := request(http.MethodPost, `{"address":"192.0.2.1"}`)
	if first.Code != http.StatusOK {
		t.Fatalf("start: %d %s", first.Code, first.Body.String())
	}
	if request(http.MethodPost, `{"address":"192.0.2.1"}`).Code != http.StatusConflict {
		t.Fatal("accepted second session")
	}
	var view lanView
	if err := json.Unmarshal(request(http.MethodGet, "").Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Address != "192.0.2.1" || view.Expires.IsZero() {
		t.Fatalf("view = %+v", view)
	}
	if request(http.MethodDelete, "").Code != http.StatusNoContent || stopped != 1 {
		t.Fatal("stop failed")
	}
	close(done)
	h.Close()
	if stopped != 1 {
		t.Fatal("stopped twice")
	}
}

func TestLANAdoptsNoExternalProcess(t *testing.T) {
	starts := 0
	h := &HTTP{
		LANAddresses: func() ([]string, error) { return []string{"192.0.2.1"}, nil },
		LANProbe:     func(string) (string, error) { return "iperf3 (pid 123, iperf3.service)", nil },
		StartIperf:   func(string, int) (func(), <-chan struct{}, error) { starts++; return nil, nil, nil },
	}
	routes := core.RouteTable(h.Routes())
	get := httptest.NewRecorder()
	routes.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/lan-test", nil))
	var view lanView
	if err := json.Unmarshal(get.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.External["192.0.2.1"] == "" || view.Address != "" {
		t.Fatalf("view = %+v", view)
	}
	post := httptest.NewRecorder()
	routes.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/lan-test", strings.NewReader(`{"address":"192.0.2.1"}`)))
	if post.Code != http.StatusOK || starts != 0 || h.lan != nil {
		t.Fatalf("post %d, starts %d", post.Code, starts)
	}
	del := httptest.NewRecorder()
	routes.ServeHTTP(del, httptest.NewRequest(http.MethodDelete, "/lan-test", nil))
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete %d", del.Code)
	}
}

func TestLANReportsOtherPortHolder(t *testing.T) {
	h := &HTTP{
		LANAddresses: func() ([]string, error) { return []string{"192.0.2.1"}, nil },
		LANProbe:     func(string) (string, error) { return "", errors.New("TCP port 5201 is held by another service") },
		StartIperf: func(string, int) (func(), <-chan struct{}, error) {
			t.Fatal("started over a conflict")
			return nil, nil, nil
		},
	}
	routes := core.RouteTable(h.Routes())
	get := httptest.NewRecorder()
	routes.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/lan-test", nil))
	var view lanView
	if err := json.Unmarshal(get.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Conflicts["192.0.2.1"] == "" {
		t.Fatalf("view = %+v", view)
	}
	post := httptest.NewRecorder()
	routes.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/lan-test", strings.NewReader(`{"address":"192.0.2.1"}`)))
	if post.Code != http.StatusConflict {
		t.Fatalf("post = %d", post.Code)
	}
}
