package packaging

import (
	"strings"
	"testing"
)

// The regression, stated as the box sees it: `olr enable` installs the binary
// to /usr/local/bin, the shipped units say /usr/bin, and systemd then execs a
// path nothing wrote. Every tarball install failed this way, and the .deb path
// hid it because dpkg puts the binary exactly where the unit says.
func TestUnitsPointAtTheBinaryThatWillExist(t *testing.T) {
	units, err := Units("/usr/local/bin/olr")
	if err != nil {
		t.Fatalf("Units: %v", err)
	}

	found := 0
	for _, u := range units {
		for _, line := range strings.Split(string(u.Data), "\n") {
			if !strings.HasPrefix(line, "Exec") {
				continue
			}
			// The packaged path must not survive anywhere systemd will run it.
			if strings.Contains(line, PackagedOLR+" ") || strings.HasSuffix(line, PackagedOLR) {
				t.Errorf("%s still execs the packaged path:\n  %s", u.Name, line)
			}
			if strings.Contains(line, "/usr/local/bin/olr") {
				found++
			}
		}
	}
	// olrd.service and olr-dnsd.service. A zero here would mean the rewrite
	// silently matched nothing, which is the failure mode worth guarding.
	if found != 2 {
		t.Errorf("rewrote %d Exec lines, want 2 (olrd.service and olr-dnsd.service)", found)
	}
}

// The .deb path must stay byte-identical: nfpm ships these files verbatim, so a
// rewrite that fired for the packaged path would put a different unit in the
// package than the one in the repository.
func TestUnitsAreUntouchedForThePackagedPath(t *testing.T) {
	for _, olrPath := range []string{PackagedOLR, ""} {
		rendered, err := Units(olrPath)
		if err != nil {
			t.Fatalf("Units(%q): %v", olrPath, err)
		}
		for _, u := range rendered {
			original, err := files.ReadFile("systemd/" + u.Name)
			if err != nil {
				t.Fatal(err)
			}
			if string(u.Data) != string(original) {
				t.Errorf("Units(%q) modified %s; the shipped file must go out verbatim", olrPath, u.Name)
			}
		}
	}
}

// PackagedOLR is what the rewrite matches on, so a unit edited to name a
// different path would make the rewrite quietly stop working. This is the test
// that fails instead.
func TestTheShippedUnitsNameThePackagedPath(t *testing.T) {
	units, err := Units(PackagedOLR)
	if err != nil {
		t.Fatalf("Units: %v", err)
	}

	naming := map[string]bool{}
	for _, u := range units {
		for _, line := range strings.Split(string(u.Data), "\n") {
			key, value, ok := strings.Cut(line, "=")
			if !ok || !strings.HasPrefix(key, "Exec") {
				continue
			}
			word := strings.TrimLeft(value, "+-!:@")
			if i := strings.Index(word, " "); i >= 0 {
				word = word[:i]
			}
			if strings.HasSuffix(word, "/olr") {
				naming[u.Name] = true
				if word != PackagedOLR {
					t.Errorf("%s execs %q; PackagedOLR is %q and the rewrite matches on it",
						u.Name, word, PackagedOLR)
				}
			}
		}
	}
	for _, want := range []string{"olrd.service", "olr-dnsd.service"} {
		if !naming[want] {
			t.Errorf("%s no longer execs the olr binary; if that is deliberate, "+
				"update this test — if not, it would have shipped broken", want)
		}
	}
}

func TestRetargetOLR(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{
			"plain ExecStart",
			"ExecStart=/usr/bin/olr internal daemon $OLRD_ARGS",
			"ExecStart=/opt/olr internal daemon $OLRD_ARGS",
		},
		{
			"no arguments",
			"ExecStart=/usr/bin/olr",
			"ExecStart=/opt/olr",
		},
		{
			// systemd's prefixes have to survive: dropping the `+` on an
			// ExecStartPost would run it inside the sandbox instead of as root.
			"keeps a systemd prefix",
			"ExecStartPost=+-/usr/bin/olr something",
			"ExecStartPost=+-/opt/olr something",
		},
		{
			// The comments in these units explain the packaged layout. Rewriting
			// them would make the file describe a machine it is not on.
			"leaves comments alone",
			"# /usr/bin/olr is where the .deb puts it",
			"# /usr/bin/olr is where the .deb puts it",
		},
		{
			"leaves other binaries alone",
			"ExecStopPost=+-/usr/sbin/nft delete table inet olr-dns",
			"ExecStopPost=+-/usr/sbin/nft delete table inet olr-dns",
		},
		{
			// A path that merely starts with the same characters is a different
			// binary, and a blind string replace would mangle it.
			"does not match a longer path",
			"ExecStart=/usr/bin/olrctl run",
			"ExecStart=/usr/bin/olrctl run",
		},
		{
			"leaves non-Exec directives alone",
			"Documentation=/usr/bin/olr",
			"Documentation=/usr/bin/olr",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := string(retargetOLR([]byte(tc.in), "/opt/olr"))
			if got != tc.want {
				t.Errorf("retargetOLR()\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

// Rewriting must not disturb anything else in the file — a unit is 80 lines of
// sandbox directives that a careless line-rewrite could reorder or drop.
func TestRetargetOLRPreservesEverythingElse(t *testing.T) {
	original, err := files.ReadFile("systemd/olrd.service")
	if err != nil {
		t.Fatal(err)
	}
	rewritten := retargetOLR(original, "/usr/local/bin/olr")

	before := strings.Split(string(original), "\n")
	after := strings.Split(string(rewritten), "\n")
	if len(before) != len(after) {
		t.Fatalf("line count changed: %d -> %d", len(before), len(after))
	}
	changed := 0
	for i := range before {
		if before[i] != after[i] {
			changed++
		}
	}
	if changed != 1 {
		t.Errorf("%d lines changed, want exactly 1 (the ExecStart)", changed)
	}
}
