package packaging

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The whole point of this package: one copy of each file, two consumers.
// packaging/nfpm.yaml names them for the .deb, the embed reads them for
// `olr enable`. A unit added to one and forgotten in the other is drift that
// nobody sees until a box is missing a service months later, so it fails here
// instead.
func TestTheDebAndTheBinaryShipTheSameFiles(t *testing.T) {
	raw, err := os.ReadFile("../../packaging/nfpm.yaml")
	if err != nil {
		t.Fatalf("reading nfpm.yaml: %v", err)
	}

	// Deliberately a regexp over the text rather than a YAML parse: this repo
	// has no YAML dependency and adding one to assert on five lines would be a
	// worse trade than a pattern that fails loudly if the file's shape changes.
	src := regexp.MustCompile(`(?m)^\s*-\s*src:\s*(internal/packaging/\S+)`)
	var packaged []string
	for _, m := range src.FindAllStringSubmatch(string(raw), -1) {
		packaged = append(packaged, strings.TrimPrefix(m[1], "internal/packaging/"))
	}
	if len(packaged) == 0 {
		t.Fatal("nfpm.yaml references no files from this package; if the .deb stopped shipping them, this test is what should have told you")
	}

	// nfpm resolves these relative to the repository root, and it only ever
	// runs on a tag — so a path that has moved fails the release rather than
	// the build that moved it. Checking the files are actually there is the
	// cheapest stand-in for running nfpm itself.
	for _, rel := range packaged {
		if _, err := os.Stat(rel); err != nil {
			t.Errorf("nfpm.yaml names internal/packaging/%s, which is not there: %v", rel, err)
		}
	}

	units, err := Units()
	if err != nil {
		t.Fatalf("Units: %v", err)
	}
	embedded := []string{EnvName}
	for _, u := range units {
		embedded = append(embedded, "systemd/"+u.Name)
	}

	sort.Strings(packaged)
	sort.Strings(embedded)
	if strings.Join(packaged, "\n") != strings.Join(embedded, "\n") {
		t.Errorf("the .deb and the binary disagree about what an installation needs\n"+
			"--- nfpm.yaml ---\n%s\n--- embedded ---\n%s",
			strings.Join(packaged, "\n"), strings.Join(embedded, "\n"))
	}
}

// olrd.service is what `olr enable` enables, so Units must lead with it — the
// backends are enabled by their own modules, not here (design.md §3.4).
func TestUnitsLeadWithTheControlPlane(t *testing.T) {
	units, err := Units()
	if err != nil {
		t.Fatalf("Units: %v", err)
	}
	if len(units) == 0 {
		t.Fatal("no units embedded")
	}
	if units[0].Name != PrimaryUnit {
		t.Errorf("Units()[0] = %q, want %q", units[0].Name, PrimaryUnit)
	}
}

func TestEveryEmbeddedUnitHasContent(t *testing.T) {
	units, err := Units()
	if err != nil {
		t.Fatalf("Units: %v", err)
	}
	for _, u := range units {
		if !strings.Contains(string(u.Data), "[Service]") {
			t.Errorf("%s does not look like a unit file", u.Name)
		}
	}
	env, err := Env()
	if err != nil {
		t.Fatalf("Env: %v", err)
	}
	if len(env) == 0 {
		t.Error("olrd.env is empty")
	}
}
