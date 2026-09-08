// Package mcp serves olrd's Model Context Protocol surface, so that an agent
// can inspect the router the same way the WebUI and the CLI do (design.md §1,
// §6.4).
//
// # It is an API client, structurally
//
// This package takes an http.Handler — olrd's own API handler — and never
// imports a module. Every tool call is an HTTP request handed to that handler
// in this process, so there is no path from an agent to dhcp, dns, devices or
// routing that skips validation, skips the one global apply lock (§3.6), or
// fails to publish the change event the UI listens for. internal/webui states
// the same invariant for the SPA and for the same reason: a surface that can
// reach further than the API is no longer an equal client of it, and §1's claim
// that all of them are equal stops being true the first time one of them is
// not.
//
// The import graph is what enforces this, not a convention, and
// cmd/olrd/conformance_test.go fails the build if a module import appears here.
//
// # Nothing here is a list of routes
//
// Tools are derived at startup from /api/routes and /api/schema. A module that
// adds a route gets a tool without this package being edited, and a tool's
// arguments are the module's own schema projection passed through byte for byte
// (tools.go). design.md §3.2 rule 3 claims one tagged struct drives the CLI,
// REST, the UI and MCP; this is the part of that claim which is about MCP, and
// it is only true while this file contains no module knowledge.
//
// # Why the protocol is written out rather than imported
//
// The official Go SDK is a fine implementation and was measured before this was
// written. Two things ruled it out. From v1.5.0 it requires Go 1.25, against
// go.mod's floor of 1.23 which §8 states and Debian 13's toolchain has to meet
// — so adopting it means either freezing on v1.2.0 or moving the floor for
// every consumer of the project. And what it would supply is far more than what
// is served here: sessions, resumability, SSE fan-out, sampling, prompts,
// resources. This server has tools and nothing else, and a tools-only server
// over streamable HTTP is one POST handler and five methods.
//
// So the cost of writing it is small and bounded, and the cost of taking it is
// a seventh direct dependency plus a Go floor the project deliberately does not
// need. What we own in exchange is protocol drift, mitigated by advertising a
// version rather than the latest one: clients negotiate down, and the surface
// that could drift is the five methods below.
package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/buildinfo"
	"github.com/open-linux-router/open-linux-router/internal/core"
)

// serverName is how olrd introduces itself to a client.
const serverName = "open-linux-router"

// The protocol revisions this speaks.
//
// A server answers `initialize` with a version the client asked for when it can,
// and its own preferred one otherwise; the client then decides whether it can
// live with that. Advertising 2025-06-18 rather than the newest revision is
// deliberate — it is the revision whose tool semantics this implements, and
// claiming a newer one would be claiming behaviour (elicitation, structured
// content, task results) that is not here.
const preferredProtocolVersion = "2025-06-18"

var supportedProtocolVersions = []string{
	"2025-06-18",
	"2025-03-26",
	"2024-11-05",
}

// Server serves MCP over one POST endpoint.
//
// There is no session state, and so no Mcp-Session-Id: every request carries
// everything needed to answer it. That is worth stating because it is a real
// limitation and not an oversight — a stateless server cannot push
// notifications, so a tool list that changed while a client was connected would
// go unannounced. olrd's tool list is fixed at startup, so there is nothing to
// announce.
type Server struct {
	api   http.Handler
	tools map[string]tool

	// listed is the same tools in the order tools/list publishes them.
	listed []tool
}

// New builds the MCP server by asking the API what it serves.
//
// The discovery requests go through api, exactly as a tool call will, so a
// surface this cannot enumerate is one no client could have called anyway.
//
// An error here is a programming error rather than a runtime condition — the
// schema and the route table are both static — so cmd/olrd treats it as fatal.
// Finding out at boot beats finding out when an agent first connects.
func New(api http.Handler) (*Server, error) {
	if api == nil {
		return nil, fmt.Errorf("mcp: no API handler")
	}
	s := &Server{api: api}

	var routes struct {
		Modules map[string][]routeDoc `json:"modules"`
	}
	if err := s.get(core.APIPrefix+"/routes", &routes); err != nil {
		return nil, fmt.Errorf("mcp: reading the route table: %w", err)
	}

	var schema struct {
		Modules map[string]projections `json:"modules"`
	}
	if err := s.get(core.APIPrefix+"/schema", &schema); err != nil {
		return nil, fmt.Errorf("mcp: reading the schema: %w", err)
	}

	tools, err := buildTools(discovery{Routes: routes.Modules, Schema: schema.Modules})
	if err != nil {
		return nil, fmt.Errorf("mcp: building the tool list: %w", err)
	}
	if len(tools) == 0 {
		return nil, fmt.Errorf("mcp: no module published a tool")
	}

	s.listed = tools
	s.tools = make(map[string]tool, len(tools))
	for _, t := range tools {
		s.tools[t.Name] = t
	}
	return s, nil
}

// Tools returns the published tool names, sorted. For tests and docs.
func (s *Server) Tools() []string {
	out := make([]string, 0, len(s.listed))
	for _, t := range s.listed {
		out = append(out, t.Name)
	}
	return out
}

// get reads one API document during discovery.
func (s *Server) get(path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, "http://olr"+path, nil)
	if err != nil {
		return err
	}
	rec := newRecorder()
	s.api.ServeHTTP(rec, req)

	if rec.code != http.StatusOK {
		return fmt.Errorf("GET %s answered %d: %s", path, rec.code, strings.TrimSpace(rec.body.String()))
	}
	return json.Unmarshal(rec.body.Bytes(), out)
}

// ServeHTTP implements the streamable HTTP transport.
//
// Only the POST half. A server may decline the server-to-client stream, and
// this one does: with no sessions and no notifications there is nothing it
// would ever carry, and an idle SSE connection held open per client is a cost
// paid for nothing.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		core.WriteError(w, http.StatusMethodNotAllowed,
			"this MCP endpoint accepts POST only; it offers no server-to-client stream")
		return
	}

	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		core.WriteError(w, http.StatusUnsupportedMediaType,
			"Content-Type must be application/json")
		return
	}

	data, err := core.ReadBody(w, r)
	if err != nil {
		s.write(w, errorResponse(nil, codeParse, err.Error()))
		return
	}

	// A batch is a JSON array. The 2025-06-18 revision removed batching, and
	// saying so is better than the alternative failure, which is decoding the
	// array as a malformed single request and answering something confusing.
	if head := strings.TrimLeft(string(data), " \t\r\n"); strings.HasPrefix(head, "[") {
		s.write(w, errorResponse(nil, codeInvalidRequest,
			"JSON-RPC batching was removed in MCP 2025-06-18; send one request per POST"))
		return
	}

	var req request
	if err := json.Unmarshal(data, &req); err != nil {
		s.write(w, errorResponse(nil, codeParse, "invalid JSON: "+err.Error()))
		return
	}
	if req.JSONRPC != jsonRPCVersion {
		s.write(w, errorResponse(req.ID, codeInvalidRequest,
			fmt.Sprintf("jsonrpc must be %q", jsonRPCVersion)))
		return
	}

	resp, ok := s.dispatch(r, req)
	if !ok {
		// A notification. JSON-RPC forbids a response, and the transport says
		// to acknowledge it with 202 and an empty body.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	s.write(w, resp)
}

// dispatch answers one call. The second return is false for a notification,
// which is answered with nothing at all.
func (s *Server) dispatch(r *http.Request, req request) (response, bool) {
	if req.isNotification() {
		// Every notification this server can receive is one it has nothing to
		// do about: `initialized` tells it a handshake it is not tracking has
		// finished, and `cancelled` refers to a request that, being served
		// synchronously, has already returned. Accepting them silently is the
		// correct handling, not a stub.
		return response{}, false
	}

	switch req.Method {
	case "initialize":
		return s.initialize(req), true
	case "ping":
		return result(req.ID, struct{}{}), true
	case "tools/list":
		return result(req.ID, map[string]any{"tools": s.listed}), true
	case "tools/call":
		return s.callTool(r, req), true
	default:
		return errorResponse(req.ID, codeMethodNotFound,
			"this server implements tools only; it has no "+req.Method), true
	}
}

func (s *Server) initialize(req request) response {
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	// A malformed initialize is not worth refusing over: the only field read
	// is the version, and the fallback for a missing one is the same as for an
	// unsupported one.
	_ = json.Unmarshal(req.Params, &params)

	version := preferredProtocolVersion
	for _, v := range supportedProtocolVersions {
		if params.ProtocolVersion == v {
			version = v
			break
		}
	}

	return result(req.ID, map[string]any{
		"protocolVersion": version,
		// Tools, and deliberately nothing else. Declaring listChanged would
		// promise a notification a stateless server cannot send.
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    serverName,
			"version": buildinfo.Version,
		},
		"instructions": instructions,
	})
}

func (s *Server) callTool(r *http.Request, req request) response {
	var params struct {
		Name      string                     `json:"name"`
		Arguments map[string]json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errorResponse(req.ID, codeInvalidParams, "invalid params: "+err.Error())
	}

	t, ok := s.tools[params.Name]
	if !ok {
		// A protocol error rather than a tool error: there is no tool to have
		// failed, and this is a client bug rather than something the model
		// should try to work around.
		return errorResponse(req.ID, codeInvalidParams, "no such tool: "+params.Name)
	}

	return result(req.ID, s.call(r.Context(), t, params.Arguments))
}

func (s *Server) write(w http.ResponseWriter, resp response) {
	// Always 200. A JSON-RPC error is a successful HTTP response carrying an
	// error object; using an HTTP status for it would hide the code and the
	// message from clients that read the status first.
	core.WriteJSON(w, http.StatusOK, resp)
}

// instructions is what a client shows the model about this server as a whole.
//
// It says the two things that are not derivable from any single tool: that a
// plan is free and a change is not, and that the tool list is read-only today.
const instructions = "This is a Linux router running open-linux-router. " +
	"Tools here read the box's configuration and what it is actually doing: " +
	"leases, DNS queries, per-device traffic, and whether each module still matches its stored configuration. " +
	"The `plan` tools answer what a change would do without doing it, and are free to call. " +
	"No tool here changes the router; propose changes to the operator in prose."
