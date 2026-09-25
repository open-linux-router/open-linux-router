package dns

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/dnsrelay"
)

// staticPublished is a PublishedView backed by a literal list.
type staticPublished []string

func (s staticPublished) Published() ([]string, error) { return s, nil }

type brokenPublished struct{}

func (brokenPublished) Published() ([]string, error) { return nil, errors.New("store unreadable") }

func renderWith(t *testing.T, b Backend, c Config, published PublishedView) Rendered {
	t.Helper()
	out, err := b.Render(c, testLinks(), published)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return out
}

func observedWith(t *testing.T, b Backend, c Config, published PublishedView) Observed {
	t.Helper()
	obs := observedFor(t, b, c, true)
	obs.Files = map[string][]byte{}
	for _, f := range renderWith(t, b, c, published).Files {
		obs.Files[f.Path] = f.Data
	}
	return obs
}

// Published names are qualified under the local domain, canonical, sorted and
// unique — the relay matches them without normalising on the hot path.
func TestRenderQualifiesPublishedNames(t *testing.T) {
	b := testBackend(t)
	cfg := validConfig()
	cfg.LocalDomain = "home.example.com"

	out := renderWith(t, b, cfg, staticPublished{"wiki", "NAS", "nas", " grafana. "})
	f, ok := out.Get(b.Paths.Published)
	if !ok {
		t.Fatalf("no %s rendered", b.Paths.Published)
	}
	got, err := dnsrelay.UnmarshalPublished(f.Data)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"grafana.home.example.com", "nas.home.example.com", "wiki.home.example.com"}
	if !slices.Equal(got.Names, want) {
		t.Errorf("names = %v, want %v", got.Names, want)
	}
	if !f.Reloadable || f.Unit != b.RelayUnit() {
		t.Errorf("reloadable %v, unit %q: publishing must be a relay SIGHUP, nothing more", f.Reloadable, f.Unit)
	}

	relay, _ := out.Get(b.Paths.RelayConf)
	if !strings.Contains(string(relay.Data), b.Paths.Published) {
		t.Errorf("relay.json does not point the relay at %s:\n%s", b.Paths.Published, relay.Data)
	}
}

// No names, no file: a box that never used ingress carries nothing for it.
func TestRenderWritesNoPublishedFileWhenNothingIsPublished(t *testing.T) {
	b := testBackend(t)
	for _, published := range []PublishedView{nil, staticPublished{}} {
		if _, ok := renderWith(t, b, validConfig(), published).Get(b.Paths.Published); ok {
			t.Errorf("%T: rendered a published-names file with nothing in it", published)
		}
	}
}

// A failed read is an error, not "nothing published": rendering an empty set
// would delete every published name from DNS because a file could not be read.
func TestRenderRefusesWhenPublishedNamesCannotBeRead(t *testing.T) {
	if _, err := testBackend(t).Render(validConfig(), testLinks(), brokenPublished{}); err == nil {
		t.Fatal("rendered as if nothing were published")
	}
}

// The property the whole design rests on: publishing a service reloads the relay
// and leaves the resolver alone.
func TestPlanPublishingANameOnlyReloadsTheRelay(t *testing.T) {
	b := testBackend(t)
	cfg := validConfig()
	obs := observedWith(t, b, cfg, staticPublished{"nas"})

	plan, err := BuildPlan(b, cfg, testLinks(), nil, staticPublished{"nas", "wiki"}, obs, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := actionFor(plan, b.RelayUnit()); got != ActionReload {
		t.Errorf("relay action = %s, want reload", got)
	}
	if got := actionFor(plan, b.ResolverUnit()); got != ActionNone {
		t.Errorf("resolver action = %s, want none", got)
	}
}

// Unpublishing the last name deletes the file, and the relay still only reloads.
func TestPlanUnpublishingTheLastNameDeletesTheFile(t *testing.T) {
	b := testBackend(t)
	cfg := validConfig()
	obs := observedWith(t, b, cfg, staticPublished{"nas"})

	plan, err := BuildPlan(b, cfg, testLinks(), nil, staticPublished{}, obs, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var deleted bool
	for _, c := range plan.Changes {
		if c.Path == b.Paths.Published && c.Kind == ChangeDelete {
			deleted = true
		}
	}
	if !deleted {
		t.Errorf("no delete planned for %s: %+v", b.Paths.Published, plan.Changes)
	}
	if got := actionFor(plan, b.RelayUnit()); got != ActionReload {
		t.Errorf("relay action = %s, want reload", got)
	}
}
