package ingress

import "slices"

// The DNS providers this build can write an ACME challenge record through.
//
// This list is a *build-time* fact and that is the uncomfortable part of the
// module. Caddy has no runtime plugin loading — providers are compiled in — so
// no official Caddy package carries any of them, which is why docs/ingress.md
// §5.2 has us shipping our own build at all.
//
// Having accepted that, §5.4 takes the other half of the decision: compile in
// the whole `caddy-dns` set rather than a curated subset. A curated subset
// turns "my provider is not supported" into a feature request that cannot be
// answered without a release, and binary size is much the cheaper side of that
// trade.
//
// The names are Caddy's own module identifiers, not ours: they appear verbatim
// in the rendered `dns <provider>` directive.
//
// **This list has a second copy, and that is a defect to close rather than a
// fact to live with.** The authoritative set is whatever the packaging's xcaddy
// invocation actually linked, and the binary can be asked — `caddy
// list-modules` prints its `dns.providers.*`. It cannot be asked from here,
// because Validate is pure by design (validate.go) and the schema's enum is
// built at reflection time with no binary in reach. So the list is static, and
// the packaging must carry a check that compares it against `caddy
// list-modules` and fails the build on a mismatch. Without that check the
// failure mode is the worst kind: a provider the operator selects from our own
// published enum, accepted by our own validator, and then rejected at runtime
// by a Caddy that never had it.
var providers = []string{
	"acmedns", "alidns", "autodns", "azure", "bunny", "civo", "cloudflare",
	"cloudns", "ddnss", "desec", "digitalocean", "directadmin", "dnsimple",
	"dnsmadeeasy", "dnspod", "dnsupdate", "domainnameshop", "duckdns", "dynu",
	"dynv6", "easydns", "exoscale", "gandi", "gcore", "glesys", "godaddy",
	"googleclouddns", "he", "hetzner", "hexonet", "infomaniak", "inwx", "ionos",
	"leaseweb", "linode", "loopia", "luadns", "mailinabox", "metaname",
	"mythicbeasts", "namecheap", "namedotcom", "namesilo", "netcup", "netlify",
	"nfsn", "njalla", "openstack-designate", "ovh", "porkbun", "powerdns",
	"rfc2136", "route53", "scaleway", "selectel", "tencentcloud", "transip",
	"vercel", "vultr", "westcn", "zonomi",
}

// Providers returns the supported provider identifiers, sorted and unique.
//
// Exposed as a function rather than the slice so that no caller can append to
// the list the validator checks against, and so that `olr ingress providers`,
// the validator and the schema's enum have one source (design.md §3.2 rule 3).
func Providers() []string {
	out := slices.Clone(providers)
	slices.Sort(out)
	return slices.Compact(out)
}
