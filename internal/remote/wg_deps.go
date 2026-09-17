package remote

import "github.com/open-linux-router/open-linux-router/internal/core"

// What this module cannot work without.
//
// Declared here rather than in a list for the whole box, because the answer to
// "does this router need wireguard-tools" is this module's to give: a box
// nobody dials into does not, and should not be asked to install one.
//
// This is the most inert dependency in olr. `wireguard-tools` is two binaries,
// a man page and a `wg-quick` unit template that is not enabled — no daemon, no
// port, nothing started on install. So it is carried by the .deb
// (core.Dependency.Inert), which means an apt box never meets this blocker at
// all and a tarball box is told exactly what to install.
//
// **The unit template is the reason this comment mentions it.** Installing the
// package puts `wg-quick@.service` on the box, and an operator who finds it may
// reasonably think that is how olr runs the tunnel. It is not, and must not be:
// docs/remote-access.md §5.1 is why — `wg-quick` installs its own policy
// routing, which `gateway` correctly refuses as a second owner of the routing
// table.

// Dependencies returns the backends this module drives.
func Dependencies() []core.Dependency {
	return []core.Dependency{{
		Tool: "wg",
		Why: "olr does not implement WireGuard — the tunnel is in the kernel, and this is the " +
			"tool that loads keys and peers into it.",
		Inert: true,
		Packages: []core.Package{
			{
				Distro: "debian",
				Name:   "wireguard-tools",
				// `wireguard` is a metapackage that also pulls in
				// `wireguard-dkms`, which builds a kernel module every box
				// running a kernel since 5.6 already has in tree. Naming the
				// -tools package keeps the install to two binaries.
				Note: "wireguard-tools, not wireguard: the metapackage also pulls in a DKMS module\n" +
					"that any kernel from 5.6 onwards already has built in.",
			},
			{Distro: "fedora", Name: "wireguard-tools"},
			{Distro: "rhel", Name: "wireguard-tools"},
			{Distro: "suse", Name: "wireguard-tools"},
			{Distro: "arch", Name: "wireguard-tools"},
			{Distro: "alpine", Name: "wireguard-tools"},
		},
	}}
}
