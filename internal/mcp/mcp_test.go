package mcp

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// A stand-in for olrd: two modules, one of them shaped awkwardly on purpose.
func testAPI(t *testing.T) http.Handler {
	t.Helper()

	srv := core.New()
	srv.Mount("widgets", []core.Route{
		{
			Method: "GET", Path: "/config", Tool: "show config",
			Summary: "Show the stored widget configuration.",
			Handler: func(w http.ResponseWriter, r *http.Request) {
				core.WriteJSON(w, http.StatusOK, map[string]any{"name": "spline"})
			},
		},
		{
			Method: "GET", Path: "/list", Tool: "show list",
			Summary: "List widgets.",
			Query: []core.QueryParam{
				{Name: "limit", Type: "integer", Summary: "How many."},
				{Name: "deep", Type: "boolean", Summary: "Look harder."},
			},
			Handler: func(w http.ResponseWriter, r *http.Request) {
				core.WriteJSON(w, http.StatusOK, map[string]string{
					"limit": r.URL.Query().Get("limit"),
					"deep":  r.URL.Query().Get("deep"),
				})
			},
		},
		{
			Method: "POST", Path: "/plan", Tool: "show plan",
			Summary: "Plan a widget change.",
			Body:    core.BodyRelaxed,
			Handler: func(w http.ResponseWriter, r *http.Request) {
				body, _ := core.ReadBody(w, r)
				core.WriteJSON(w, http.StatusOK, map[string]string{"saw": string(body)})
			},
		},
		{
			// Mutating, and so deliberately unpublished.
			Method: "PUT", Path: "/config",
			Summary:  "Replace the widget configuration.",
			Body:     core.BodyFull,
			Mutating: true,
			Handler: func(w http.ResponseWriter, r *http.Request) {
				core.WriteJSON(w, http.StatusOK, struct{}{})
			},
		},
		{
			Method: "GET", Path: "/broken", Tool: "status",
			Summary: "Fail on purpose.",
			Handler: func(w http.ResponseWriter, r *http.Request) {
				core.WriteError(w, http.StatusUnprocessableEntity, "invalid widget configuration",
					core.Problem{Path: "widgets[0].name", Message: "must not be empty"})
			},
		},
	}, testConfig{})

	return srv.Handler()
}

type testConfig struct {
	Name  string `json:"name" jsonschema:"title=Name"`
	Count int    `json:"count,omitempty"`
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := New(testAPI(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// call drives the server the way a client does, over its actual HTTP surface,
// so the transport is exercised rather than only the dispatch beneath it.
func call(t *testing.T, s *Server, method string, params any) response {
	t.Helper()

	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		body["params"] = params
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(http.MethodPost, "http://olr/api/mcp", strings.NewReader(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	rec := newRecorder()
	s.ServeHTTP(rec, req)
	if rec.code != http.StatusOK {
		t.Fatalf("%s: HTTP %d: %s", method, rec.code, rec.body.String())
	}

	var resp response
	if err := json.Unmarshal(rec.body.Bytes(), &resp); err != nil {
		t.Fatalf("%s: %v (%s)", method, err, rec.body.String())
	}
	return resp
}

func toolResultOf(t *testing.T, resp response) toolResult {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	var out toolResult
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Content) == 0 {
		t.Fatal("result carried no content")
	}
	return out
}

// The tool list is derived from what the API published, not from a list here.
func TestToolsAreDerivedFromThePublishedRoutes(t *testing.T) {
	s := newTestServer(t)

	got := strings.Join(s.Tools(), " ")
	want := "widgets_show_config widgets_show_list widgets_show_plan widgets_status"
	if got != want {
		t.Errorf("tools = %q, want %q", got, want)
	}
}

// Writes are not published yet (increment 1 is read-only), and the rule that
// holds that is mechanical rather than a list of exceptions.
func TestMutatingRoutesArePublishedAsNoTool(t *testing.T) {
	s := newTestServer(t)
	for _, name := range s.Tools() {
		if strings.Contains(name, "replace") || strings.Contains(name, "set") {
			t.Errorf("%s is a write and should not be published yet", name)
		}
	}
}

// The whole design claim: a tool's arguments are the module's own schema,
// unchanged. If this passes through a second schema library it stops being true.
func TestBodySchemaIsThePublishedProjectionVerbatim(t *testing.T) {
	s := newTestServer(t)

	var published json.RawMessage
	for _, tl := range s.listed {
		if tl.Name == "widgets_show_plan" {
			var schema struct {
				Properties map[string]json.RawMessage `json:"properties"`
			}
			if err := json.Unmarshal(tl.InputSchema, &schema); err != nil {
				t.Fatal(err)
			}
			published = schema.Properties[configKey]
		}
	}
	if published == nil {
		t.Fatal("widgets_show_plan published no config property")
	}

	projections, err := core.Reflect("widgets", testConfig{})
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(projections.Relaxed)
	if err != nil {
		t.Fatal(err)
	}

	if string(published) != string(want) {
		t.Errorf("the tool's schema is not core's:\n got %s\nwant %s", published, want)
	}
}

// A relaxed projection is the right one for a plan: naming one field must not
// fail validation for omitting the others (§10).
func TestPlanTakesTheRelaxedProjection(t *testing.T) {
	s := newTestServer(t)

	for _, tl := range s.listed {
		if tl.Name != "widgets_show_plan" {
			continue
		}
		var schema struct {
			Properties map[string]struct {
				Required []string `json:"required"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(tl.InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		if req := schema.Properties[configKey].Required; len(req) != 0 {
			t.Errorf("plan requires %v; a partial change must not have to restate the document", req)
		}
	}
}

func TestInitializeNegotiatesTheProtocolVersion(t *testing.T) {
	s := newTestServer(t)

	tests := []struct{ asked, want string }{
		{"2025-06-18", "2025-06-18"},
		{"2024-11-05", "2024-11-05"},
		{"1999-01-01", preferredProtocolVersion},
		{"", preferredProtocolVersion},
	}

	for _, tt := range tests {
		resp := call(t, s, "initialize", map[string]any{"protocolVersion": tt.asked})
		var out struct {
			ProtocolVersion string `json:"protocolVersion"`
			Capabilities    struct {
				Tools map[string]any `json:"tools"`
			} `json:"capabilities"`
		}
		if err := json.Unmarshal(resp.Result, &out); err != nil {
			t.Fatal(err)
		}
		if out.ProtocolVersion != tt.want {
			t.Errorf("asked %q, got %q, want %q", tt.asked, out.ProtocolVersion, tt.want)
		}
		if out.Capabilities.Tools == nil {
			t.Error("tools capability was not declared")
		}
	}
}

// A tool call and a direct request must produce the same bytes, or the two
// surfaces have started to disagree about what the box says.
func TestToolCallReturnsWhatTheAPIReturns(t *testing.T) {
	api := testAPI(t)
	s, err := New(api)
	if err != nil {
		t.Fatal(err)
	}

	resp := call(t, s, "tools/call", map[string]any{"name": "widgets_show_config"})
	got := toolResultOf(t, resp)
	if got.IsError {
		t.Fatalf("call reported an error: %s", got.Content[0].Text)
	}

	direct := newRecorder()
	req, _ := http.NewRequest(http.MethodGet, "http://olr/api/widgets/config", nil)
	api.ServeHTTP(direct, req)

	if got.Content[0].Text != direct.body.String() {
		t.Errorf("tool said %q, the API said %q", got.Content[0].Text, direct.body.String())
	}
}

// Declared parameters have to survive as query string, in the spelling a
// handler parses — ?limit=5, not ?limit="5".
func TestArgumentsBecomeQueryParameters(t *testing.T) {
	s := newTestServer(t)

	resp := call(t, s, "tools/call", map[string]any{
		"name":      "widgets_show_list",
		"arguments": map[string]any{"limit": 5, "deep": true},
	})
	got := toolResultOf(t, resp)

	var seen map[string]string
	if err := json.Unmarshal([]byte(got.Content[0].Text), &seen); err != nil {
		t.Fatal(err)
	}
	if seen["limit"] != "5" {
		t.Errorf("limit reached the handler as %q, want \"5\"", seen["limit"])
	}
	if seen["deep"] != "true" {
		t.Errorf("deep reached the handler as %q, want \"true\"", seen["deep"])
	}
}

// A body argument reaches the handler as the document itself, not wrapped in
// the key it travelled under.
func TestBodyArgumentIsSentUnwrapped(t *testing.T) {
	s := newTestServer(t)

	resp := call(t, s, "tools/call", map[string]any{
		"name":      "widgets_show_plan",
		"arguments": map[string]any{configKey: map[string]any{"name": "spline"}},
	})
	got := toolResultOf(t, resp)

	var seen map[string]string
	if err := json.Unmarshal([]byte(got.Content[0].Text), &seen); err != nil {
		t.Fatal(err)
	}
	if seen["saw"] != `{"name":"spline"}` {
		t.Errorf("handler saw %q, want the bare document", seen["saw"])
	}
}

// A refused request must reach the model as text it can act on, with the field
// named — not as a protocol error the model never sees.
func TestRefusalIsAToolErrorNamingTheField(t *testing.T) {
	s := newTestServer(t)

	resp := call(t, s, "tools/call", map[string]any{"name": "widgets_status"})
	if resp.Error != nil {
		t.Fatalf("a refused request became a JSON-RPC error, which the model never sees: %s",
			resp.Error.Message)
	}
	got := toolResultOf(t, resp)

	if !got.IsError {
		t.Error("a 422 was not marked as an error")
	}
	if !strings.Contains(got.Content[0].Text, "widgets[0].name") {
		t.Errorf("the failing field was not named: %s", got.Content[0].Text)
	}
	if !strings.Contains(got.Content[0].Text, "must not be empty") {
		t.Errorf("the reason was not given: %s", got.Content[0].Text)
	}
}

// The summary belongs in the message and the detail in problems[]. Rendering
// both would say each problem twice — which is what `olr` did before
// core.ErrorBody.String became the one renderer.
func TestFailureTextDoesNotRepeatEachProblem(t *testing.T) {
	s := newTestServer(t)

	resp := call(t, s, "tools/call", map[string]any{"name": "widgets_status"})
	text := toolResultOf(t, resp).Content[0].Text

	if n := strings.Count(text, "must not be empty"); n != 1 {
		t.Errorf("the problem appears %d times:\n%s", n, text)
	}
}

func TestUnknownToolIsAProtocolError(t *testing.T) {
	s := newTestServer(t)

	resp := call(t, s, "tools/call", map[string]any{"name": "widgets_nope"})
	if resp.Error == nil {
		t.Fatal("an unknown tool should be a protocol error, not a tool result")
	}
	if resp.Error.Code != codeInvalidParams {
		t.Errorf("code = %d, want %d", resp.Error.Code, codeInvalidParams)
	}
}

func TestUnknownMethodIsMethodNotFound(t *testing.T) {
	s := newTestServer(t)

	resp := call(t, s, "resources/list", nil)
	if resp.Error == nil || resp.Error.Code != codeMethodNotFound {
		t.Errorf("resources/list should be method-not-found, got %+v", resp.Error)
	}
}

// A notification gets no response body at all; answering one is a protocol
// violation that some clients treat as fatal.
func TestNotificationIsAcknowledgedWithNoBody(t *testing.T) {
	s := newTestServer(t)

	req, _ := http.NewRequest(http.MethodPost, "http://olr/api/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	req.Header.Set("Content-Type", "application/json")

	rec := newRecorder()
	s.ServeHTTP(rec, req)

	if rec.code != http.StatusAccepted {
		t.Errorf("status = %d, want 202", rec.code)
	}
	if rec.body.Len() != 0 {
		t.Errorf("a notification was answered with %q", rec.body.String())
	}
}

// This server offers no server-to-client stream, and has to say so rather than
// leaving a client waiting on a GET that will never produce an event.
func TestGetIsRefused(t *testing.T) {
	s := newTestServer(t)

	req, _ := http.NewRequest(http.MethodGet, "http://olr/api/mcp", nil)
	rec := newRecorder()
	s.ServeHTTP(rec, req)

	if rec.code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.code)
	}
	if rec.header.Get("Allow") != http.MethodPost {
		t.Errorf("Allow = %q, want POST", rec.header.Get("Allow"))
	}
}

func TestBatchIsRefusedWithAnExplanation(t *testing.T) {
	s := newTestServer(t)

	req, _ := http.NewRequest(http.MethodPost, "http://olr/api/mcp",
		strings.NewReader(`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`))
	req.Header.Set("Content-Type", "application/json")

	rec := newRecorder()
	s.ServeHTTP(rec, req)

	var resp response
	if err := json.Unmarshal(rec.body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "batching") {
		t.Errorf("a batch was not explained: %+v", resp.Error)
	}
}

// The id is echoed exactly as it arrived. A client that sent 1 and is answered
// "1" is left waiting on a request it thinks is unanswered.
func TestRequestIDIsEchoedUnchanged(t *testing.T) {
	s := newTestServer(t)

	for _, id := range []string{`1`, `"abc"`, `null`} {
		req, _ := http.NewRequest(http.MethodPost, "http://olr/api/mcp",
			strings.NewReader(`{"jsonrpc":"2.0","id":`+id+`,"method":"ping"}`))
		req.Header.Set("Content-Type", "application/json")

		rec := newRecorder()
		s.ServeHTTP(rec, req)

		var resp response
		if err := json.Unmarshal(rec.body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if string(resp.ID) != id {
			t.Errorf("id %s came back as %s", id, resp.ID)
		}
	}
}

func TestNewFailsWithoutAnAPI(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Error("New(nil) should fail rather than serve an empty tool list")
	}
}
