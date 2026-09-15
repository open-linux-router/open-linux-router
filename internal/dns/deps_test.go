package dns

import (
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The redirect is the only thing here that needs nft, so a router that has not
// asked for it must not be told to install nftables. That is the same rule the
// module-level split exists for, applied one level in: do not make somebody
// fetch a package for a feature they have not turned on.
func TestNftablesIsOnlyNeededWhenTheRedirectIs(t *testing.T) {
	var off Config
	for _, d := range Dependencies(off) {
		if d.Tool == "nft" {
			t.Error("asked for nftables with the redirect off")
		}
	}

	var on Config
	on.Hijack.Enabled = true
	var found bool
	for _, d := range Dependencies(on) {
		if d.Tool == "nft" {
			found = true
		}
	}
	if !found {
		t.Error("the redirect is on and nothing asked for nftables")
	}
}

// unbound is needed whatever else is configured — it is the half of DNS this
// module does not implement.
func TestUnboundIsAlwaysRequired(t *testing.T) {
	var cfg Config
	deps := Dependencies(cfg)
	if len(deps) == 0 || deps[0].Tool != "unbound" {
		t.Fatalf("got %+v; want unbound first", deps)
	}
	// Not inert, and the .deb must not carry it: installing it starts a
	// resolver on 127.0.0.1:53. internal/packaging's depends test enforces the
	// consequence; this pins the fact it reasons from.
	if deps[0].Inert {
		t.Error("unbound is marked inert, but installing it starts unbound.service on :53")
	}
}

// The two red panels are one problem, and this is the link that says so.
//
// Debian's unbound package starts unbound.service on 127.0.0.1:53, so an install
// that did not also stand that unit down would swap "unbound is not installed"
// for "unbound.service holds :53" and leave the operator exactly as stuck. The
// unit named here has to be one core knows how to clear, or the promise the
// button makes is one nothing can keep.
func TestUnboundDeclaresTheUnitItsInstallBringsUp(t *testing.T) {
	dep := Dependencies(Config{})[0]
	if len(dep.Shadows) == 0 {
		t.Fatal("unbound shadows nothing, so installing it summons the port conflict and stops")
	}
	for _, unit := range dep.Shadows {
		var known bool
		for _, b := range core.DistroBackends {
			if b.Unit == unit && b.Clearable() {
				known = true
			}
		}
		if !known {
			t.Errorf("unbound shadows %s, which core cannot stand down", unit)
		}
	}
}
