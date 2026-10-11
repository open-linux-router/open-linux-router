package firewall

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const nativeFixture = `{"nftables":[{"table":{"family":"inet","name":"olr_filter","handle":1}},{"chain":{"family":"inet","table":"olr_filter","name":"input","type":"filter","hook":"input","prio":0,"policy":"drop","handle":2}},{"rule":{"family":"inet","table":"olr_filter","chain":"input","handle":3,"expr":[{"counter":{"packets":12,"bytes":100}},{"accept":null}]}}]}`

func TestNativeScopeAndSyntax(t *testing.T) {
	for _, raw := range []string{
		`{"nftables":[{"flush":{"ruleset":null}}]}`,
		`{"nftables":[{"table":{"family":"ip","name":"olr_filter"}}]}`,
		`{"nftables":[{"table":{"family":"inet","name":"docker"}}]}`,
		`{"nftables":[{"rule":{"family":"inet","table":"olr_filter","chain":"input","expr":[]}}]}`,
		`{"nftables":[{"table":{"family":"inet","name":"olr_filter"}},{"chain":{"family":"inet","table":"docker","name":"input"}}]}`,
		`{"nftables":[{"add":{"table":{"family":"inet","name":"olr_filter"}}}]}`,
	} {
		if _, err := nativeCommands([]byte(raw)); err == nil {
			t.Fatalf("accepted out-of-scope/command document: %s", raw)
		}
	}
	commands, err := nativeCommands([]byte(nativeFixture))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(commands)
	if bytes.Contains(b, []byte(`"handle"`)) {
		t.Fatal("kernel handles were sent as creation identities")
	}
	if !bytes.Contains(b, []byte(`"policy":"drop"`)) {
		t.Fatal("native chain policy changed")
	}
}

func TestNativeAtomicBatchPreservesOtherTables(t *testing.T) {
	current := strings.Replace(nativeFixture, `{"nftables":[`, `{"nftables":[{"table":{"family":"inet","name":"docker"}},`, 1)
	batch, err := nativeBatch([]byte(nativeFixture), []byte(current), true)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(batch, []byte("docker")) || bytes.Contains(batch, []byte("flush")) {
		t.Fatal("batch touched other tables")
	}
	var doc struct {
		Nftables []map[string]any `json:"nftables"`
	}
	if err := json.Unmarshal(batch, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Nftables) != 4 || doc.Nftables[0]["delete"] == nil {
		t.Fatalf("not one delete-and-create transaction: %s", batch)
	}
	off, err := nativeBatch([]byte(nativeFixture), []byte(current), false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(off), `"delete"`) != 1 || bytes.Contains(off, []byte(`"add"`)) {
		t.Fatalf("off batch: %s", off)
	}
}

func TestNativeComparisonIgnoresCountersNotExpressions(t *testing.T) {
	base, err := canonicalNative([]byte(nativeFixture))
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.NewReplacer(`"handle":3`, `"handle":99`, `"packets":12`, `"packets":999`, `"bytes":100`, `"bytes":99999`).Replace(nativeFixture)
	other, _ := canonicalNative([]byte(changed))
	if !bytes.Equal(base, other) {
		t.Fatal("moving counters/handles caused drift")
	}
	other, _ = canonicalNative([]byte(strings.Replace(nativeFixture, `"accept":null`, `"drop":null`, 1)))
	if bytes.Equal(base, other) {
		t.Fatal("verdict change hidden")
	}
}

func fakeNFT(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell test helper")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "nft")
	script := "#!/bin/sh\nif [ \"$2\" = list ]; then printf '%s' \"$NFT_TEST_CURRENT\"; exit 0; fi\nif [ \"$2\" = -c ] && [ \"$NFT_TEST_INVALID\" = 1 ]; then echo 'invalid native expression' >&2; exit 1; fi\ncat > \"$NFT_TEST_BATCH\"\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NFT_TEST_CURRENT", nativeFixture)
	batch := filepath.Join(dir, "batch.json")
	t.Setenv("NFT_TEST_BATCH", batch)
	return batch
}

func TestNativePlanningAndExecution(t *testing.T) {
	batchPath := fakeNFT(t)
	c := Config{Enabled: true, Ruleset: json.RawMessage(nativeFixture)}
	a := Applier{}
	p, _, err := a.Plan(context.Background(), c, "")
	if err != nil || !p.Empty {
		t.Fatalf("unchanged plan: %+v %v", p, err)
	}
	c.Ruleset = json.RawMessage(strings.Replace(nativeFixture, `"accept":null`, `"drop":null`, 1))
	p, d, err := a.Plan(context.Background(), c, "")
	if err != nil || p.Empty || p.Impact != ImpactDisruptive || len(p.Warnings) == 0 {
		t.Fatalf("changed plan: %+v %v", p, err)
	}
	if err := applyNative(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	batch, err := os.ReadFile(batchPath)
	if err != nil || !bytes.Contains(batch, []byte(`"delete"`)) || !bytes.Contains(batch, []byte(`"drop":null`)) {
		t.Fatalf("batch %s, %v", batch, err)
	}
	t.Setenv("NFT_TEST_INVALID", "1")
	if _, _, err := a.Plan(context.Background(), c, ""); err == nil {
		t.Fatal("invalid nft expression passed planning")
	}
}

func TestNativeConfigPersistsUnmodifiedExpressions(t *testing.T) {
	c, err := UnmarshalConfig([]byte(`{"enabled":true,"ruleset":` + nativeFixture + `}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := MarshalConfig(c)
	if err != nil {
		t.Fatal(err)
	}
	next, err := UnmarshalConfig(raw)
	if err != nil || !bytes.Equal(next.Ruleset, c.Ruleset) {
		t.Fatal("native document did not survive persistence", err)
	}
	patched, err := merge(c, []byte(`{"enabled":false}`))
	before, _ := canonicalNative(c.Ruleset)
	after, _ := canonicalNative(patched.Ruleset)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("switch erased native rules", err)
	}
}

func TestNativeHTTPValidationConfirmationAndStaleEdits(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux kernel implementation")
	}
	fakeNFT(t)
	store := core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName)
	h := HTTP{Applier: Applier{Store: store, Kernel: NewKernel()}, Lock: core.NewLock(), Events: core.NewEvents()}
	request := func(method, path, body, revision string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if revision != "" {
			r.Header.Set("If-Match", revision)
		}
		w := httptest.NewRecorder()
		h.Handler().ServeHTTP(w, r)
		return w
	}
	read := request("GET", "/ruleset", "", "")
	if read.Code != http.StatusOK {
		t.Fatal(read.Body.String())
	}
	var view nativeView
	if err := json.Unmarshal(read.Body.Bytes(), &view); err != nil || view.Revision == "" {
		t.Fatal("missing revision", err)
	}
	changed := strings.Replace(nativeFixture, `"accept":null`, `"drop":null`, 1)
	body := `{"enabled":true,"ruleset":` + changed + `}`
	if w := request("PATCH", "/config?dry_run=true", body, view.Revision); w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	if cfg, _ := h.Applier.Load(); len(cfg.Ruleset) != 0 {
		t.Fatal("dry run stored native rules")
	}
	if w := request("PATCH", "/config", body, view.Revision); w.Code != http.StatusConflict {
		t.Fatal("missing confirmation gate", w.Code, w.Body.String())
	}
	if cfg, _ := h.Applier.Load(); len(cfg.Ruleset) != 0 {
		t.Fatal("unconfirmed edit stored")
	}
	if w := request("PATCH", "/config?confirm=true", body, view.Revision); w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	if cfg, _ := h.Applier.Load(); len(cfg.Ruleset) == 0 {
		t.Fatal("confirmed edit not persisted")
	}
	if w := request("PATCH", "/config?confirm=true", body, view.Revision); w.Code != http.StatusPreconditionFailed {
		t.Fatal("stale edit accepted", w.Code, w.Body.String())
	}
	t.Setenv("NFT_TEST_INVALID", "1")
	if w := request("PATCH", "/config?dry_run=true", body, ""); w.Code != http.StatusUnprocessableEntity {
		t.Fatal("invalid expression not reported as validation failure", w.Code, w.Body.String())
	}
}

func TestNativeCommandsPreserveLargeIntegerExpressions(t *testing.T) {
	raw := strings.Replace(nativeFixture, `{"accept":null}`, `{"quota":{"val":9007199254740993,"val_unit":"bytes"}}`, 1)
	commands, err := nativeCommands([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(commands)
	if err != nil || !bytes.Contains(out, []byte("9007199254740993")) {
		t.Fatal("native integer expression lost precision", string(out), err)
	}
}

func TestNativeReapplyRestoresSavedTableAfterRestart(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux kernel implementation")
	}
	batchPath := fakeNFT(t)
	t.Setenv("NFT_TEST_CURRENT", `{"nftables":[]}`)
	store := core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName)
	a := Applier{Store: store, Kernel: NewKernel()}
	c := Config{Enabled: true, Ruleset: json.RawMessage(nativeFixture)}
	if err := a.save(c); err != nil {
		t.Fatal(err)
	}
	p, steps, err := a.Reapply(context.Background())
	if err != nil || p.Empty || len(steps) != 1 || !steps[0].Done {
		t.Fatal("saved native table not restored", p, steps, err)
	}
	batch, err := os.ReadFile(batchPath)
	if err != nil || bytes.Contains(batch, []byte(`"delete"`)) || !bytes.Contains(batch, []byte(`"add"`)) {
		t.Fatal("bad startup transaction", string(batch), err)
	}
}

func TestNativeRevisionCounterGrowthAndRuleOrder(t *testing.T) {
	c := Config{Enabled: true}
	a, err := nativeRevision([]byte(nativeFixture), c)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := nativeRevision([]byte(strings.Replace(nativeFixture, `"packets":12`, `"packets":2000`, 1)), c)
	if a != b {
		t.Fatal("counter movement changed revision")
	}
	b, _ = nativeRevision([]byte(strings.Replace(nativeFixture, `"accept":null`, `"drop":null`, 1)), c)
	if a == b {
		t.Fatal("verdict edit did not change revision")
	}
	var doc map[string][]map[string]any
	if err := json.Unmarshal([]byte(nativeFixture), &doc); err != nil {
		t.Fatal(err)
	}
	second := map[string]any{"rule": map[string]any{"family": "inet", "table": TableName, "chain": "input", "expr": []any{map[string]any{"drop": nil}}}}
	doc["nftables"] = append(doc["nftables"], second)
	first, _ := json.Marshal(doc)
	doc["nftables"][2], doc["nftables"][3] = doc["nftables"][3], doc["nftables"][2]
	reordered, _ := json.Marshal(doc)
	x, _ := canonicalNative(first)
	y, _ := canonicalNative(reordered)
	if bytes.Equal(x, y) {
		t.Fatal("rule ordering hidden by normalization")
	}
}
