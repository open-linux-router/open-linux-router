package packaging

import (
	"strings"
	"testing"
)

// Debian and Ubuntu are the packaged targets, so the shipped units already
// name the right paths there and correcting them would be noise.
func TestNoDropInsWhenEverythingIsWhereTheUnitsExpect(t *testing.T) {
	got := DropIns(Tools{
		Dnsmasq:          DebianDnsmasq,
		Unbound:          DebianUnbound,
		UnboundCheckconf: "/usr/sbin/unbound-checkconf",
		UnboundAnchor:    "/usr/sbin/unbound-anchor",
		Nft:              DebianNft,
	})
	if len(got) != 0 {
		t.Fatalf("got %d drop-ins on a Debian layout, want none: %+v", len(got), got)
	}
}

// Arch and Alpine put all three in /usr/bin.
func TestDropInsCorrectEveryPathOnANonDebianLayout(t *testing.T) {
	got := DropIns(Tools{
		Dnsmasq:          "/usr/bin/dnsmasq",
		Unbound:          "/usr/bin/unbound",
		UnboundCheckconf: "/usr/bin/unbound-checkconf",
		UnboundAnchor:    "/usr/bin/unbound-anchor",
		Nft:              "/usr/bin/nft",
	})
	if len(got) != 3 {
		t.Fatalf("got %d drop-ins, want 3: %+v", len(got), got)
	}

	want := map[string]string{
		"/etc/systemd/system/olr-dhcp.service.d/10-path.conf":  "/usr/bin/dnsmasq",
		"/etc/systemd/system/olr-dns.service.d/10-path.conf":   "/usr/bin/unbound",
		"/etc/systemd/system/olr-dnsd.service.d/10-path.conf":  "/usr/bin/nft",
	}
	for _, d := range got {
		bin, ok := want[d.Path]
		if !ok {
			t.Errorf("unexpected drop-in at %s", d.Path)
			continue
		}
		if !strings.Contains(string(d.Data), bin) {
			t.Errorf("%s does not mention %s:\n%s", d.Path, bin, d.Data)
		}
		if !strings.HasSuffix(d.Dir, ".service.d") {
			t.Errorf("%s is not a .service.d directory", d.Dir)
		}
		delete(want, d.Path)
	}
	for path := range want {
		t.Errorf("no drop-in written for %s", path)
	}
}

// The rule that makes drop-ins work at all: assigning to an Exec* directive
// appends to a list, so a drop-in that sets ExecStart without first clearing it
// leaves the unit running both binaries. Every Exec* this package emits must be
// preceded by its own empty assignment.
func TestEveryExecDirectiveIsClearedBeforeItIsSet(t *testing.T) {
	for _, d := range DropIns(Tools{
		Dnsmasq:          "/usr/bin/dnsmasq",
		Unbound:          "/usr/bin/unbound",
		UnboundCheckconf: "/usr/bin/unbound-checkconf",
		UnboundAnchor:    "/usr/bin/unbound-anchor",
		Nft:              "/usr/bin/nft",
	}) {
		cleared := map[string]bool{}
		for _, line := range strings.Split(string(d.Data), "\n") {
			name, value, ok := strings.Cut(line, "=")
			if !ok || !strings.HasPrefix(name, "Exec") {
				continue
			}
			if value == "" {
				cleared[name] = true
				continue
			}
			if !cleared[name] {
				t.Errorf("%s sets %s before clearing it:\n%s", d.Path, name, d.Data)
			}
		}
	}
}

// unbound-anchor is optional — some distributions ship it separately — and its
// absence must not take the checkconf line with it, since both live under the
// same cleared directive.
func TestDNSDropInKeepsCheckconfWhenTheAnchorToolIsMissing(t *testing.T) {
	got := DropIns(Tools{
		Unbound:          "/usr/bin/unbound",
		UnboundCheckconf: "/usr/bin/unbound-checkconf",
	})
	if len(got) != 1 {
		t.Fatalf("got %d drop-ins, want 1: %+v", len(got), got)
	}
	body := string(got[0].Data)
	if strings.Contains(body, "unbound-anchor") {
		t.Errorf("anchor line written for a box without unbound-anchor:\n%s", body)
	}
	if !strings.Contains(body, "unbound-checkconf") {
		t.Errorf("checkconf line lost along with the anchor:\n%s", body)
	}
}

// nft is the one backend olr tolerates missing: only the DNS redirect needs it
// (docs/dns.md), so its absence is a warning at enable time, not a drop-in.
func TestNoRelayDropInWhenNftIsAbsent(t *testing.T) {
	for _, d := range DropIns(Tools{Dnsmasq: "/usr/bin/dnsmasq"}) {
		if strings.Contains(d.Path, "olr-dnsd") {
			t.Errorf("wrote a relay drop-in with no nft on the box: %s", d.Path)
		}
	}
}
