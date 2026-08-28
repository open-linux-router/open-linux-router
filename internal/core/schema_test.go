package core

import (
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// Duration stands in for a module's own text-marshalling type.
type testDuration time.Duration

func (d testDuration) MarshalText() ([]byte, error) { return []byte("12h"), nil }

type testConfig struct {
	Enabled bool           `json:"enabled"`
	Name    string         `json:"name"`
	Addr    netip.Addr     `json:"addr"`
	Subnet  netip.Prefix   `json:"subnet,omitempty"`
	Lease   testDuration   `json:"lease,omitempty"`
	Servers []netip.Addr   `json:"servers,omitempty"`
	Nested  []testSubtable `json:"nested,omitempty"`
}

type testSubtable struct {
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

// typeOf walks to a property's declared JSON type.
func typeOf(t *testing.T, raw []byte, path ...string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshalling schema: %v", err)
	}
	cur := doc
	for _, step := range path {
		next, ok := cur[step].(map[string]any)
		if !ok {
			t.Fatalf("no %q under %v (have %v)", step, path, keys(cur))
		}
		cur = next
	}
	return cur
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func mustReflect(t *testing.T) Projections {
	t.Helper()
	p, err := Reflect("test", testConfig{})
	if err != nil {
		t.Fatalf("Reflect: %v", err)
	}
	return p
}

// netip.Addr is a struct of unexported fields that marshals as a string.
// Reflected naively it publishes as an empty object, and every surface derived
// from the schema — UI form, MCP tool, OpenAPI — is then wrong about the wire
// format. This is the regression that matters most in this file.
func TestAddressesPublishAsStringsNotObjects(t *testing.T) {
	raw, err := json.Marshal(mustReflect(t).Full)
	if err != nil {
		t.Fatal(err)
	}

	for _, prop := range []string{"addr", "subnet"} {
		got := typeOf(t, raw, "properties", prop)
		if got["type"] != "string" {
			t.Errorf("%s: type = %v, want string", prop, got["type"])
		}
	}

	items := typeOf(t, raw, "properties", "servers", "items")
	if items["type"] != "string" {
		t.Errorf("servers.items: type = %v, want string", items["type"])
	}
}

// A duration wrapper exists precisely so a 12h lease is not serialised as
// 43200000000000 (design.md §10). A schema claiming `integer` reintroduces the
// lie the wrapper was written to prevent.
func TestTextMarshallingTypesPublishAsStrings(t *testing.T) {
	raw, err := json.Marshal(mustReflect(t).Full)
	if err != nil {
		t.Fatal(err)
	}

	// The type lands in $defs and the property refs it; either shape is fine
	// so long as it resolves to a string.
	prop := typeOf(t, raw, "properties", "lease")
	if ref, ok := prop["$ref"].(string); ok {
		name := ref[strings.LastIndex(ref, "/")+1:]
		prop = typeOf(t, raw, "$defs", name)
	}
	if prop["type"] != "string" {
		t.Errorf("lease: type = %v, want string", prop["type"])
	}
}

// §10: reflection derives `required` from the absence of omitempty, and core
// publishes a relaxed projection so a single-field PATCH does not fail
// validation against its own schema.
func TestProjectionsDifferOnlyInRequired(t *testing.T) {
	p := mustReflect(t)

	if want := []string{"enabled", "name", "addr"}; len(p.Full.Required) != len(want) {
		t.Fatalf("full required = %v, want %v", p.Full.Required, want)
	}
	if len(p.Relaxed.Required) != 0 {
		t.Errorf("relaxed required = %v, want none", p.Relaxed.Required)
	}

	// Nested definitions must be relaxed too, or a partial update of an item
	// inside an array is rejected for omitting a sibling it never touched.
	for name, def := range p.Relaxed.Definitions {
		if len(def.Required) != 0 {
			t.Errorf("relaxed $defs.%s required = %v, want none", name, def.Required)
		}
	}
	full, ok := p.Full.Definitions["testSubtable"]
	if !ok {
		t.Fatalf("full schema has no testSubtable definition (have %v)", defNames(p))
	}
	if len(full.Required) == 0 {
		t.Error("full $defs.testSubtable should still require its non-omitempty field")
	}
}

func defNames(p Projections) []string {
	out := make([]string, 0, len(p.Full.Definitions))
	for k := range p.Full.Definitions {
		out = append(out, k)
	}
	return out
}

// Relaxing must not reach across into the full projection. They are reflected
// separately for this reason; sharing one sub-schema pointer would silently
// strip `required` from both.
func TestRelaxingDoesNotMutateTheFullProjection(t *testing.T) {
	p := mustReflect(t)
	if len(p.Full.Required) == 0 {
		t.Fatal("full projection lost its required list")
	}
}

func TestTitleComesFromTheModuleName(t *testing.T) {
	p := mustReflect(t)
	if p.Full.Title != "TestConfig" {
		t.Errorf("title = %q, want TestConfig", p.Full.Title)
	}
	if p.Relaxed.Title != p.Full.Title {
		t.Errorf("relaxed title = %q, want %q", p.Relaxed.Title, p.Full.Title)
	}
}

func TestReflectRejectsNil(t *testing.T) {
	if _, err := Reflect("test", nil); err == nil {
		t.Error("want an error reflecting nil")
	}
}
