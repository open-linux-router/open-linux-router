package mcp

import (
	"bytes"
	"net/http"
)

// recorder captures a handler's response in memory.
//
// net/http/httptest has one of these and it is better than this one, but
// importing that package registers an `-httptest.serve` flag at init — and olrd
// parses its flags with the stdlib flag package, so the flag would appear in
// `olrd --help` on a production binary. Twenty lines is a smaller price than a
// test-harness flag on a router's daemon.
type recorder struct {
	code        int
	header      http.Header
	body        bytes.Buffer
	wroteHeader bool
}

func newRecorder() *recorder {
	return &recorder{code: http.StatusOK, header: make(http.Header)}
}

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) WriteHeader(code int) {
	// First write wins, as with a real ResponseWriter: a handler that has
	// already committed a status and then writes again is reporting the first
	// one, and silently taking the second would make this disagree with what
	// the same handler sends over the wire.
	if r.wroteHeader {
		return
	}
	r.wroteHeader = true
	r.code = code
}

func (r *recorder) Write(b []byte) (int, error) {
	r.WriteHeader(http.StatusOK)
	return r.body.Write(b)
}
