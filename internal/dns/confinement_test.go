package dns

import (
	"path/filepath"
	"strings"
	"testing"
)

// What the distribution lets the resolver open.
//
// Debian's unbound package ships an AppArmor profile for /usr/sbin/unbound —
// Ubuntu inherits it — and it grants exactly this, taken verbatim from
// /etc/apparmor.d/usr.sbin.unbound in unbound 1.22.0-2+deb13u3:
//
//	profile unbound /usr/sbin/unbound flags=(attach_disconnected) {
//	  # non-chrooted paths
//	  /etc/unbound/** r,
//	  owner /etc/unbound/*.key* rw,
//	  ...
//	  /{,etc/unbound/}var/lib/unbound/** r,
//	  owner /{,etc/unbound/}var/lib/unbound/** rw,
//	  ...
//	}
//
// Which is the whole of the lesson: design.md §7's "generated files are
// additive" is only true of a path the profile already allows. Everything a
// confined daemon opens has to be inside one of those two trees, and the
// failure when it is not is expensive out of all proportion to the mistake —
// unbound exits 1 the moment it is exec'd ("Could not open …: Permission
// denied"), the unit loops in activating (auto-restart) forever, and
// unbound-checkconf, which nothing confines, calls the same file valid and
// exits 0. Two satisfied checks and a dead resolver.
//
// This test is the same kind of line as internal/packaging's
// TestNoUnitConfinesItself: it asserts a decision that is invisible in the files
// it governs, so that the next person to move these paths — reasonably, in
// isolation, because /etc/open-linux-router is obviously ours — gets told here
// instead of by an operator whose DNS is down.
const (
	debianReadRoot  = "/etc/unbound"
	debianStateRoot = "/var/lib/unbound"
)

// Every path the daemon itself opens is inside those trees, and inside a
// directory of ours within them.
func TestUnboundOpensNothingOutsideItsConfinement(t *testing.T) {
	p := DefaultPaths()

	// Read. /etc/unbound/** r is the only read grant the profile makes.
	if !underRoot(p.UnboundConf, debianReadRoot) {
		t.Errorf("unbound.conf is rendered at %s, outside %s — on Debian the confined "+
			"daemon cannot open it there, whatever unbound-checkconf says about it",
			p.UnboundConf, debianReadRoot)
	}

	// Write. unbound rewrites the trust anchor as the root KSK rolls, and
	// /etc/unbound/** is read-only apart from a top-level *.key*, so the state
	// tree is the only place that will do.
	if !underRoot(p.TrustAnchor, debianStateRoot) {
		t.Errorf("the trust anchor is at %s, outside %s — unbound rewrites it when the "+
			"root key rolls, and the profile allows no write there", p.TrustAnchor, debianStateRoot)
	}

	// A directory of ours inside each tree, never the tree itself. Applier.observe
	// walks the directories it renders into and plans a delete for every file it
	// finds there that the current config does not produce, so rendering into
	// /etc/unbound would put the distribution's own unbound.conf on that list —
	// and /etc/unbound/unbound.conf.d is included by the distribution's own
	// resolver, which would then be reading our config.
	for _, tc := range []struct{ path, root string }{
		{p.UnboundConf, debianReadRoot},
		{p.TrustAnchor, debianStateRoot},
	} {
		if !strings.HasPrefix(tc.path, filepath.Join(tc.root, "open-linux-router")+"/") {
			t.Errorf("%s is not inside %s/open-linux-router — see Paths for why each tree "+
				"holds a directory of ours rather than the file itself",
				tc.path, tc.root)
		}
	}
}

// The other half of the split: what olr's own binaries read stays in olr's own
// directory, where nothing can be confined out from under it.
func TestOnlyTheResolversFilesLeaveOlrDirectory(t *testing.T) {
	p := DefaultPaths()

	for _, path := range []string{p.RelayConf, p.PolicyDir, p.HijackNFT} {
		if !underRoot(path, "/etc/open-linux-router") {
			t.Errorf("%s is outside /etc/open-linux-router, and nothing confines the relay "+
				"that reads it — this one only moves for a reason", path)
		}
	}
	if !underRoot(p.ObserveSocket, "/run/olr") {
		t.Errorf("the observe socket moved to %s; the relay's unit owns /run/olr/dns and "+
			"systemd removes a RuntimeDirectory when that unit stops", p.ObserveSocket)
	}
}

// Development renders the same layout under a scratch root, so the shape a
// scratch box exercises is the shape a real one has. A path that only exists on
// the real box is a path no test ever renders into.
func TestRootedPathsAreTheRealLayoutUnderARoot(t *testing.T) {
	root := t.TempDir()
	real, rooted := DefaultPaths(), RootedPaths(root)

	for _, tc := range []struct{ name, real, rooted string }{
		{"UnboundConf", real.UnboundConf, rooted.UnboundConf},
		{"RelayConf", real.RelayConf, rooted.RelayConf},
		{"PolicyDir", real.PolicyDir, rooted.PolicyDir},
		{"HijackNFT", real.HijackNFT, rooted.HijackNFT},
		{"TrustAnchor", real.TrustAnchor, rooted.TrustAnchor},
		{"ObserveSocket", real.ObserveSocket, rooted.ObserveSocket},
	} {
		if want := filepath.Join(root, tc.real); tc.rooted != want {
			t.Errorf("RootedPaths(root).%s = %s, want %s", tc.name, tc.rooted, want)
		}
	}
}

// underRoot reports whether path is inside dir — not dir itself, and not a
// sibling whose name merely starts with it.
func underRoot(path, dir string) bool {
	return strings.HasPrefix(path, dir+"/")
}
