package cli

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// serveSocket runs h on a unix socket and returns a client for it.
func serveSocket(t *testing.T, h http.Handler) *Client {
	t.Helper()

	// Sockets live under a short path: the sun_path limit is around 100 bytes
	// and a test name can be long enough to overrun a temp dir plus a filename.
	socket := filepath.Join(t.TempDir(), "s")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listening on %s: %v", socket, err)
	}

	srv := &httptest.Server{
		Listener: listener,
		Config:   &http.Server{Handler: h},
	}
	srv.Start()
	t.Cleanup(srv.Close)

	return NewClient(socket)
}

func TestClientGetDecodesTheBody(t *testing.T) {
	c := serveSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/dhcp/config" {
			t.Errorf("path = %q", r.URL.Path)
		}
		core.WriteJSON(w, http.StatusOK, map[string]any{"enabled": true})
	}))

	var out struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.Get(context.Background(), "/api/dhcp/config", &out); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !out.Enabled {
		t.Error("body was not decoded")
	}
}

func TestClientSendsTheBody(t *testing.T) {
	var got string
	c := serveSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := core.ReadBody(w, r)
		got = string(data)
		core.WriteJSON(w, http.StatusOK, map[string]any{})
	}))

	body := map[string]any{"enabled": false}
	if err := c.Put(context.Background(), "/api/dhcp/config", body, nil); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !strings.Contains(got, `"enabled":false`) {
		t.Errorf("server received %q", got)
	}
}

// A rejected config comes back with problems addressed by field path. Rendering
// them from the server's own bytes is what keeps the CLI and the WebUI saying
// the same thing about the same mistake.
func TestClientSurfacesAddressedProblems(t *testing.T) {
	c := serveSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		core.WriteError(w, http.StatusUnprocessableEntity, "invalid dhcp configuration",
			core.Problem{Path: "pools[0].start", Message: "required"})
	}))

	err := c.Put(context.Background(), "/api/dhcp/config", map[string]any{}, nil)
	if err == nil {
		t.Fatal("a 422 was not reported as an error")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not an *APIError: %T %v", err, err)
	}
	if apiErr.Status != http.StatusUnprocessableEntity {
		t.Errorf("Status = %d", apiErr.Status)
	}
	if len(apiErr.Problems) != 1 || apiErr.Problems[0].Path != "pools[0].start" {
		t.Errorf("problems = %+v", apiErr.Problems)
	}
	if !strings.Contains(err.Error(), "pools[0].start: required") {
		t.Errorf("message does not carry the field path: %v", err)
	}
}

// design.md §5.3.2: there is no rollback, so a failed apply answers with the
// steps that did land. Discarding the body on a non-2xx would leave the
// operator to guess what state the box is in.
func TestClientDecodesTheBodyOfAFailedApply(t *testing.T) {
	c := serveSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		core.WriteJSON(w, http.StatusInternalServerError, map[string]any{
			"steps": []map[string]any{
				{"description": "store configuration", "done": true},
				{"description": "start olr-dhcp.service", "done": false, "error": "boom"},
			},
			"error": map[string]any{"message": "boom"},
		})
	}))

	var out struct {
		Steps []struct {
			Description string `json:"description"`
			Done        bool   `json:"done"`
		} `json:"steps"`
	}
	err := c.Put(context.Background(), "/api/dhcp/config", map[string]any{}, &out)
	if err == nil {
		t.Fatal("a 500 was not reported as an error")
	}
	if len(out.Steps) != 2 {
		t.Fatalf("steps were discarded: %+v", out.Steps)
	}
	if !out.Steps[0].Done || out.Steps[1].Done {
		t.Errorf("steps did not survive intact: %+v", out.Steps)
	}
}

// "olrd is not running" is far and away the most common reason to fail here,
// and a raw syscall error naming a socket path says nothing about that.
func TestClientExplainsAnUnreachableDaemon(t *testing.T) {
	c := NewClient(filepath.Join(t.TempDir(), "absent.sock"))

	err := c.Get(context.Background(), "/api/dhcp/config", nil)
	if err == nil {
		t.Fatal("connecting to a socket that does not exist succeeded")
	}
	if !strings.Contains(err.Error(), "olr daemon start") {
		t.Errorf("error does not say how to fix it: %v", err)
	}
}
