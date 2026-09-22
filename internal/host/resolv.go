package host

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// /etc/resolv.conf, and who it is really written by.
//
// Three shapes, and they get three different treatments:
//
//   - **A link into systemd-resolved.** resolved is the box's resolver and
//     stays it — design.md §3.4 never stops an OS component — so olr gives it
//     the uplink's resolvers through a drop-in, the way internal/core's
//     yield-:53 fix already talks to it, and restarts it.
//   - **A plain file**, or none. Written by a DHCP client (which applyDhcpcd has
//     just told to stop), by an installer, or by hand. olr writes it, keeping
//     the operator's search domains and options, and keeps the original to put
//     back when the resolvers stop being olr's.
//   - **A link to anything else** — resolvconf, NetworkManager's runtime file.
//     Reported (Findings), not changed. Replacing the link would take the file
//     from a program that would then quietly stop working.

const (
	resolvConf     = "/etc/resolv.conf"
	resolvedDropIn = "/etc/systemd/resolved.conf.d/20-olr-resolvers.conf"
	resolvedUnit   = "systemd-resolved.service"

	resolvHeader = "# Written by open-linux-router: this router looks names up through the uplink's resolvers.\n" +
		"# Change them on the uplink (Networks page, or `olr dial set uplink --dns`), not here.\n"
)

type resolvShape int

const (
	resolvFile resolvShape = iota
	resolvResolved
	resolvOther
)

// resolvKind says which shape /etc/resolv.conf has, and where it links to.
func (a Applier) resolvKind() (resolvShape, string) {
	fi, err := os.Lstat(a.path(resolvConf))
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return resolvFile, ""
	}
	target, err := os.Readlink(a.path(resolvConf))
	if err != nil {
		return resolvOther, "a link olr could not read"
	}
	if strings.Contains(target, "systemd/resolve/") {
		return resolvResolved, target
	}
	return resolvOther, target
}

func (a Applier) applyResolvers(ctx context.Context, d Desired) []core.Step {
	kind, _ := a.resolvKind()
	switch kind {
	case resolvResolved:
		return a.applyResolved(ctx, d.Resolvers)
	case resolvFile:
		return a.applyResolvFile(d.Resolvers)
	default:
		return nil
	}
}

// applyResolved writes or removes the drop-in, restarting resolved only when
// it changed — a restart drops resolved's cache, and a status poll is not a
// reason to.
func (a Applier) applyResolved(ctx context.Context, servers []netip.Addr) []core.Step {
	path := a.path(resolvedDropIn)
	have, err := os.ReadFile(path)
	exists := err == nil

	var s core.Step
	switch {
	case len(servers) > 0:
		want := renderResolvedDropIn(servers)
		if exists && string(have) == want {
			return nil
		}
		s = step("give systemd-resolved the uplink's resolvers: "+joinAddrs(servers), func() error {
			return core.WriteFileAtomic(path, []byte(want), 0o644)
		})
	case exists:
		s = step("take the uplink's resolvers back out of systemd-resolved", func() error {
			return os.Remove(path)
		})
	default:
		return nil
	}
	if s.Error != "" {
		return []core.Step{s}
	}
	return []core.Step{s, step("restart systemd-resolved", func() error {
		return a.restart(ctx, resolvedUnit)
	})}
}

func renderResolvedDropIn(servers []netip.Addr) string {
	return resolvHeader + "[Resolve]\nDNS=" + joinAddrs(servers) + "\n"
}

// applyResolvFile writes the file when olr owns the resolvers, and puts the
// original back when it stops owning them.
func (a Applier) applyResolvFile(servers []netip.Addr) []core.Step {
	path := a.path(resolvConf)
	backup := filepath.Join(a.stateDir(), "resolv.conf.orig")

	current, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return []core.Step{{Description: "read " + resolvConf, Error: err.Error()}}
	}
	ours := strings.HasPrefix(string(current), resolvHeader)

	if len(servers) == 0 {
		if !ours {
			return nil
		}
		original, err := os.ReadFile(backup)
		if err != nil {
			// Nothing kept to put back — olr wrote the first one there was.
			// Leaving olr's is the choice that keeps the box resolving; the
			// distribution's client rewrites it the next time it has
			// something to say, now that it is allowed to.
			return nil
		}
		return []core.Step{step("put back the /etc/resolv.conf olr replaced", func() error {
			if err := core.WriteFileAtomic(path, original, 0o644); err != nil {
				return err
			}
			return os.Remove(backup)
		})}
	}

	want := renderResolvConf(string(current), servers)
	if string(current) == want {
		return nil
	}
	var steps []core.Step
	if !ours && err == nil {
		// Kept once, the first time: the file as it was before olr, which is
		// what giving the resolvers back has to restore. A second take would
		// overwrite it with olr's own.
		if _, statErr := os.Stat(backup); os.IsNotExist(statErr) {
			s := step("keep the current /etc/resolv.conf to put back later", func() error {
				return core.WriteFileAtomic(backup, current, 0o644)
			})
			steps = append(steps, s)
			if s.Error != "" {
				return steps
			}
		}
	}
	return append(steps, step("look names up through the uplink's resolvers: "+joinAddrs(servers)+
		" (/etc/resolv.conf)", func() error {
		return core.WriteFileAtomic(path, []byte(want), 0o644)
	}))
}

// renderResolvConf is olr's resolv.conf: the header, the operator's search
// domains, the resolvers, and the operator's options — everything but the
// name servers carried over from what was there.
func renderResolvConf(current string, servers []netip.Addr) string {
	var search, options []string
	for _, line := range strings.Split(current, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "search", "domain":
			search = append(search, strings.TrimSpace(line))
		case "options":
			options = append(options, strings.TrimSpace(line))
		}
	}

	var b strings.Builder
	b.WriteString(resolvHeader)
	for _, l := range search {
		b.WriteString(l + "\n")
	}
	for _, s := range servers {
		fmt.Fprintf(&b, "nameserver %s\n", s)
	}
	for _, l := range options {
		b.WriteString(l + "\n")
	}
	return b.String()
}

func joinAddrs(in []netip.Addr) string {
	out := make([]string, 0, len(in))
	for _, a := range in {
		out = append(out, a.String())
	}
	return strings.Join(out, " ")
}

// --- the managers olr does not take over yet --------------------------------

// otherManagers reports anything besides dhcpcd configuring iface's IPv4.
func (a Applier) otherManagers(iface string) []Finding {
	var out []Finding
	if idx, err := a.index(iface); err == nil {
		if state, err := os.ReadFile(a.path(fmt.Sprintf("/run/systemd/netif/links/%d", idx))); err == nil &&
			strings.Contains(string(state), "ADMIN_STATE=configured") {
			out = append(out, Finding{Interface: iface, Message: fmt.Sprintf("systemd-networkd also "+
				"configures %s, and olr does not take interfaces from it yet, so the two can disagree "+
				"about its IPv4 address. In the .network file for %s set DHCP=ipv6 (or DHCP=no), then "+
				"run `networkctl reload`", iface, iface)})
		}
		if state, err := os.ReadFile(a.path(fmt.Sprintf("/run/NetworkManager/devices/%d", idx))); err == nil &&
			strings.Contains(string(state), "managed=true") {
			out = append(out, Finding{Interface: iface, Message: fmt.Sprintf("NetworkManager also "+
				"manages %s, and olr does not take interfaces from it yet, so the two can disagree "+
				"about its IPv4 address. Run `nmcli device set %s managed no`", iface, iface)})
		}
	}
	if a.dhclientOn(iface) {
		out = append(out, Finding{Interface: iface, Message: fmt.Sprintf("dhclient is asking for an "+
			"IPv4 address on %s, and olr cannot stop it yet. In /etc/network/interfaces change "+
			"`iface %s inet dhcp` to `iface %s inet manual`, then reboot", iface, iface, iface)})
	}
	return out
}

// dhclientOn reports a running dhclient with iface among its arguments.
func (a Applier) dhclientOn(iface string) bool {
	procs, _ := filepath.Glob(a.path("/proc/[0-9]*/cmdline"))
	for _, p := range procs {
		data, err := os.ReadFile(p)
		if err != nil || len(data) == 0 {
			continue
		}
		args := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
		if base := filepath.Base(args[0]); base != "dhclient" && base != "dhclient3" {
			continue
		}
		for _, arg := range args[1:] {
			if arg == iface {
				return true
			}
		}
	}
	return false
}

func (a Applier) index(name string) (int, error) {
	if a.Index != nil {
		return a.Index(name)
	}
	return interfaceIndex(name)
}

func (a Applier) signal(pid int, sig Sig) error {
	if a.Signal != nil {
		return a.Signal(pid, sig)
	}
	s, err := sysSignal(sig)
	if err != nil {
		return err
	}
	if err := syscall.Kill(pid, s); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return errNotRunning
		}
		return err
	}
	return nil
}

// sysSignal is the real signal behind each of ours.
//
// dhcpcd(8) maps its command-line verbs onto signals, and two of them read
// alike and do opposite things. `-n, --rebind` is SIGHUP: reload the
// configuration and rebind — what this package wants. `-k, --release` is
// SIGALRM: release the lease, de-configure the interface "regardless of the
// persistent option", and exit when nothing is left. The first version sent
// SIGALRM, believing it meant "reconfigure", and on the box this was built for
// it stopped dhcpcd outright and took the IPv6 it managed with it — the one
// thing this package promises to leave alone.
func sysSignal(sig Sig) (syscall.Signal, error) {
	switch sig {
	case SigReload:
		return syscall.SIGHUP, nil
	default:
		return 0, fmt.Errorf("unknown signal %d", sig)
	}
}
