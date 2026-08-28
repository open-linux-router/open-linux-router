package core

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// recorder captures the status code so logging can report it without the
// handler having to cooperate.
type recorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *recorder) WriteHeader(status int) {
	if !r.wrote {
		r.status = status
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.status = http.StatusOK
		r.wrote = true
	}
	return r.ResponseWriter.Write(b)
}

// Flush forwards to the underlying writer so that wrapping a handler does not
// break SSE. Without this the event stream buffers forever and the live UI
// looks frozen — the classic cost of a middleware that forgets it is standing
// in front of a streaming response.
func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// WithLogging logs one line per request to the journal (§3.4 — journald is the
// logging system; we do not build another).
func WithLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		level := slog.LevelDebug
		if rec.status >= 500 {
			level = slog.LevelError
		}
		// Reads are constant background noise from a polling UI; failures are
		// not. Logging every 200 at info would bury the one line that matters.
		slog.Default().Log(r.Context(), level, "request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start),
		)
	})
}

// WithRecovery turns a panic into a 500 instead of a dead daemon.
//
// olrd is resident and holds the admin API; a nil map write in one module's
// handler must not take the box's control plane down with it. The stack goes to
// the journal, because a panic we cannot diagnose later is barely better than
// a crash.
func WithRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wrapped so that a panic *after* the response started does not
		// provoke a superfluous-WriteHeader warning on top of the real problem.
		rec := &recorder{ResponseWriter: w, status: http.StatusOK}

		defer func() {
			v := recover()
			if v == nil {
				return
			}
			// ErrAbortHandler is net/http's own signal that a handler gave up
			// deliberately; re-panicking keeps its meaning.
			if v == http.ErrAbortHandler {
				panic(v)
			}
			slog.Default().Error("panic serving request",
				"method", r.Method,
				"path", r.URL.Path,
				"panic", v,
				"stack", string(debug.Stack()),
			)
			if !rec.wrote {
				WriteError(rec, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(rec, r)
	})
}
