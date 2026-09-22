// Package host is the seam between olr and the distribution's own network
// configuration, for the interfaces olr has taken over.
//
// # Why it exists
//
// olr programs addresses and routes straight into the kernel over netlink, and
// never goes through ifupdown, dhcpcd, systemd-networkd or NetworkManager. That
// is what lets it behave the same on every distribution — and it meant those
// programs kept running beside it, configuring the same interfaces, neither
// knowing about the other. design.md §7 called it "two managers" and listed
// taking the interface from the distribution as owed.
//
// The box that forced it: the uplink set statically on ens18, and ifupdown
// still starting dhcpcd there at every boot because /etc/network/interfaces
// said "ens18: DHCP". dhcpcd never got a lease, gave ens18 a 169.254 address,
// and wrote an empty /etc/resolv.conf — so the router reached the internet by
// address and could not resolve a single name.
//
// # What olr takes, stated plainly
//
//   - **IPv4 on the interfaces whose IPv4 olr writes**: the uplink with a static
//     address, and every network member with a subnet. The distribution's DHCP
//     client stops doing IPv4 there. IPv6 is left exactly where it is — olr
//     does not configure IPv6, and whatever the distribution does there (SLAAC,
//     DHCPv6) is the only IPv6 the box has.
//   - **The box's own resolvers**, when the uplink is static and names them. A
//     static uplink replaces the DHCP client that would otherwise have supplied
//     them, so it has to supply them too.
//
// Taken through each program's own supported switch, and given back by
// removing exactly what olr added: design.md §3.4's "generated files are
// additive … never replacing user files", with /etc/resolv.conf the one
// exception, and its original kept so it can be put back.
//
// # What it does not take yet
//
// NetworkManager, systemd-networkd, dhclient, and a resolv.conf maintained by
// resolvconf are detected and reported, with the step that would stop them,
// and not changed. They are real and common, and each one's switch is a
// different file with a different way of being told to re-read it; shipping
// them untested would mean editing the network configuration a box boots with
// on the strength of a reading of its manual.
package host

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Desired is what olr owns of the host's network configuration.
type Desired struct {
	// IPv4 are the interfaces whose IPv4 addressing olr writes.
	IPv4 []string

	// Resolvers are the name servers this box itself uses. Empty means olr
	// does not own the box's resolvers, and leaves them to the distribution.
	Resolvers []netip.Addr
}

// Finding is something about the host that stops olr's configuration from
// being the whole truth, said against the thing it is about.
type Finding struct {
	Interface string
	Message   string
}

// Applier makes the host's own network configuration agree with Desired.
//
// Every field is a seam for tests; the zero value is the real box.
type Applier struct {
	// Root prefixes every path, so tests work in a temp directory. Empty is /.
	Root string

	// StateDir is where originals are kept to be put back. Empty is
	// /var/lib/open-linux-router/host, under Root.
	StateDir string

	// Signal delivers a signal to a process. Nil is the real kill(2).
	Signal func(pid int, sig Sig) error

	// Restart restarts a systemd unit. Nil is core's D-Bus unit.
	Restart func(ctx context.Context, unit string) error

	// Index returns an interface's kernel index. Nil is the real lookup.
	Index func(name string) (int, error)
}

// Sig names the signals this package sends, so tests need no syscall.
type Sig int

// SigReload asks dhcpcd to re-read its configuration and rebind: SIGHUP,
// which is what `dhcpcd -n, --rebind` sends. See sysSignal for the one it must
// never be confused with.
const SigReload Sig = 1

func (a Applier) path(p string) string {
	if a.Root == "" {
		return p
	}
	return filepath.Join(a.Root, p)
}

func (a Applier) stateDir() string {
	if a.StateDir != "" {
		return a.StateDir
	}
	return a.path("/var/lib/open-linux-router/host")
}

func (a Applier) restart(ctx context.Context, unit string) error {
	if a.Restart != nil {
		return a.Restart(ctx, unit)
	}
	u, err := core.NewUnit(unit)
	if err != nil {
		return err
	}
	return u.Restart(ctx)
}

// Apply makes the host agree with d, and reports each step.
//
// dhcpcd first, then resolv.conf, and the order is the argument: a dhcpcd that
// still owns the file would rewrite it the next time anything changed on an
// interface, and olr's resolvers would be gone without anybody having done
// anything. Steps come back alongside any error rather than instead of it, the
// same contract as every writer in this tree (design.md §5.2): no rollback,
// and what did happen is reported.
func (a Applier) Apply(ctx context.Context, d Desired) ([]core.Step, error) {
	var steps []core.Step
	steps = append(steps, a.applyDhcpcd(d)...)
	steps = append(steps, a.applyResolvers(ctx, d)...)

	var failed int
	for _, s := range steps {
		if s.Error != "" {
			failed++
		}
	}
	if failed > 0 {
		return steps, fmt.Errorf("%d of %d host configuration steps failed", failed, len(steps))
	}
	return steps, nil
}

// Findings reports what olr cannot take over, for the interfaces and the
// resolvers d names.
func (a Applier) Findings(d Desired) []Finding {
	var out []Finding
	for _, iface := range d.IPv4 {
		out = append(out, a.otherManagers(iface)...)
	}
	if len(d.Resolvers) > 0 {
		if kind, target := a.resolvKind(); kind == resolvOther {
			out = append(out, Finding{Message: fmt.Sprintf("/etc/resolv.conf is maintained through %s, "+
				"which olr does not write to yet, so this router does not use the uplink's resolvers. "+
				"Point it at them there, or replace the link with a plain file and olr will manage it", target)})
		}
	}
	return out
}

// Resolvers reads the name servers the box actually uses, whoever configured
// them. Nil when they cannot be read.
//
// On a box that resolves through systemd-resolved, /etc/resolv.conf names only
// its stub, 127.0.0.53, so the answer is resolved's own list of upstreams —
// which is where the uplink's resolvers show up, or do not.
func (a Applier) Resolvers() []netip.Addr {
	path := resolvConf
	if kind, _ := a.resolvKind(); kind == resolvResolved {
		path = "/run/systemd/resolve/resolv.conf"
	}
	data, err := os.ReadFile(a.path(path))
	if err != nil {
		return nil
	}
	return nameservers(string(data))
}

func nameservers(content string) []netip.Addr {
	var out []netip.Addr
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "nameserver" {
			if addr, err := netip.ParseAddr(fields[1]); err == nil {
				out = append(out, addr)
			}
		}
	}
	return out
}

// sortedUnique returns a sorted copy with duplicates removed, so the files
// this package renders cannot differ over the order a caller listed things in.
func sortedUnique(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return slices.Compact(out)
}

// step runs fn and records it.
func step(description string, fn func() error) core.Step {
	s := core.Step{Description: description}
	if err := fn(); err != nil {
		s.Error = err.Error()
	} else {
		s.Done = true
	}
	return s
}

// errNotRunning is a process that exited between being found and signalled.
var errNotRunning = errors.New("not running")
