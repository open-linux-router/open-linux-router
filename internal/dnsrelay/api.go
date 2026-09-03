package dnsrelay

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// The read-only observation socket.
//
// This is the *read* direction and only the read direction. Configuration still
// arrives as rendered files plus SIGHUP, which is what design.md §3.5's
// corollary is about — it names two costs of a private control channel, and a
// read-only socket incurs neither: the relay still starts from its files with
// nothing else alive on the box, and `curl --unix-socket` on it is a better
// debugging story than parsing dnsmasq's lease file, not a worse one.
//
// It is a socket rather than a file for one practical reason. The alternative
// is appending a line per query to disk, and a great many olr boxes boot from
// an SD card, where that is continuous write wear for a log nobody has asked to
// keep across reboots.

// apiServer is the observation listener.
type apiServer struct {
	server   *http.Server
	listener net.Listener
	path     string
}

// Close stops serving and removes the socket.
func (a *apiServer) Close() {
	if a == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = a.server.Shutdown(ctx)
	a.listener.Close()
	// systemd removes the RuntimeDirectory on stop, but the relay also exits by
	// other routes, and a stale socket file makes the next start fail to bind.
	_ = os.Remove(a.path)
}

// serveObservations starts the read-only API.
func (r *Relay) serveObservations(ctx context.Context) (*apiServer, error) {
	path := r.cfg.ObserveSocket

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	// A socket left behind by a killed process would otherwise make every
	// subsequent start fail with "address already in use", which under
	// Restart=always is a crash loop rather than a one-off.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("removing the stale socket %s: %w", path, err)
	}

	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", path, err)
	}
	// Owner-only. The query log is a record of what everybody in the building
	// looked up, which is about as sensitive as anything on the box; it is not
	// something to leave world-readable because the default mode said so.
	if err := os.Chmod(path, 0o600); err != nil {
		listener.Close()
		return nil, fmt.Errorf("setting the mode on %s: %w", path, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /queries", r.handleQueries)
	mux.HandleFunc("GET /names", r.handleNames)
	mux.HandleFunc("GET /stats", r.handleStats)

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			r.logger.Warn("observation socket stopped", "error", err)
		}
	}()

	r.logger.Info("serving observations", "socket", path)
	return &apiServer{server: server, listener: listener, path: path}, nil
}

func (r *Relay) handleQueries(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, QueriesResponse{
		Queries: r.Queries(),
		Stats:   r.Snapshot(),
	})
}

func (r *Relay) handleNames(w http.ResponseWriter, _ *http.Request) {
	now := time.Now()
	writeJSON(w, NamesResponse{
		Names: r.Names(now),
		Stats: r.Snapshot(),
	})
}

func (r *Relay) handleStats(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, r.Snapshot())
}

func writeJSON(w http.ResponseWriter, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "encoding response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(body)
}
