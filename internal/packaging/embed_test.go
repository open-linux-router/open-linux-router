package packaging

import (
	"os"
	"regexp"
	"slices"
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

	units, err := Units(PackagedOLR)
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
	units, err := Units(PackagedOLR)
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

// confinement is every directive that narrows what a unit may do or who it runs
// as. design.md §3.5 "Privileges" says olr ships none of them today.
//
// Listed rather than pattern-matched, because a prefix rule would catch every
// Protect* and miss DynamicUser=, and a rule loose enough to catch both would
// also catch StateDirectory= — which stays, and which is not confinement.
var confinement = []string{
	"AmbientCapabilities",
	"CapabilityBoundingSet",
	"DevicePolicy",
	"DynamicUser",
	"Group",
	"IPAddressDeny",
	"InaccessiblePaths",
	"LockPersonality",
	"MemoryDenyWriteExecute",
	"NoNewPrivileges",
	"PrivateDevices",
	"PrivateNetwork",
	"PrivateTmp",
	"PrivateUsers",
	"ProcSubset",
	"ProtectControlGroups",
	"ProtectHome",
	"ProtectKernelModules",
	"ProtectKernelTunables",
	"ProtectProc",
	"ProtectSystem",
	"ReadOnlyPaths",
	"ReadWritePaths",
	"RestrictAddressFamilies",
	"RestrictNamespaces",
	"RestrictRealtime",
	"RestrictSUIDSGID",
	"SystemCallFilter",
	"User",
}

// No unit confines itself. Every one runs as root with what systemd gives a
// system service by default.
//
// This asserts a decision rather than catching a bug, and it is here because
// the decision is invisible in the files it governs: a unit with no
// ProtectSystem= looks exactly like a unit where somebody forgot one. The next
// person to add a single directive to a single unit — reasonably, in isolation,
// because that unit obviously has no business reading /home — gets told here
// instead of by an operator whose DNS is down.
//
// What it stands in the way of, concretely. olr-dns.service shipped
// `RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX`, and unbound resolves its
// `interface:` lines through getifaddrs(), which opens a netlink socket even
// when every interface named is a literal address. EAFNOSUPPORT, "could not
// resolve interface names", an auto-restart loop, a rendered config that was
// never wrong, and nothing in the error pointing at the unit file.
//
// Hardening is worth having, and this is not an argument that it is not. It is
// an argument that it is worth doing deliberately and testably, in one pass,
// rather than one remembered directive at a time — so when it comes back, it
// comes back through here.
func TestNoUnitConfinesItself(t *testing.T) {
	units, err := Units(PackagedOLR)
	if err != nil {
		t.Fatalf("Units: %v", err)
	}
	if len(units) == 0 {
		t.Fatal("no units embedded")
	}

	for _, u := range units {
		for _, directive := range directivesIn(string(u.Data)) {
			if slices.Contains(confinement, directive) {
				t.Errorf("%s sets %s=; design.md §3.5 \"Privileges\" says olr's units run "+
					"as root with no sandbox until hardening is done as one deliberate pass. "+
					"Change the decision there first, and this test with it.", u.Name, directive)
			}
		}
	}
}

// directivesIn returns the directive names a unit actually sets — the text to
// the left of the first `=` on a line that is not a comment.
//
// Parsed rather than searched for by name, which is the whole difference
// between this test and its first draft. That one looked for "AF_NETLINK"
// anywhere in the file and passed against the very unit it was written for,
// because the comment explaining the directive contained the word too. A test a
// prose edit can satisfy is worse than no test: it reports on itself.
func directivesIn(unit string) []string {
	var out []string
	for _, line := range strings.Split(unit, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		name, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out = append(out, strings.TrimSpace(name))
	}
	return out
}

func TestEveryEmbeddedUnitHasContent(t *testing.T) {
	units, err := Units(PackagedOLR)
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
