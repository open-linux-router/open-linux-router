package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The backends a module needs, declared by the module that needs them.
//
// Per module rather than one list for the box, and that is the whole point. A
// router that hands out addresses and never resolves a name has no use for
// unbound, and until now olr disagreed twice: `olr enable` refused to install at
// all when unbound was missing, and the .deb pulled it in on every box — where
// Debian then started it on 127.0.0.1:53, so the package manufactured the exact
// conflict olr's own status page reports.
//
// So a dependency is checked when somebody asks for the thing that needs it,
// and reported as a Blocker on that module's page and nowhere else. internal/
// ingress has always worked this way — "a router with no published services
// never needs a proxy" — and this is that rule applied to the other three.

// BlockerMissingDependency is the Kind of the blockers this file produces.
const BlockerMissingDependency = "missing_dependency"

// Dependency is one backend binary a module cannot work without.
type Dependency struct {
	// Tool is the executable to look for.
	Tool string

	// Why is one sentence on what olr cannot do without it, in the operator's
	// terms rather than the package's.
	Why string

	// Packages names the tool per distribution family.
	Packages []Package

	// Inert reports whether installing the package disturbs the box.
	//
	// This is what decides whether the .deb declares it, and it is a sharper
	// test than "which module uses it". dnsmasq-base and nftables install a
	// binary and nothing else; depending on them costs a box that never uses
	// them some disk and no behaviour. The full unbound package enables and
	// starts unbound.service on 127.0.0.1:53 the moment it lands, so depending
	// on it means every olr install takes a port on a machine that may never
	// serve DNS — and olr then reports its own dependency as a conflict.
	//
	// A test holds packaging/nfpm.yaml to this: the .deb's depends list is
	// exactly the inert ones.
	Inert bool

	// Shadows names the distribution units installing this package brings up,
	// where those are second copies of a daemon olr runs itself.
	//
	// It exists because the two red panels an operator meets are one problem.
	// "unbound is not installed" and "unbound.service holds :53" arrive in
	// sequence — Debian's package enables and starts the unit on install — so
	// fixing only the first swaps one panel for the other. Declaring the unit
	// here is what lets the install action say both lines before it is pressed
	// and clear both when it is.
	//
	// The names are matched against DistroBackends, which is where the
	// knowledge of how to stand one down lives; a name with no entry there is a
	// test failure rather than a silent no-op. Empty for an inert package,
	// which by definition brings nothing up.
	Shadows []string
}

// Package is a tool's name in one distribution family.
type Package struct {
	// Distro is matched with Distro.Is, so "debian" covers Ubuntu and the rest
	// of the family through ID_LIKE.
	Distro string

	// Name is the package to install.
	Name string

	// Note is anything the operator needs to know beyond the name — and it is
	// per-distribution because the reasons are. "dnsmasq-base, not dnsmasq"
	// matters enormously on Debian and is meaningless on Fedora, which does not
	// split the package at all.
	Note string
}

// PackageFor returns the package for this distribution, if it is known.
func (d Dependency) PackageFor(distro Distro) (Package, bool) {
	for _, p := range d.Packages {
		if distro.Is(p.Distro) {
			return p, true
		}
	}
	return Package{}, false
}

// Missing reports whether the tool is absent from this box.
func (d Dependency) Missing() bool { return LookTool(d.Tool) == "" }

// DependencyBlockers reports the dependencies that are not installed.
//
// Ordinary and expected on a tarball box, which is why it is a blocker and not
// an error: the module says what it needs, at the moment somebody wants it,
// with the command for the distribution actually running.
func DependencyBlockers(deps []Dependency) []Blocker {
	distro := DetectDistro()

	var out []Blocker
	for _, d := range deps {
		if !d.Missing() {
			continue
		}
		out = append(out, Blocker{
			Kind:    BlockerMissingDependency,
			Summary: fmt.Sprintf("%s is not installed on this box.", d.Tool),
			Detail:  d.Why,
			Fix:     installAdvice(d, distro),
			Action:  installAction(d, distro),
		})
	}
	return out
}

// installAction is olr fetching the package itself, or nil where it cannot.
//
// Nil for a distribution whose package manager is not in installArgv, which is
// the same set installAdvice already declines to guess a command for. The two
// degrade together on purpose: a box that gets "install unbound with your
// distribution's package manager" must not also get a button, and the single
// table behind both is what stops olr recommending one thing and running
// another.
func installAction(d Dependency, distro Distro) *Action {
	pkg, ok := d.PackageFor(distro)
	if !ok {
		return nil
	}
	argv, ok := installArgvFor(distro, pkg.Name)
	if !ok {
		return nil
	}

	// The stand-downs the install is about to make necessary, named now so the
	// button can promise them rather than summoning them.
	shadows := shadowedBackends(d.Shadows)

	runs := []string{strings.Join(argv, " ")}
	label := "Install " + pkg.Name
	for _, b := range shadows {
		runs = append(runs, standDownCommand(b.Unit))
	}
	if len(shadows) > 0 {
		label += " and hand olr " + shadows[0].Holds
	}

	return &Action{
		ID:    "install:" + d.Tool,
		Label: label,
		Runs:  runs,
		do: func(ctx context.Context) []Step {
			steps := installPackage(ctx, argv)
			if StepsFailed(steps) {
				return steps
			}
			return append(steps, standDownShadows(ctx, shadows)...)
		},
	}
}

// shadowedBackends resolves unit names against the table that knows how to
// stand them down.
func shadowedBackends(units []string) []DistroBackend {
	var out []DistroBackend
	for _, name := range units {
		for _, b := range DistroBackends {
			if b.Unit == name && b.perform != nil {
				out = append(out, b)
			}
		}
	}
	return out
}

// standDownShadows clears the units the install just brought up.
//
// Liveness is re-read rather than assumed, because whether the package starts
// anything is the distribution's decision and not ours: Debian enables and
// starts unbound.service on install and Fedora does not. A unit that did not
// come up is reported as a step that found nothing to do rather than skipped
// silently — the button promised the line, so the answer to it belongs in the
// record even when the answer is "there was nothing there".
func standDownShadows(ctx context.Context, shadows []DistroBackend) []Step {
	var steps []Step
	for _, b := range shadows {
		unit, err := newUnit(b.Unit)
		if err != nil {
			// No service manager to ask. Not an error: the install succeeded,
			// and a box with no systemd cannot have had a unit started by it.
			continue
		}
		status, err := unit.Status(ctx)
		if err != nil || !status.Installed || (!status.Active && !status.Enabled) {
			steps = append(steps, Step{
				Description: standDownCommand(b.Unit) +
					" — this distribution did not start it, so there was nothing to stand down",
				Done: true,
			})
			continue
		}
		steps = append(steps, standDown(ctx, b)...)
	}
	return steps
}

// installAdvice is the command to run, degrading as it recognises less.
//
// The fallback names the tool and stops. Guessing a package manager would be
// worse than admitting we do not know: an operator on a distribution we have
// never heard of can translate "install unbound" in a second, and cannot
// un-run an apt command that was never going to work anyway.
func installAdvice(d Dependency, distro Distro) string {
	pkg, ok := d.PackageFor(distro)
	if !ok {
		return "Install " + d.Tool + " with your distribution's package manager."
	}

	var command string
	for _, family := range installOrder {
		if !distro.Is(family) {
			continue
		}
		if format, known := installCommands[family]; known {
			command = fmt.Sprintf(format, pkg.Name)
			break
		}
	}
	if command == "" {
		command = "Install " + pkg.Name + " with your distribution's package manager."
	}
	if pkg.Note != "" {
		return command + "\n\n# " + pkg.Note
	}
	return command
}

// sbinDirs are searched for a backend that $PATH does not have.
//
// $PATH alone was a real bug, not a theoretical one. dnsmasq, unbound and nft
// are daemons, Debian puts daemons in /usr/sbin, and /usr/sbin is not on an
// ordinary user's PATH there. So olr told operators with dnsmasq installed
// *and running* that dnsmasq was not installed, and sent them to install it a
// second time.
//
// Searched after $PATH, never before: an operator with their own build earlier
// on PATH means it.
var sbinDirs = []string{"/usr/local/sbin", "/usr/sbin", "/sbin"}

// LookTool finds a backend binary, or returns "".
func LookTool(name string) string {
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
