package dns

import "github.com/open-linux-router/open-linux-router/internal/core"

// What this module cannot work without.
//
// unbound is the reason this file exists at all. It was a hard dependency of
// the .deb and a hard precondition of `olr enable`, so a box that only ever
// wanted DHCP still had to have a resolver — and on Debian, installing that
// resolver *starts* it on 127.0.0.1:53, which olr's own status page then
// reports as something holding the port. Depending on it by default cost every
// operator a daemon they had not asked for and handed some of them a conflict
// to debug.
//
// So it is declared here, checked when somebody turns DNS on, and left out of
// the package. See core.Dependency.Inert.

// Dependencies returns the backends this module needs for the given
// configuration.
//
// Config-aware, because "what does DNS need" has two answers. unbound is always
// one. nftables is only needed by the redirect (packaging/dropin.go writes the
// rule into the relay's unit), so a router that has not turned the redirect on
// should not be told to install it — that is the same rule this file exists to
// apply, one level in.
//
// Two entries for unbound, not one and not three. unbound-checkconf does ship
// with the resolver everywhere, so it is looked up by the units and reported by
// neither. unbound-anchor does not: Debian, Fedora, RHEL and openSUSE all split
// it into a package of its own, and a box that installed only `unbound` has a
// resolver that cannot start at all — see unboundAnchorDependency.
func Dependencies(c Config) []core.Dependency {
	deps := []core.Dependency{unboundDependency, unboundAnchorDependency}
	if c.Hijack.Enabled {
		deps = append(deps, nftablesDependency)
	}
	return deps
}

var unboundDependency = core.Dependency{
	Tool: "unbound",
	Why: "olr owns :53 and applies policy there, but it does not resolve names — " +
		"recursion, DNSSEC validation and caching are unbound's half.",
	// Not inert: Debian's unbound package enables and starts unbound.service
	// on 127.0.0.1:53 the moment it is installed.
	Inert: false,
	// Which is why the install has to clear it in the same breath. Without
	// this, fixing "unbound is not installed" only ever swaps one red panel for
	// the next one — the conflict is *caused* by the cure.
	Shadows: []string{"unbound.service"},
	Packages: []core.Package{
		{
			Distro: "debian",
			Name:   "unbound",
			// Still the truth for somebody installing by hand, and kept for
			// them. It is no longer the only path: `olr dns fix` and the button
			// on the DNS page do both halves, which is what the note is
			// describing the manual version of.
			Note: "Debian starts its own unbound.service on 127.0.0.1:53 when this installs.\n" +
				"olr runs a separate instance and owns :53 through its relay, so stand the\n" +
				"distribution's one down afterwards:\n" +
				"  sudo systemctl disable --now unbound.service\n" +
				"Or let olr do both: `sudo olr dns fix`.",
		},
		{Distro: "fedora", Name: "unbound"},
		{Distro: "rhel", Name: "unbound"},
		{Distro: "suse", Name: "unbound"},
		{Distro: "arch", Name: "unbound"},
		{Distro: "alpine", Name: "unbound"},
	},
}

// unboundAnchorDependency is the root trust anchor's fetcher, and the surprise
// is how hard a requirement it is.
//
// olr renders `auto-trust-anchor-file`, and unbound treats a missing one as
// fatal rather than as something to bootstrap — "error reading
// auto-trust-anchor-file" and it refuses to start. The unit runs unbound-anchor
// before it to write that file. The `-` on that ExecStartPre is about
// unbound-anchor's exit code, not about it being optional, so an absent binary
// is skipped in silence and the failure surfaces one step later as a resolver
// that will not come up.
//
// Which is exactly what a box that installed `unbound` on Debian gets, because
// the binary is not in that package. Declaring it here is what turns that into
// a sentence on the DNS page with a button under it.
//
// Inert, and so carried by the .deb: the package is one binary, one man page,
// no unit and no port (compare unbound above, which is none of those things).
// That means an apt box never meets this blocker at all — it is already there
// when DNS is first turned on — and a tarball box is told precisely what to
// install rather than being left with a unit that fails.
var unboundAnchorDependency = core.Dependency{
	Tool: "unbound-anchor",
	Why: "unbound validates DNSSEC against the root trust anchor and refuses to " +
		"start without it; this is what fetches it and keeps it current as the root key rolls.",
	Inert: true,
	Packages: []core.Package{
		// Split out of the resolver package by everything but Arch and Alpine.
		{Distro: "debian", Name: "unbound-anchor"},
		{Distro: "fedora", Name: "unbound-anchor"},
		{Distro: "rhel", Name: "unbound-anchor"},
		{Distro: "suse", Name: "unbound-anchor"},
		// Ship one unbound package with every tool in it, so this resolves to
		// the same install the entry above already asked for.
		{Distro: "arch", Name: "unbound"},
		{Distro: "alpine", Name: "unbound"},
	},
}

// nftablesDependency is needed only by the redirect, which is why it is not in
// the unconditional list.
//
// Inert, and so declared by the .deb: the nftables package installs a command
// and a unit that is not enabled, and takes no port. A router without it is not
// a configuration worth optimising the package for, but a router that has not
// asked for the redirect should still not be nagged about it.
var nftablesDependency = core.Dependency{
	Tool: "nft",
	Why: "the DNS redirect sends clients that ask another resolver to olr instead, " +
		"and the rule that does it is loaded with nft.",
	Inert: true,
	Packages: []core.Package{
		{Distro: "debian", Name: "nftables"},
		{Distro: "fedora", Name: "nftables"},
		{Distro: "rhel", Name: "nftables"},
		{Distro: "suse", Name: "nftables"},
		{Distro: "arch", Name: "nftables"},
		{Distro: "alpine", Name: "nftables"},
	},
}
