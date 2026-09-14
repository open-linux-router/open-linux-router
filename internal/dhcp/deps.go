package dhcp

import "github.com/open-linux-router/open-linux-router/internal/core"

// What this module cannot work without.
//
// Declared here rather than in a list for the whole box, because the answer to
// "does this router need dnsmasq" is this module's to give: a box that never
// hands out an address does not, and should not be asked to install one.

// Dependencies returns the backends this module drives.
func Dependencies() []core.Dependency {
	return []core.Dependency{{
		Tool: "dnsmasq",
		Why:  "olr does not implement DHCP itself — it renders configuration for dnsmasq and drives it.",
		// Inert: dnsmasq-base ships the binary and no service, which is the
		// entire reason olr depends on it rather than on the full package.
		// Installing it changes nothing about how the box behaves.
		Inert: true,
		Packages: []core.Package{
			{
				Distro: "debian",
				Name:   "dnsmasq-base",
				Note: "dnsmasq-base, not dnsmasq: the full package also ships a system dnsmasq\n" +
					"service that binds :53 as soon as it is installed. olr runs its own\n" +
					"instance from its own unit and will not fight another daemon for the port.",
			},
			// No -base split anywhere else, and no service started on install,
			// so the Debian warning above would be pure noise on these.
			{Distro: "fedora", Name: "dnsmasq"},
			{Distro: "rhel", Name: "dnsmasq"},
			{Distro: "suse", Name: "dnsmasq"},
			{Distro: "arch", Name: "dnsmasq"},
			{Distro: "alpine", Name: "dnsmasq"},
		},
	}}
}
