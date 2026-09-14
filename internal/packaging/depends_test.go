// Package packaging_test is external so it can import the modules, which
// import internal/packaging. The dependency list is declared by the modules and
// consumed by the .deb, so the only place both are visible is out here.
package packaging_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/dhcp"
	"github.com/open-linux-router/open-linux-router/internal/dns"
)

// nfpmPath is the package definition, relative to this test.
const nfpmPath = "../../packaging/nfpm.yaml"

// allDependencies is every dependency any module can ask for.
//
// dns is asked with the redirect on, because that is the maximal set and this
// test is about what the *package* should carry rather than what one box needs.
func allDependencies() []core.Dependency {
	var cfg dns.Config
	cfg.Hijack.Enabled = true

	var out []core.Dependency
	out = append(out, dhcp.Dependencies()...)
	out = append(out, dns.Dependencies(cfg)...)
	return out
}

// The .deb depends on exactly the inert dependencies — the ones whose
// installation puts a binary on the box and does nothing else.
//
// This is the rule that took unbound out of the list. Debian's unbound package
// enables and starts unbound.service on 127.0.0.1:53 as it installs, so
// depending on it gave every olr box a resolver it had not asked for, holding
// the port, which olr's own status page then reported as a conflict. The
// package was manufacturing the problem the product complains about.
//
// Asserted rather than remembered, because the failure is silent in both
// directions: adding a non-inert dependency quietly starts a daemon on every
// install, and dropping an inert one quietly moves a hard failure from apt to
// first use.
func TestPackageDependsOnExactlyTheInertDependencies(t *testing.T) {
	var want []string
	for _, d := range allDependencies() {
		pkg, ok := d.PackageFor(core.Distro{ID: "debian"})
		if !ok {
			t.Errorf("%s has no Debian package name, so the .deb cannot name it", d.Tool)
			continue
		}
		if d.Inert {
			want = append(want, pkg.Name)
		}
	}

	got := readDepends(t)
	sort.Strings(want)
	sort.Strings(got)

	if strings.Join(want, ",") != strings.Join(got, ",") {
		t.Errorf("packaging/nfpm.yaml depends:\n  got  %v\n  want %v\n"+
			"The .deb must declare every inert dependency and no others "+
			"(core.Dependency.Inert says why).", got, want)
	}
}

// Every dependency must be installable everywhere olr claims to run, and the
// tarball exists precisely for the distributions the .deb does not cover
// (.github/workflows/release.yml). A dependency with only a Debian name sends
// those operators an apt command they cannot run.
func TestEveryDependencyNamesEveryFamily(t *testing.T) {
	families := []string{"debian", "fedora", "rhel", "suse", "arch", "alpine"}
	for _, d := range allDependencies() {
		for _, family := range families {
			if _, ok := d.PackageFor(core.Distro{ID: family}); !ok {
				t.Errorf("%s has no package name for %s", d.Tool, family)
			}
		}
	}
}

// The Debian note about dnsmasq-base must not leak to distributions that do not
// split the package, where it is advice about a package that does not exist.
func TestDebianOnlyNotesStayOnDebian(t *testing.T) {
	for _, d := range allDependencies() {
		for _, pkg := range d.Packages {
			if pkg.Distro == "debian" {
				continue
			}
			if strings.Contains(pkg.Note, "-base") {
				t.Errorf("%s/%s repeats Debian's -base warning: %q", d.Tool, pkg.Distro, pkg.Note)
			}
		}
	}
}

// readDepends pulls the depends: list out of nfpm.yaml.
//
// Hand-parsed rather than with a YAML library: gopkg.in/yaml.v3 is an indirect
// dependency today, and promoting it to a direct one to read six lines in a
// test is a worse trade than twenty lines of scanning. The block is a flat list
// of scalars and a malformed one fails the assertion above loudly.
func readDepends(t *testing.T) []string {
	t.Helper()

	data, err := os.ReadFile(filepath.FromSlash(nfpmPath))
	if err != nil {
		t.Fatalf("reading %s: %v", nfpmPath, err)
	}

	var out []string
	inBlock := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "depends:") {
			inBlock = true
			continue
		}
		if !inBlock {
			continue
		}
		trimmed := strings.TrimSpace(line)
		// A comment or a blank line inside the block is still the block.
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Anything that is not an indented list item ends it.
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			break
		}
		if item, ok := strings.CutPrefix(trimmed, "- "); ok {
			out = append(out, strings.TrimSpace(item))
		}
	}

	if len(out) == 0 {
		t.Fatalf("found no depends: entries in %s; this test has stopped reading what it thinks it reads", nfpmPath)
	}
	return out
}
