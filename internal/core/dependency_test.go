package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withOSRelease points DetectDistro at a fixture.
func withOSRelease(t *testing.T, contents string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "os-release")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := osReleasePath
	t.Cleanup(func() { osReleasePath = prev })
	osReleasePath = path
}

const debianOSRelease = `PRETTY_NAME="Debian GNU/Linux 13 (trixie)"
NAME="Debian GNU/Linux"
ID=debian
HOME_URL="https://www.debian.org/"
`

// Ubuntu is the case ID_LIKE exists for: one "debian" entry has to cover it,
// or every derivative needs a row of its own and the ones nobody thought of
// get nothing.
const ubuntuOSRelease = `PRETTY_NAME="Ubuntu 24.04.1 LTS"
ID=ubuntu
ID_LIKE=debian
`

const fedoraOSRelease = `PRETTY_NAME="Fedora Linux 41"
ID=fedora
`

func TestDetectDistroReadsIDAndFamily(t *testing.T) {
	withOSRelease(t, ubuntuOSRelease)

	d := DetectDistro()
	if d.ID != "ubuntu" {
		t.Errorf("ID = %q; want ubuntu", d.ID)
	}
	if !d.Is("ubuntu") {
		t.Error("Ubuntu does not match its own ID")
	}
	if !d.Is("debian") {
		t.Error("Ubuntu does not match debian through ID_LIKE, so every Debian entry misses it")
	}
	if d.Is("fedora") {
		t.Error("Ubuntu matched fedora")
	}
}

// A box with no os-release is reported as unknown, not as an error. The file is
// not part of any standard olr can rely on, and no caller should fail because
// it is absent.
func TestMissingOSReleaseIsUnknownNotAnError(t *testing.T) {
	prev := osReleasePath
	t.Cleanup(func() { osReleasePath = prev })
	osReleasePath = filepath.Join(t.TempDir(), "does-not-exist")

	if d := DetectDistro(); d.ID != "" || len(d.Like) != 0 {
		t.Errorf("got %+v; want the zero Distro", d)
	}
}

var testDep = Dependency{
	Tool: "unbound",
	Why:  "olr does not resolve names itself.",
	Packages: []Package{
		{Distro: "debian", Name: "unbound-debian", Note: "the -base warning"},
		{Distro: "fedora", Name: "unbound-fedora"},
	},
}

// The whole reason this exists: the tarball is the path for distributions the
// .deb does not cover, so it must not hand out apt commands to people who have
// no apt.
func TestInstallAdviceIsDistroCorrect(t *testing.T) {
	for _, tc := range []struct {
		name       string
		osRelease  string
		wantSubstr string
	}{
		{"debian", debianOSRelease, "sudo apt install unbound-debian"},
		{"ubuntu via ID_LIKE", ubuntuOSRelease, "sudo apt install unbound-debian"},
		{"fedora", fedoraOSRelease, "sudo dnf install unbound-fedora"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withOSRelease(t, tc.osRelease)
			got := installAdvice(testDep, DetectDistro())
			if !strings.Contains(got, tc.wantSubstr) {
				t.Errorf("advice = %q; want it to contain %q", got, tc.wantSubstr)
			}
		})
	}
}

// An unrecognised distribution is told what to install and not how. Guessing a
// package manager is worse than admitting we do not know: the operator can
// translate "install unbound" instantly, and cannot un-run a wrong command.
func TestUnknownDistroGetsGenericAdviceNotAGuess(t *testing.T) {
	withOSRelease(t, "ID=plan9\n")

	got := installAdvice(testDep, DetectDistro())
	for _, forbidden := range []string{"apt", "dnf", "pacman", "apk", "zypper"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("guessed %s on an unknown distribution: %q", forbidden, got)
		}
	}
	if !strings.Contains(got, "unbound") {
		t.Errorf("advice does not name the tool: %q", got)
	}
}

// Notes are per-distribution because the reasons are. Debian's dnsmasq-base
// warning is meaningless on a distribution that does not split the package,
// and printing it there would send somebody looking for a package that does
// not exist.
func TestNotesAreScopedToTheirDistro(t *testing.T) {
	withOSRelease(t, debianOSRelease)
	if got := installAdvice(testDep, DetectDistro()); !strings.Contains(got, "the -base warning") {
		t.Errorf("Debian lost its note: %q", got)
	}

	withOSRelease(t, fedoraOSRelease)
	if got := installAdvice(testDep, DetectDistro()); strings.Contains(got, "the -base warning") {
		t.Errorf("Debian's note leaked to Fedora: %q", got)
	}
}

// A tool that is present produces no blocker, which is the ordinary case and
// the one that must stay silent.
func TestInstalledToolIsNotABlocker(t *testing.T) {
	present := Dependency{Tool: "sh", Why: "every box has one."}
	if got := DependencyBlockers([]Dependency{present}); len(got) != 0 {
		t.Fatalf("got %+v; want nothing for an installed tool", got)
	}
}

func TestMissingToolBecomesABlockerCarryingTheFix(t *testing.T) {
	withOSRelease(t, debianOSRelease)
	missing := Dependency{
		Tool:     "olr-no-such-backend",
		Why:      "olr does not implement it.",
		Packages: []Package{{Distro: "debian", Name: "the-package"}},
	}

	got := DependencyBlockers([]Dependency{missing})
	if len(got) != 1 {
		t.Fatalf("got %d blockers, want 1: %+v", len(got), got)
	}
	if got[0].Kind != BlockerMissingDependency {
		t.Errorf("kind = %q", got[0].Kind)
	}
	if !strings.Contains(got[0].Fix, "sudo apt install the-package") {
		t.Errorf("fix does not carry the command: %q", got[0].Fix)
	}
	if !strings.Contains(got[0].Summary, "olr-no-such-backend") {
		t.Errorf("summary does not name the tool: %q", got[0].Summary)
	}
}
