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
// unbound is one entry, not three. unbound-checkconf and unbound-anchor matter
// to the units and are looked up separately there, but they ship in the same
// package everywhere, so reporting them individually would tell an operator to
// install the same thing three times.
func Dependencies(c Config) []core.Dependency {
	deps := []core.Dependency{unboundDependency}
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
