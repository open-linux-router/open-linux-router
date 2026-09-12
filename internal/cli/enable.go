package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/packaging"
)

// InstallDir is where `olr enable` puts the binary when it is not already on a
// system path.
//
// /usr/local/bin, not /usr/bin: /usr/bin belongs to the distribution's package
// manager, and the .deb installs olr there. A standalone install that wrote
// /usr/bin/olr would overwrite a file dpkg owns, without dpkg knowing — which
// the shell script this replaces did, and which made the next `apt upgrade`
// unpredictable. The FHS reserves /usr/local for exactly this case.
const InstallDir = "/usr/local/bin"

// enableCommand makes this box run olr, and keep running it across reboots.
//
// The name is systemd's, and so is the meaning core.Unit already gives it:
// Start is "run now", Enable is "run after the next reboot too". It is not
// called `install` for a reason worth recording — by the time an operator can
// type it, olr is plainly already installed, and the word would describe the
// smallest of the things this does rather than the point of it.
//
// What it does beyond the symlinks is the standalone path's whole story: a
// single downloaded binary has no unit files beside it, and systemd reads unit
// files from disk and offers no way around that. So this writes them. On a box
// where the .deb already did, it finds them identical and says so.
//
// Nothing here is silent. Every path written is printed as it is written,
// because the operator ran one command and four files appeared, and design.md
// §7's promise — installing changes nothing until you say so — survives only
// if saying so tells you what changed.
func enableCommand() *cobra.Command {
	c := &cobra.Command{
		Use:     "enable",
		Short:   "Run olr on this box, now and after every reboot",
		GroupID: GroupService,
		Long: "Set this machine up to run olr, and start it.\n\n" +
			"Writes the systemd units to " + packaging.UnitDir + ", creates\n" +
			packaging.EnvPath + " if it does not exist, corrects the units'\n" +
			"paths when dnsmasq or unbound live somewhere other than Debian puts\n" +
			"them, and enables the service so it survives a reboot.\n\n" +
			"Only the control plane is enabled. No DHCP server appears and no\n" +
			"resolver takes over port 53 — each module enables its own backend\n" +
			"when you configure it.\n\n" +
			"Everything written is printed. Use --dry-run to see it first.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return runEnable(cmd) },
	}
	return c
}

// disableCommand stops olr coming back after a reboot.
//
// Deliberately not the inverse of everything `enable` did: it removes the boot
// symlinks and stops the service, and leaves the unit files where they are.
// That is systemd's meaning of the word and internal/dhcp's — `olr dhcp
// disable` keeps the pool it stops serving — and a `disable` that deleted
// files would be an uninstall wearing a smaller word. The output says the
// files are still there, so nobody has to guess whether the box is clean.
func disableCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "disable",
		Short:   "Stop olr, and stop it starting at boot",
		GroupID: GroupService,
		Long: "Stop the olr service and remove it from boot.\n\n" +
			"The unit files stay where they are, so `olr enable` brings it back\n" +
			"without reinstalling anything. Configuration and state are untouched.\n\n" +
			"Backends keep serving: stopping the control plane never interrupts\n" +
			"DHCP or DNS. Use `olr dhcp disable` and `olr dns disable` for those.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return runDisable(cmd) },
	}
}

func runEnable(cmd *cobra.Command) error {
	out := cmd.OutOrStdout()
	dry := DryRun(cmd)

	// Root first, before anything is looked up. Ordering is the whole point:
	// finding the backends needs no privilege but reports on the *invoking*
	// user's environment, so running without sudo used to produce a confident
	// and wrong sentence about dnsmasq instead of the one true reason the
	// command could not proceed. The first failure an operator sees should be
	// the real one.
	if !dry {
		if os.Geteuid() != 0 {
			return errors.New("olr enable writes to " + packaging.UnitDir + " and needs root; try sudo")
		}
	}

	tools, err := findTools()
	if err != nil {
		return err
	}
	if tools.Nft == "" {
		fmt.Fprintf(out, "warning: nft was not found on $PATH or in %s. Everything works\n"+
			"except the DNS redirect (`olr dns set --redirect`), which needs your\n"+
			"nftables package.\n\n", strings.Join(sbinDirs, ", "))
	}

	// Read-only, so it runs on the --dry-run path too: "what would this do"
	// should include "and what is already in the way".
	warnDistroBackends(cmd.Context(), out)

	plan, err := enablePlan(tools)
	if err != nil {
		return err
	}

	if dry {
		fmt.Fprintf(out, "Would write:\n")
		for _, w := range plan {
			fmt.Fprintf(out, "  %s%s\n", w.path, w.note)
		}
		fmt.Fprintf(out, "\nThen enable and start %s.\n", packaging.PrimaryUnit)
		return nil
	}

	for _, w := range plan {
		written, err := w.apply()
		if err != nil {
			return err
		}
		if written {
			fmt.Fprintf(out, "wrote %s%s\n", w.path, w.note)
		} else {
			fmt.Fprintf(out, "kept  %s%s\n", w.path, w.note)
		}
	}

	// Enable before Start: core.Unit.Enable ends with a systemd daemon-reload,
	// which is what makes the units just written visible to the manager. Doing
	// it the other way round asks systemd to start a unit it has not read.
	//
	// Both go over D-Bus. There is no exec.Command("systemctl") here, and the
	// reason is the same one design.md §3.6 gives for olrd: the mechanism
	// already exists in core, and a second one would be a second thing to keep
	// correct.
	unit, err := core.NewUnit(packaging.PrimaryUnit)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), daemonTimeout)
	defer cancel()

	if err := unit.Enable(ctx); err != nil {
		return fmt.Errorf("enabling %s: %w", packaging.PrimaryUnit, err)
	}
	fmt.Fprintf(out, "enabled %s\n", packaging.PrimaryUnit)

	if err := unit.Start(ctx); err != nil {
		return fmt.Errorf("starting %s: %w", packaging.PrimaryUnit, err)
	}
	fmt.Fprintf(out, "started %s\n", packaging.PrimaryUnit)

	return reportEnabled(out)
}

func runDisable(cmd *cobra.Command) error {
	if err := RejectDryRun(cmd); err != nil {
		return err
	}
	out := cmd.OutOrStdout()

	unit, err := core.NewUnit(packaging.PrimaryUnit)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), daemonTimeout)
	defer cancel()

	if err := unit.Stop(ctx); err != nil {
		return err
	}
	if err := unit.Disable(ctx); err != nil {
		return err
	}

	fmt.Fprintf(out, "%s stopped, and will not start at boot.\n\n", packaging.PrimaryUnit)
	fmt.Fprintf(out, "The unit files are still in %s and your configuration is\n"+
		"untouched, so `olr enable` brings it back. DHCP and DNS keep serving —\n"+
		"stopping the control plane does not interrupt them.\n", packaging.UnitDir)
	return nil
}

// write is one file `olr enable` puts on disk.
type write struct {
	path string
	data []byte
	mode os.FileMode
	// keep reports whether an existing file at this path must be left alone.
	keep bool
	note string
}

// apply writes the file, and reports whether it actually did.
func (w write) apply() (bool, error) {
	if w.keep {
		if _, err := os.Stat(w.path); err == nil {
			return false, nil
		} else if !os.IsNotExist(err) {
			return false, fmt.Errorf("checking %s: %w", w.path, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
		return false, fmt.Errorf("creating %s: %w", filepath.Dir(w.path), err)
	}
	if err := core.WriteFileAtomic(w.path, w.data, w.mode); err != nil {
		return false, fmt.Errorf("writing %s: %w", w.path, err)
	}
	return true, nil
}

// enablePlan lists everything that will be written, in the order it will be.
//
// Built whole before anything is written so that --dry-run and the real run
// cannot describe different things.
func enablePlan(tools packaging.Tools) ([]write, error) {
	var plan []write

	self, err := selfInstall()
	if err != nil {
		return nil, err
	}
	if self != nil {
		plan = append(plan, *self)
	}

	units, err := packaging.Units()
	if err != nil {
		return nil, err
	}
	for _, u := range units {
		plan = append(plan, write{
			path: filepath.Join(packaging.UnitDir, u.Name),
			data: u.Data,
			mode: 0o644,
		})
	}

	env, err := packaging.Env()
	if err != nil {
		return nil, err
	}
	plan = append(plan, write{
		path: packaging.EnvPath,
		data: env,
		mode: 0o644,
		keep: true,
		note: "  (kept if it already exists)",
	})

	for _, d := range packaging.DropIns(tools) {
		plan = append(plan, write{
			path: d.Path,
			data: d.Data,
			mode: 0o644,
			note: "  (this box does not use Debian's paths)",
		})
	}
	return plan, nil
}

// selfInstall copies the running binary onto a system path, unless it is
// already running from one.
//
// The units name an absolute path, so a binary left in a download directory
// would give systemd an ExecStart that stops working the moment somebody
// tidies up. Writing through core.WriteFileAtomic matters more here than
// anywhere else: it renames into place, and renaming over a *running*
// executable works where writing to it fails with ETXTBSY. That is what lets
// `olr enable` double as the upgrade path.
func selfInstall() (*write, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("finding this binary: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", exe, err)
	}

	dir := filepath.Dir(exe)
	// Already installed: /usr/local/bin by this command, or /usr/bin by the
	// .deb, whose copy is dpkg's to manage and not ours to overwrite.
	if dir == InstallDir || dir == "/usr/bin" || dir == "/bin" || dir == "/usr/sbin" {
		return nil, nil
	}

	data, err := os.ReadFile(exe)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", exe, err)
	}
	return &write{
		path: filepath.Join(InstallDir, "olr"),
		data: data,
		mode: 0o755,
		note: "  (copied from " + exe + ")",
	}, nil
}

// sbinDirs are searched for a backend that $PATH does not have.
//
// $PATH alone was a real bug, not a theoretical one. dnsmasq, unbound and nft
// are daemons, Debian puts daemons in /usr/sbin, and /usr/sbin is not on an
// ordinary user's PATH there. So `./olr enable` told operators with dnsmasq
// installed *and running* that dnsmasq was not installed, and sent them to
// install it a second time. packaging.DebianDnsmasq — one import away — has
// said /usr/sbin all along.
//
// Searched after $PATH, never before: an operator with their own build earlier
// on PATH means it.
var sbinDirs = []string{"/usr/local/sbin", "/usr/sbin", "/sbin"}

// lookTool finds a backend binary, or returns "".
func lookTool(name string) string {
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	for _, dir := range sbinDirs {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return path
		}
	}
	return ""
}

// findTools locates the backends, and refuses when the two that are not
// optional are missing.
//
// The .deb declares these as dependencies so apt resolves them before any of
// our code runs. Here there is no package manager to ask, so this is the
// equivalent — and it fails before writing anything, because a box with units
// installed and no dnsmasq is a worse place to stop than a box with neither.
func findTools() (packaging.Tools, error) {
	t := packaging.Tools{
		Dnsmasq:          lookTool("dnsmasq"),
		Unbound:          lookTool("unbound"),
		UnboundCheckconf: lookTool("unbound-checkconf"),
		UnboundAnchor:    lookTool("unbound-anchor"),
		Nft:              lookTool("nft"),
		// Not required, and deliberately not checked below. A router with no
		// published services never needs a proxy, so its absence is not a
		// reason to refuse to enable olr — `olr ingress` says what is missing
		// and how to get it, at the moment somebody actually wants it.
		Caddy: lookTool("caddy"),
	}

	if t.Dnsmasq == "" {
		// dnsmasq-base is named, and the difference explained, because `apt
		// install dnsmasq` is what everybody types and it is the wrong answer:
		// the full package ships a dnsmasq.service that binds :53 the moment it
		// is installed, and olr will not fight another daemon for a port. The
		// .deb has depended on dnsmasq-base since the beginning; this is the
		// path where nobody gets to read that dependency.
		return t, missingTool("dnsmasq", "dnsmasq-base",
			"olr does not implement DHCP itself.",
			"dnsmasq-base rather than dnsmasq: the full package also ships a system\n"+
				"dnsmasq service that binds :53 as soon as it is installed. olr runs its\n"+
				"own instance from its own unit, and refuses to start when something else\n"+
				"already holds the port.")
	}
	if t.Unbound == "" || t.UnboundCheckconf == "" {
		return t, missingTool("unbound", "unbound",
			"olr does not resolve names itself.",
			"Debian enables its own unbound.service on install, listening on\n"+
				"127.0.0.1:53. olr runs a separate instance and owns :53 through its relay,\n"+
				"so disable that one before turning DNS on:\n"+
				"  sudo systemctl disable --now unbound.service")
	}
	return t, nil
}

// missingTool says what was not found, where we looked, and what to install.
//
// Naming the directories searched is the part worth keeping. "not on PATH" is a
// claim about the operator's shell that they cannot check without knowing which
// PATH we meant, and it was wrong often enough to be worth never saying again.
func missingTool(name, pkg, why, note string) error {
	return fmt.Errorf("%s was not found.\nLooked on $PATH and in %s.\n\n%s On Debian and Ubuntu:\n\n"+
		"  sudo apt install %s\n\n%s",
		name, strings.Join(sbinDirs, ", "), why, pkg, note)
}

// distroBackends are the units a distribution ships for the daemons olr drives.
//
// olr starts its own instances from its own units (internal/dhcp/render.go and
// internal/dns/render.go both say why), so the distro's are not upgrades of
// ours or ours of theirs — they are a second daemon competing for one port. The
// module preflights catch that at the moment a module is turned on, which is
// correct but late: by then the operator has configured a pool or an upstream
// and is expecting it to work. `olr enable` is already looking these binaries
// up, so it can see the collision coming and say so while nothing is at stake.
var distroBackends = []struct {
	unit   string
	holds  string
	advice string
}{
	{"dnsmasq.service", "UDP/67 and :53",
		"  sudo systemctl disable --now dnsmasq.service"},
	{"unbound.service", ":53",
		"  sudo systemctl disable --now unbound.service"},
	// Not disabled, ever: this box resolves through it, so stopping it takes
	// name resolution away from the machine you are typing on until olr's relay
	// is up. It is told to give up the socket instead.
	{"systemd-resolved.service", ":53",
		"  # do not disable this one — the box resolves through it\n" +
			"  # set DNSStubListener=no in /etc/systemd/resolved.conf, then:\n" +
			"  sudo systemctl restart systemd-resolved"},
}

// warnDistroBackends reports the distribution's own daemons, if any are live.
//
// Best-effort and never fatal. `olr enable` starts no backend, so none of this
// is a conflict yet, and refusing to install over a daemon that is doing its job
// today would be exactly the machine-wide interference design.md §3.4 forbids.
func warnDistroBackends(parent context.Context, out io.Writer) {
	ctx, cancel := context.WithTimeout(parent, daemonTimeout)
	defer cancel()

	for _, b := range distroBackends {
		unit, err := core.NewUnit(b.unit)
		if err != nil {
			return // no service manager to ask; nothing to warn about
		}
		status, err := unit.Status(ctx)
		if err != nil || !status.Installed || (!status.Active && !status.Enabled) {
			continue
		}

		state := "enabled at boot"
		if status.Active {
			state = "running"
			if status.Enabled {
				state = "running and enabled at boot"
			}
		}
		fmt.Fprintf(out, "warning: %s is %s, and holds %s.\n"+
			"olr runs its own instance rather than taking that one over, and will\n"+
			"refuse to start while the port is held. Before turning the module on:\n%s\n\n",
			b.unit, state, b.holds, b.advice)
	}
}

// reportEnabled says what to do next, which is the step nobody guesses.
func reportEnabled(w io.Writer) error {
	_, err := fmt.Fprintf(w, `
olr is running, and nothing else on this machine has changed. No DHCP
server, no resolver, no firewall rule — each module starts its backend
when you configure it.

To open the web UI on your network:

  sudo olr listen 0.0.0.0:8080

Then browse to http://<this box>:8080 and paste the token from
%s when asked.

Or stay on the command line:

  olr link show interfaces      what this machine has
  sudo olr adopt <interface>    hand one to olr
  olr dhcp --help               then serve addresses on it
`, core.TokenPath)
	return err
}
