package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempStore(t *testing.T, modules ...string) *Store {
	t.Helper()
	if len(modules) == 0 {
		modules = []string{"link", "dhcp"}
	}
	return NewStore(filepath.Join(t.TempDir(), "olr.json"), modules...)
}

// A box that has never been configured is a legitimate state, not an error —
// it is exactly what a fresh install looks like.
func TestLoadMissingDocumentIsEmpty(t *testing.T) {
	doc, err := tempStore(t).Load()
	if err != nil {
		t.Fatalf("Load on a missing document: %v", err)
	}
	if _, ok := doc.Raw("dhcp"); ok {
		t.Error("a missing document produced a dhcp section")
	}
	if len(doc.Unknown()) != 0 {
		t.Errorf("a missing document reported unknown keys: %v", doc.Unknown())
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	s := tempStore(t)

	var doc Document
	doc.Set("dhcp", []byte(`{"enabled":true}`))
	if err := s.Save(doc); err != nil {
		t.Fatalf("Save: %v", err)
	}

	back, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	raw, ok := back.Raw("dhcp")
	if !ok {
		t.Fatal("dhcp section did not survive the round trip")
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshalling the subtree: %v", err)
	}
	if got["enabled"] != true {
		t.Errorf("enabled = %v, want true", got["enabled"])
	}
}

// The document carries the ownership header (design.md §3.4) as a "$schema"
// key, because JSON has no comments. It is written by the store and stripped on
// load, so it never reaches a module's decoder — which would reject it as an
// unknown field.
func TestSaveStampsSchemaAndLoadStripsIt(t *testing.T) {
	s := tempStore(t)

	var doc Document
	doc.Set("dhcp", []byte(`{"enabled":false}`))
	if err := s.Save(doc); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), SchemaURL) {
		t.Errorf("document carries no $schema:\n%s", data)
	}

	back, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := back.Raw(schemaKey); ok {
		t.Error("$schema survived the load and would reach a module's decoder")
	}
}

// The reason this matters is a downgrade. A box running an older olr must not
// destroy the configuration of a module it has never heard of just by saving an
// unrelated change.
func TestUnknownSectionsArePreservedAcrossASave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "olr.json")
	if err := os.WriteFile(path, []byte(
		`{"$schema":"x","dhcp":{"enabled":true},"qos":{"shaper":"cake"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// A build that has dhcp but not qos.
	s := NewStore(path, "dhcp")
	doc, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := []string{"qos"}; len(doc.Unknown()) != 1 || doc.Unknown()[0] != want[0] {
		t.Errorf("Unknown() = %v, want %v", doc.Unknown(), want)
	}

	doc.Set("dhcp", []byte(`{"enabled":false}`))
	if err := s.Save(doc); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"cake"`) {
		t.Errorf("an unknown section was dropped by a save:\n%s", data)
	}
}

// Mount order is dependency order (§3.2's literal list), so the file reads top
// to bottom the way the box is brought up rather than alphabetically.
func TestWriteOrderFollowsMountOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "olr.json")
	s := NewStore(path, "link", "dhcp")

	var doc Document
	doc.Set("dhcp", []byte(`{"enabled":true}`))
	doc.Set("link", []byte(`{"adopted":[]}`))
	doc.Set("zzz-unknown", []byte(`{}`))
	doc.Set("aaa-unknown", []byte(`{}`))
	if err := s.Save(doc); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	order := []string{`"$schema"`, `"link"`, `"dhcp"`, `"aaa-unknown"`, `"zzz-unknown"`}
	at := -1
	for _, key := range order {
		i := strings.Index(text, key)
		if i < 0 {
			t.Fatalf("%s missing from the document:\n%s", key, text)
		}
		if i < at {
			t.Errorf("%s is out of order; want %v:\n%s", key, order, text)
		}
		at = i
	}
}

// Indentation is the document's, not the module's: a subtree arrives however
// its module happened to marshal it, and the file has to look the same either
// way or the byte comparison downstream would report a change nobody made.
func TestOutputIsIndentedRegardlessOfSubtreeFormatting(t *testing.T) {
	s := tempStore(t, "dhcp")

	var compact, sprawling Document
	compact.Set("dhcp", []byte(`{"enabled":true,"pools":[]}`))
	sprawling.Set("dhcp", []byte("{\n\t\"enabled\":   true,\n\t\"pools\": [\n\n]\n}"))

	first, err := s.encode(compact)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	second, err := s.encode(sprawling)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("formatting leaked into the document:\n%s\n---\n%s", first, second)
	}
	if !strings.HasSuffix(string(first), "\n") {
		t.Error("document is not newline-terminated")
	}
}

func TestLoadReportsACorruptDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "olr.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(path, "dhcp").Load(); err == nil {
		t.Fatal("a corrupt document loaded without error")
	} else if !strings.Contains(err.Error(), path) {
		t.Errorf("error does not name the file: %v", err)
	}
}

// Migration shim, deleted with loadLegacy: an upgrade must not come up with an
// unconfigured box just because the per-module file is no longer read.
func TestLegacyPerModuleFilesAreFoldedInWhenTheDocumentIsAbsent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dhcp.json"), []byte(
		`{"$schema":"https://example.invalid/dhcp.json","enabled":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewStore(filepath.Join(dir, "olr.json"), "dhcp")
	doc, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	raw, ok := doc.Raw("dhcp")
	if !ok {
		t.Fatal("the legacy file was not folded in")
	}
	// The old files carried their own "$schema"; the key is the document's now
	// and a module's strict decoder would reject it.
	if strings.Contains(string(raw), "$schema") {
		t.Errorf("the legacy $schema key survived: %s", raw)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["enabled"] != true {
		t.Errorf("enabled = %v, want true", got["enabled"])
	}
}

// Once the document exists it is the only truth. A stale per-module file beside
// it is ignored rather than merged, so there is never a question of which won.
func TestLegacyFilesAreIgnoredOnceTheDocumentExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dhcp.json"), []byte(`{"enabled":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "olr.json")
	if err := os.WriteFile(path, []byte(`{"dhcp":{"enabled":false}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewStore(path, "dhcp")
	doc, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	raw, _ := doc.Raw("dhcp")
	if strings.Contains(string(raw), "true") {
		t.Errorf("the stale per-module file won over the document: %s", raw)
	}

	// It is still reported, so an operator can be told to delete it.
	if legacy := s.LegacyPaths(); len(legacy) != 1 {
		t.Errorf("LegacyPaths() = %v, want the one stale file", legacy)
	}
}

// Set must copy: a caller reusing its buffer would otherwise mutate the
// document underneath us.
func TestSetCopiesTheSubtree(t *testing.T) {
	buf := []byte(`{"enabled":true}`)
	var doc Document
	doc.Set("dhcp", buf)
	copy(buf, `{"enabled":fals`)

	raw, _ := doc.Raw("dhcp")
	if string(raw) != `{"enabled":true}` {
		t.Errorf("subtree aliased the caller's buffer: %s", raw)
	}
}
