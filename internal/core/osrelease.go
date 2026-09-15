package core

import (
	"bufio"
	"os"
	"strings"
)

// Which distribution this is, which olr needs for exactly one purpose: telling
// an operator how to install something.
//
// It exists because the tarball is the path for the distributions the .deb does
// not cover (packaging/nfpm.yaml, .github/workflows/release.yml both say so) —
// and until now that path gave Debian-only advice. A Fedora operator missing
// unbound was told `sudo apt install dnsmasq-base`, in which both the command
// and the package name are wrong for them.
//
// Deliberately not used for anything else. Branching behaviour on the
// distribution is how a tool stops being an ordinary Linux program (design.md
// §3.4); this only changes the sentence we print.

// osReleasePath is a variable so tests can point at a fixture.
var osReleasePath = "/etc/os-release"

// Distro identifies the running distribution, as loosely as os-release allows.
type Distro struct {
	// ID is os-release's ID: "debian", "ubuntu", "fedora", "arch".
	ID string

	// Like is ID_LIKE, the families this distribution claims descent from.
	// Ubuntu says "debian", Rocky says "rhel centos fedora". It is what makes
	// one entry cover a family rather than requiring a row per derivative.
	Like []string

	// Name is PRETTY_NAME, for saying which box we think this is when we get it
	// wrong.
	Name string
}

// DetectDistro reads /etc/os-release.
//
// A box without the file — or with an unreadable one — is reported as the zero
// Distro rather than as an error. Every caller degrades to generic advice, and
// none of them should fail because a file that is not part of any standard is
// missing.
func DetectDistro() Distro {
	f, err := os.Open(osReleasePath)
	if err != nil {
		return Distro{}
	}
	defer f.Close()

	var d Distro
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if !ok {
			continue
		}
		// Values may be quoted, and ID_LIKE usually is because it holds spaces.
		value = strings.Trim(value, `"'`)
		switch key {
		case "ID":
			d.ID = value
		case "ID_LIKE":
			d.Like = strings.Fields(value)
		case "PRETTY_NAME":
			d.Name = value
		}
	}
	return d
}

// Is reports whether this distribution is the named one, or descends from it.
//
// ID first, then ID_LIKE, so Ubuntu matches both "ubuntu" and "debian" and a
// caller can write one entry for the family and override it for a member.
func (d Distro) Is(id string) bool {
	if d.ID == id {
		return true
	}
	for _, like := range d.Like {
		if like == id {
			return true
		}
	}
	return false
}

// installCommands is how each family installs a package.
//
// Keyed by the same ids Is matches, so "debian" covers Ubuntu, Mint and
// Raspberry Pi OS through ID_LIKE without a row each.
var installCommands = map[string]string{
	"debian": "sudo apt install %s",
	"fedora": "sudo dnf install %s",
	"rhel":   "sudo dnf install %s",
	"arch":   "sudo pacman -S %s",
	"alpine": "sudo apk add %s",
	"suse":   "sudo zypper install %s",
}

// installOrder is the order families are tried, because ID_LIKE can name
// several. Longest-established package manager first is not the rule; the rule
// is that the more specific family wins, and these are listed most specific
// first so "rhel fedora" picks rhel's dnf and not fedora's identical one.
var installOrder = []string{"debian", "rhel", "fedora", "suse", "arch", "alpine"}

// installArgv is how olr installs a package itself, as argv rather than as a
// sentence.
//
// A second table beside installCommands rather than a parse of it, because the
// two are not the same command and pretending they were would be the bug. What
// an operator types is interactive and elevated; what olr runs is already root
// and has nobody to answer a prompt, so each of these carries its family's way
// of saying "assume yes" — and Debian's says apt-get, because `apt` prints
// "does not have a stable CLI interface" the moment it is scripted.
//
// A test asserts the two tables cover exactly the same families, so neither can
// grow an entry the other lacks and leave olr recommending one thing and doing
// another.
//
// DPkg::Lock::Timeout on Debian is not a detail. A fresh box is usually running
// unattended-upgrades, which holds the dpkg lock for minutes at a time, and it
// is far and away the most likely reason this fails at all. Waiting for it beats
// failing on it, and failing on it in dpkg's own words beats failing in ours.
var installArgv = map[string][]string{
	"debian": {"apt-get", "install", "-y", "-o", "DPkg::Lock::Timeout=60"},
	"fedora": {"dnf", "install", "-y"},
	"rhel":   {"dnf", "install", "-y"},
	"arch":   {"pacman", "-S", "--noconfirm"},
	// apk needs no flag: it is non-interactive already, and adding one that
	// does not exist would fail every install on Alpine.
	"alpine": {"apk", "add"},
	"suse":   {"zypper", "--non-interactive", "install"},
}
