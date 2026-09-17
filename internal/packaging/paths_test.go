// Package packaging_test is external so it can import the module that renders
// the files these units name — internal/dns imports internal/packaging, so the
// only place both are visible is out here. Same reason as depends_test.go.
package packaging_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/dns"
	"github.com/open-linux-router/open-linux-router/internal/packaging"
)

// dropInName is packaging's name for the file, which is unexported. Restated
// rather than exported, because nothing outside `olr enable` should be writing
// one and this test is asserting on what it wrote.
const dropInName = "10-path.conf"

// The paths the resolver's unit names are the paths the DNS module renders.
//
// Three files carry that one answer — internal/dns.Paths, the embedded unit,
// and dropin.go's constants — and until this test nothing compared them. They
// fail quietly when they drift, and the failure lands on an operator: the unit
// starts the daemon against a config nobody wrote, or against no config at all,
// and the module's own plan stays happy because from where it stands the file
// it rendered is present and correct.
//
// The same shape as TestTheDebAndTheBinaryShipTheSameFiles: one answer, several
// copies of it, and a test that says when they stop agreeing. The paths
// themselves are not free to choose — Debian confines /usr/sbin/unbound to
// /etc/unbound and /var/lib/unbound — so internal/dns/confinement_test.go holds
// which side of that line each file is on, and this one holds the units to it.
//
// Asserted on directive lines rather than on the file's text, for the reason
// embed_test.go gives: this unit is heavily commented, and the comments name
// these same paths. A test that a prose edit can satisfy reports on the prose.
func TestUnitsNameThePathsTheDNSModuleRenders(t *testing.T) {
	paths := dns.DefaultPaths()
	unit := unitNamed(t, "olr-dns.service")

	// The config twice — checked, then run — and the anchor once.
	if !anyDirective(unit, "ExecStartPre=", "unbound-checkconf "+paths.UnboundConf) {
		t.Errorf("no ExecStartPre line checks %s:\n%s", paths.UnboundConf, execLines(unit))
	}
	if !anyDirective(unit, "ExecStart=", "-c "+paths.UnboundConf) {
		t.Errorf("no ExecStart line opens %s:\n%s", paths.UnboundConf, execLines(unit))
	}
	if !anyDirective(unit, "ExecStartPre=", "-a "+paths.TrustAnchor) {
		t.Errorf("no ExecStartPre line bootstraps the anchor at %s:\n%s", paths.TrustAnchor, execLines(unit))
	}

	// StateDirectory= is the same path twice over: relative to /var/lib, and
	// what creates the anchor's directory before unbound-anchor runs.
	wantState := strings.TrimPrefix(filepath.Dir(paths.TrustAnchor), "/var/lib/")
	if !anyDirective(unit, "StateDirectory=", wantState) {
		t.Errorf("no StateDirectory= line creates %s, the directory unbound-anchor writes "+
			"the anchor into:\n%s", filepath.Dir(paths.TrustAnchor), unit)
	}
}

// The drop-in is the other copy of the same paths: on a box whose unbound is
// not at Debian's path, these are the lines the daemon actually runs under.
func TestTheDropInNamesTheSamePaths(t *testing.T) {
	paths := dns.DefaultPaths()

	var body string
	for _, d := range packaging.DropIns(packaging.Tools{
		Unbound:          "/usr/bin/unbound",
		UnboundCheckconf: "/usr/bin/unbound-checkconf",
		UnboundAnchor:    "/usr/bin/unbound-anchor",
	}) {
		if strings.HasSuffix(d.Path, "olr-dns.service.d/"+dropInName) {
			body = string(d.Data)
		}
	}
	if body == "" {
		t.Fatal("no olr-dns drop-in written for a non-Debian unbound; this test would assert nothing")
	}

	if !anyDirective(body, "ExecStart=", "-c "+paths.UnboundConf) {
		t.Errorf("the olr-dns drop-in does not start the daemon on %s:\n%s", paths.UnboundConf, body)
	}
	if !anyDirective(body, "ExecStartPre=", "-a "+paths.TrustAnchor) {
		t.Errorf("the olr-dns drop-in does not bootstrap the anchor at %s:\n%s", paths.TrustAnchor, body)
	}
}

// anyDirective reports whether some line starting with prefix contains want.
func anyDirective(unit, prefix, want string) bool {
	for _, line := range strings.Split(unit, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}

// execLines is the directive lines, for a failure message that shows what the
// unit actually says rather than what it explains.
func execLines(unit string) string {
	var out []string
	for _, line := range strings.Split(unit, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func unitNamed(t *testing.T, name string) string {
	t.Helper()
	units, err := packaging.Units(packaging.PackagedOLR)
	if err != nil {
		t.Fatalf("Units: %v", err)
	}
	for _, u := range units {
		if u.Name == name {
			return string(u.Data)
		}
	}
	t.Fatalf("no unit named %s", name)
	return ""
}
