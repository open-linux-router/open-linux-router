// Package core is the thin control plane that olrd is assembled from.
//
// It is a library modules call, not a framework that calls modules
// (design.md §3.1). There is no Module interface and no registry: a module
// hands core an http.Handler and its config struct, and core hands modules the
// apply lock, the event bus, and a handful of HTTP helpers. Nothing here
// iterates over modules except to concatenate their schemas, which is exactly
// the finding §3.1 rests on.
//
// Core holds no configuration and no observed state. Per design.md §10,
// `kill -9 olrd` followed by a restart must not change a single answer the API
// gives, so every read below goes to the module, which goes to the system.
package core

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// APIPrefix is the root of the HTTP API (design.md §6.2).
const APIPrefix = "/api"

// Server is the route table, the schema aggregator, and the apply lock.
type Server struct {
	lock   *Lock
	events *Events

	modules []*module
	byName  map[string]*module

	// handler is built once by Handler and reused. Mounting after that point
	// is a programming error and panics, so there is nothing to invalidate.
	handler http.Handler
}

type module struct {
	name    string
	handler http.Handler

	// schema is a zero value of the module's config struct. It is the single
	// source for the REST body, the UI form, and the MCP tool definition
	// (design.md §3.2 rule 3) — reflected, never hand-written.
	schema any
}

// New returns an empty server. Modules are mounted by the caller.
func New() *Server {
	return &Server{lock: NewLock(), events: NewEvents(), byName: map[string]*module{}}
}

// ApplyLock returns the one global apply lock (design.md §3.6).
//
// There is deliberately no per-module lock. A single lock is what makes
// cross-module validation sound: check dhcp's pool against link's subnet, then
// apply, with nothing able to move link in between (§5.3.1).
func (s *Server) ApplyLock() *Lock { return s.lock }

// Events returns the event bus that feeds the WebUI's live updates (§6.3).
func (s *Server) Events() *Events { return s.events }

// Mount registers a module's routes under /api/<name>/ and its config struct
// for schema reflection.
//
// The handler is mounted with the prefix stripped, so a module matches its own
// routes as `GET /config` rather than repeating its own name in every pattern.
//
// It panics on a bad or duplicate name, and on mounting after the handler has
// been built. Modules are a bounded literal list in cmd/olrd (§3.2), so these
// are startup-time programming errors, not runtime conditions — the same
// reasoning as cli.Verb panicking on a verb outside the vocabulary.
func (s *Server) Mount(name string, h http.Handler, schema any) {
	if s.handler != nil {
		panic("core: Mount after Handler; modules are mounted once at startup")
	}
	if err := validModuleName(name); err != nil {
		panic("core: " + err.Error())
	}
	if _, dup := s.byName[name]; dup {
		panic(fmt.Sprintf("core: module %q mounted twice", name))
	}
	if h == nil {
		panic(fmt.Sprintf("core: module %q mounted with a nil handler", name))
	}

	m := &module{name: name, handler: h, schema: schema}
	s.modules = append(s.modules, m)
	s.byName[name] = m
}

// Modules returns the mounted module names, sorted.
func (s *Server) Modules() []string {
	out := make([]string, 0, len(s.modules))
	for _, m := range s.modules {
		out = append(out, m.name)
	}
	sort.Strings(out)
	return out
}

// Handler returns the API handler. It serves /api/... and nothing else; the
// WebUI is composed alongside it by cmd/olrd rather than being core's concern.
//
// Calling it freezes the module list.
func (s *Server) Handler() http.Handler {
	if s.handler != nil {
		return s.handler
	}

	mux := http.NewServeMux()

	// Core's own routes. Kept to the three things §3 says core is: what exists,
	// what shape it has, and what just changed.
	mux.HandleFunc("GET "+APIPrefix+"/modules", s.handleModules)
	mux.HandleFunc("GET "+APIPrefix+"/schema", s.handleSchema)
	mux.HandleFunc("GET "+APIPrefix+"/schema/{module}", s.handleModuleSchema)
	mux.Handle("GET "+APIPrefix+"/events", s.events.Handler())

	for _, m := range s.modules {
		prefix := APIPrefix + "/" + m.name
		mux.Handle(prefix+"/", http.StripPrefix(prefix, m.handler))
	}

	// An unmatched /api/... path is a 404 in our JSON error shape rather than
	// net/http's text one, so a client never has to parse two error formats.
	mux.HandleFunc(APIPrefix+"/", func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, http.StatusNotFound, "no such endpoint: "+r.Method+" "+r.URL.Path)
	})

	s.handler = mux
	return mux
}

func (s *Server) handleModules(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]any{"modules": s.Modules()})
}

// handleSchema publishes every module's schema in both projections.
func (s *Server) handleSchema(w http.ResponseWriter, r *http.Request) {
	out := make(map[string]Projections, len(s.modules))
	for _, m := range s.modules {
		p, err := Reflect(m.name, m.schema)
		if err != nil {
			WriteError(w, http.StatusInternalServerError,
				fmt.Sprintf("reflecting %s schema: %v", m.name, err))
			return
		}
		out[m.name] = p
	}
	WriteJSON(w, http.StatusOK, map[string]any{"modules": out})
}

func (s *Server) handleModuleSchema(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("module")
	m, ok := s.byName[name]
	if !ok {
		WriteError(w, http.StatusNotFound, "no such module: "+name)
		return
	}
	p, err := Reflect(m.name, m.schema)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, p)
}

// validModuleName keeps a module name usable as a single path segment, so that
// mounting can stay a plain prefix match.
func validModuleName(name string) error {
	if name == "" {
		return fmt.Errorf("module name is empty")
	}
	if strings.ContainsAny(name, "/ \t") {
		return fmt.Errorf("module name %q contains a path separator or space", name)
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r == '-' {
			continue
		}
		return fmt.Errorf("module name %q must be lowercase letters and dashes", name)
	}
	return nil
}
