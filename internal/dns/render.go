package dns

import (
	"fmt"
	"io/fs"
	"net/netip"
	"path/filepath"
	"slices"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/dnsrelay"
)

// Paths locates everything the module writes or tells its backends about.
//
// A struct rather than a set of constants so tests can render into a temporary
// directory and still get files whose internal references are self-consistent —
// the rendered files name these paths, so overriding them halfway would produce
// a config that pointed at the real system.
type Paths struct {
	// UnboundConf is the resolver's configuration.
	UnboundConf string

	// RelayConf is the relay's, holding everything that costs a restart.
	RelayConf string

	// PolicyDir holds one file per policy. Re-read on SIGHUP, which is what
	// makes editing a blocklist free.
	PolicyDir string

	// HijackNFT is the nftables ruleset the relay's unit loads once the relay
	// is bound.
	HijackNFT string

	// TrustAnchor is unbound's root key, which it rewrites as the root KSK
	// rolls. Ours rather than the distro's: /var/lib/unbound belongs to
	// whatever the operator installed unbound for, and writing into it would be
	// squatting shared state (design.md §3.4).
	TrustAnchor string

	// ObserveSocket is where the relay serves what it saw. In the module's own
	// runtime subdirectory, not /run/olr itself: systemd deletes a
	// RuntimeDirectory when its unit stops, so naming /run/olr would make
	// stopping the relay delete olrd's control socket with it.
	ObserveSocket string
}

// DefaultPaths is the on-disk layout for a real install.
func DefaultPaths() Paths { return RootedPaths("") }

// RootedPaths is the same layout relocated under root.
//
// An empty root gives the real one. A non-empty root is for development: it
// lets olrd run as an ordinary user against a scratch directory, so the whole
// render-plan-apply path can be exercised without root or systemd.
func RootedPaths(root string) Paths {
	rendered := filepath.Join(root, "/etc/open-linux-router/rendered/dns")
	return Paths{
		UnboundConf:   filepath.Join(rendered, "unbound.conf"),
		RelayConf:     filepath.Join(rendered, "relay.json"),
		PolicyDir:     filepath.Join(rendered, "policy.d"),
		HijackNFT:     filepath.Join(rendered, "hijack.nft"),
		TrustAnchor:   filepath.Join(root, "/var/lib/open-linux-router/dns/root.key"),
		ObserveSocket: filepath.Join(root, "/run/olr/dns/observe.sock"),
	}
}

// File is one rendered file.
type File struct {
	// Path is absolute.
	Path string
	// Mode is the file's permissions.
	Mode fs.FileMode
	// Data is the full contents.
	Data []byte
	// Reloadable reports whether the owning backend picks up a change without
	// a restart.
	Reloadable bool
	// Unit names the backend this file belongs to, so the planner knows which
	// of the two to signal. A file nothing reads has an empty Unit.
	Unit string
}

// Rendered is the complete set of files a config produces. Always sorted by
// path, because drift detection compares rendered output against what is on
// disk and a stable order is what makes that comparison meaningful.
type Rendered struct {
	Files []File
}

// Get returns a file by path.
func (r Rendered) Get(path string) (File, bool) {
	i := slices.IndexFunc(r.Files, func(f File) bool { return f.Path == path })
	if i < 0 {
		return File{}, false
	}
	return r.Files[i], true
}

// Paths lists the rendered paths, in order.
func (r Rendered) Paths() []string {
	out := make([]string, len(r.Files))
	for i, f := range r.Files {
		out[i] = f.Path
	}
	return out
}

func (r *Rendered) add(f File) { r.Files = append(r.Files, f) }

func (r *Rendered) sort() {
	slices.SortFunc(r.Files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
}

// Backend renders a config into its files and names the units that run them.
//
// Two units, not one, and that is the module's real shape: unbound resolves,
// and our relay owns :53 in front of it (docs/dns.md §4). There is no interface
// here for the same reason internal/dhcp has none for dnsmasq — there is
// exactly one implementation, and an interface would have to carry reloadable(),
// which is a question about *these* files and means nothing to anything else.
type Backend struct {
	Paths Paths

	// Source is the intent file named in every generated file's ownership
	// header. An operator who finds one of these needs pointing at the file
	// that actually produced it.
	Source string
}

// NewBackend returns a backend writing to the given layout.
func NewBackend(p Paths) Backend { return Backend{Paths: p, Source: core.ConfigPath} }

// WithSource names the intent file in generated headers.
func (b Backend) WithSource(path string) Backend {
	if path != "" {
		b.Source = path
	}
	return b
}

// Name identifies the backend pair in status output.
func (Backend) Name() string { return "unbound + olr-dnsd" }

// ResolverUnit supervises unbound.
//
// Deliberately not the distro's unbound.service. That unit and /etc/unbound
// belong to whatever the operator installed unbound for; taking them over would
// be squatting shared state (design.md §3.4), and our instance answers on
// loopback:5353 rather than :53 anyway, so the two can coexist.
func (Backend) ResolverUnit() string { return "olr-dns.service" }

// RelayUnit supervises our own relay.
func (Backend) RelayUnit() string { return "olr-dnsd.service" }

// Units lists both, resolver first — the order they have to come up in, since
// a relay whose upstream is not answering serves SERVFAIL to the whole house.
func (b Backend) Units() []string { return []string{b.ResolverUnit(), b.RelayUnit()} }

// header is the ownership banner every generated file carries (design.md §7).
func (b Backend) header(what, comment string) string {
	source := b.Source
	if source == "" {
		source = core.ConfigPath
	}
	return fmt.Sprintf(`%[2]s %[1]s
%[2]s
%[2]s Generated by open-linux-router from
%[2]s   %[3]s
%[2]s Do not edit: this file is rewritten on every apply and your changes will be
%[2]s lost. Use `+"`olr dns set`"+`, or the extra_unbound_conf field for settings
%[2]s olr does not model.
`, what, comment, source)
}

// Canonical reduces a rendered file to what its backend actually reads:
// comments and blank lines removed.
//
// This is what stops a comment from being a config change. Much of what we
// render is the ownership header and the explanations around each directive,
// all of it string literals in this file — so without normalisation, editing
// one of those in a new olr release would mark every deployed box as drifted
// and schedule a restart of its resolver. The file still gets rewritten; it
// just stops being a reason to signal a daemon.
//
// It handles both comment characters we emit, "#" for unbound and nftables and
// none at all for JSON, which has no comments to strip.
func (Backend) Canonical(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	kept := lines[:0]
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		kept = append(kept, trimmed)
	}
	return []byte(strings.Join(kept, "\n"))
}

// reloadable reports whether a path sits in the directory the relay re-reads on
// SIGHUP. Used by the planner to tell a reload from a restart, including for
// stray files no longer rendered by the current config.
func (b Backend) reloadable(path string) bool {
	return strings.HasPrefix(path, b.Paths.PolicyDir+"/")
}

// unitFor reports which backend reads a path, for a file the current config no
// longer renders. A stale policy file still has to be deleted *and* signalled.
func (b Backend) unitFor(path string) string {
	switch {
	case path == b.Paths.UnboundConf:
		return b.ResolverUnit()
	case path == b.Paths.RelayConf, path == b.Paths.HijackNFT, b.reloadable(path):
		return b.RelayUnit()
	}
	return ""
}

// Render is pure: same config and same link facts, same bytes.
func (b Backend) Render(c Config, links LinkView) (Rendered, error) {
	c = c.Clone()
	c.Normalize()

	var out Rendered

	unbound, err := b.renderUnbound(c)
	if err != nil {
		return Rendered{}, err
	}
	out.add(File{Path: b.Paths.UnboundConf, Mode: 0o644, Data: unbound, Unit: b.ResolverUnit()})

	relay, err := b.renderRelay(c, links)
	if err != nil {
		return Rendered{}, err
	}
	out.add(File{Path: b.Paths.RelayConf, Mode: 0o644, Data: relay, Unit: b.RelayUnit()})

	for _, p := range c.Policies {
		data, err := b.renderPolicy(p)
		if err != nil {
			return Rendered{}, err
		}
		out.add(File{
			Path: b.Paths.PolicyDir + "/" + policyFile(p.Name),
			Mode: 0o644,
			Data: data,
			// The whole reason policies live in a directory: the relay re-reads
			// it on SIGHUP, so adding a blocked name never interrupts anybody's
			// resolution. This mirrors internal/dhcp's hosts.d exactly.
			Reloadable: true,
			Unit:       b.RelayUnit(),
		})
	}

	if c.Hijack.Enabled {
		nft, err := b.renderHijack(c)
		if err != nil {
			return Rendered{}, err
		}
		// Owned by the relay's unit, which loads it once the relay is bound and
		// tears it down when it stops. Tying the two together is the point:
		// redirecting the network's :53 at a relay that is not listening would
		// black-hole DNS for everyone.
		out.add(File{Path: b.Paths.HijackNFT, Mode: 0o644, Data: nft, Unit: b.RelayUnit()})
	}

	out.sort()
	return out, nil
}

// --- unbound ---------------------------------------------------------------

// rebindPrefixes are the ranges unbound refuses to see in an answer for a
// public name.
//
// This is anti-DNS-rebinding: a name on the internet resolving to an address on
// your LAN is how a web page reaches a device it should not be able to. unbound
// configures none of these by default, so leaving the list out would mean
// leaving the defence off.
//
// 198.18.0.0/15 is deliberately absent, and that absence is load-bearing rather
// than an oversight. It is the benchmarking range proxies use for fake-IP
// answers, and docs/dns.md's domain-routing story depends on those answers
// reaching the client intact. Adding it here would make domain routing fail
// with no error anywhere — the proxy would mint a fake IP and unbound would
// silently strip it. Whoever adds "the missing one" to this list breaks that,
// so: it is missing on purpose.
var rebindPrefixes = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.0.0/16",
	"fd00::/8",
	"fe80::/10",
}

func (b Backend) renderUnbound(c Config) ([]byte, error) {
	var s strings.Builder
	s.WriteString(b.header("unbound configuration for the olr dns module", "#"))

	fmt.Fprintf(&s, `
server:
    # Loopback only, and on %d rather than 53. olr's own relay owns :53 so that
    # every query arrives with the client's address attached; unbound sits
    # behind it doing the parts we have no business writing — recursion,
    # DNSSEC and caching (docs/dns.md §4). A consequence worth knowing: this
    # resolver is unreachable from the network, by construction rather than by
    # firewall rule.
    interface: 127.0.0.1@%[1]d
    interface: ::1@%[1]d
    access-control: 0.0.0.0/0 refuse
    access-control: ::/0 refuse
    access-control: 127.0.0.0/8 allow
    access-control: ::1 allow

    do-ip4: yes
    do-ip6: yes
    do-udp: yes
    do-tcp: yes
`, ResolverPort)

	s.WriteString(`
    # systemd supervises the process and captures stderr into the journal
    # (design.md §3.4 — no log file of our own, no pid file to go stale).
    # Privileges are dropped by the unit, not here.
    username: ""
    chroot: ""
    pidfile: ""
    use-syslog: no
    logfile: ""
    log-time-ascii: yes
    verbosity: 0

    hide-identity: yes
    hide-version: yes
    harden-glue: yes
    harden-dnssec-stripped: yes
    harden-below-nxdomain: yes

    # Send only as much of the name as each server needs. It costs nothing and
    # it is most of what recursing yourself buys over forwarding.
    qname-minimisation: yes
    prefetch: yes
`)

	fmt.Fprintf(&s, `
    # DNSSEC. The anchor is ours rather than the distro's: /var/lib/unbound
    # belongs to whatever the operator installed unbound for, and unbound
    # rewrites this file as the root key rolls, so sharing it would be writing
    # into somebody else's state (design.md §3.4). The unit bootstraps it with
    # unbound-anchor before starting, which falls back to a built-in copy when
    # the network is not up yet.
    auto-trust-anchor-file: "%s"
`, b.Paths.TrustAnchor)

	s.WriteString(`
    # Anti-rebinding: a public name must not resolve to an address on this LAN.
    # See rebindPrefixes in render.go for the one range deliberately missing.
`)
	for _, p := range rebindPrefixes {
		fmt.Fprintf(&s, "    private-address: %s\n", p)
	}

	if err := b.renderForward(&s, c.Upstream); err != nil {
		return nil, err
	}

	if extra := strings.TrimRight(c.ExtraConf, "\n"); extra != "" {
		s.WriteString(`
# --- extra_unbound_conf ---
# Passed through verbatim from the module config (design.md §3.2 rule 5). It is
# revisioned with everything else, which is the point: unusual settings stay
# visible to olr instead of being hand-edited into a file olr does not read.
`)
		s.WriteString(extra)
		s.WriteString("\n")
	}

	return []byte(s.String()), nil
}

func (b Backend) renderForward(s *strings.Builder, u Upstream) error {
	if u.Mode.OrDefault() != ModeForward {
		s.WriteString(`
# Recursing from the root: no forward-zone at all. Nobody upstream sees the
# whole picture of what this network looks up, and there is no forwarder whose
# outage would be ours.
`)
		return nil
	}
	if len(u.Servers) == 0 {
		return fmt.Errorf("upstream mode is %q but no servers are configured", ModeForward)
	}

	s.WriteString(`
# Forwarding everything upstream. The trade against recursing is stated in the
# schema; what matters here is that whoever is listed below sees every name this
# network resolves.
forward-zone:
    name: "."
`)
	if u.TLS {
		s.WriteString(`    # DoT. Without tls_name this encrypts the query and does not
    # authenticate who answers it, which is why validate warns.
    forward-tls-upstream: yes
`)
	}
	for _, srv := range u.Servers {
		fmt.Fprintf(s, "    forward-addr: %s\n", formatForwardAddr(srv, u))
	}
	return nil
}

// DefaultDoTPort is where DNS-over-TLS lives.
const DefaultDoTPort = 853

// formatForwardAddr writes one forward-addr, in unbound's addr@port#name form.
//
// A server given without a port gets the right one for the transport, which is
// the difference between "1.1.1.1 with tls on" working and silently trying DoT
// against plaintext :53.
func formatForwardAddr(srv netip.AddrPort, u Upstream) string {
	port := srv.Port()
	if port == 0 {
		port = 53
		if u.TLS {
			port = DefaultDoTPort
		}
	}
	out := fmt.Sprintf("%s@%d", srv.Addr(), port)
	if u.TLS && u.TLSName != "" {
		out += "#" + u.TLSName
	}
	return out
}

// --- the relay -------------------------------------------------------------

func (b Backend) renderRelay(c Config, links LinkView) ([]byte, error) {
	allow := c.AllowFrom
	if len(allow) == 0 {
		// Derived, never defaulted open. An empty allow_from means "the
		// networks I am listening on", which is what an operator means and is
		// the only reading that cannot accidentally ship an amplifier.
		allow = LANPrefixes(links, c.Listen)
	}

	cfg := dnsrelay.Config{
		Listen:          c.Listen,
		AllowFrom:       allow,
		Upstream:        DefaultResolver,
		PolicyDir:       b.Paths.PolicyDir,
		ObserveSocket:   b.Paths.ObserveSocket,
		QueryLogEntries: 0,
	}
	if c.QueryLog.Enabled {
		cfg.QueryLogEntries = c.QueryLog.EntriesOrDefault()
	}

	data, err := dnsrelay.MarshalConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("rendering relay configuration: %w", err)
	}
	// JSON has no comments, so the ownership header cannot go in the file. The
	// relay's unit and this module's `olr dns show` both name the source
	// instead, and the file is under a directory called "rendered".
	return append(data, '\n'), nil
}

func (b Backend) renderPolicy(p Policy) ([]byte, error) {
	data, err := dnsrelay.MarshalPolicy(dnsrelay.Policy{
		Name:     p.Name,
		Clients:  p.Clients,
		Block:    p.Block,
		Allow:    p.Allow,
		Response: string(p.Response.OrDefault()),
	})
	if err != nil {
		return nil, fmt.Errorf("rendering policy %q: %w", p.Name, err)
	}
	return append(data, '\n'), nil
}

// policyFile turns a policy name into a filename. Validate constrains the name
// to a slug, so this is a rename rather than an escape.
func policyFile(name string) string { return name + ".json" }

// --- nftables --------------------------------------------------------------

// HijackTable is the nftables table this module owns.
//
// One table, ours alone. design.md §4.2: "nftables → each module writes its own
// table, never a shared ruleset". That is what lets the relay's unit flush and
// recreate the whole thing on every start without touching a rule anybody else
// wrote.
const HijackTable = "olr-dns"

func (b Backend) renderHijack(c Config) ([]byte, error) {
	v4, hasV4 := c.RedirectTarget(false)
	v6, hasV6 := c.RedirectTarget(true)
	if !hasV4 && !hasV6 {
		return nil, fmt.Errorf("hijack is enabled but there is no listen address to redirect to")
	}
	if len(c.Hijack.Interfaces) == 0 {
		return nil, fmt.Errorf("hijack is enabled but names no interfaces")
	}

	ifaces := "{ " + strings.Join(quoteAll(c.Hijack.Interfaces), ", ") + " }"

	var s strings.Builder
	s.WriteString(b.header("nftables ruleset for the olr dns module", "#"))

	fmt.Fprintf(&s, `
# Loaded by %s once the relay is listening, and deleted when it stops. The
# coupling is deliberate: pointing the whole network's DNS at an address nothing
# answers on would be a house-wide outage with no error message.
#
# Creating the table before deleting it is the idempotent form — nft refuses to
# delete a table that is not there, and this file has to survive being loaded
# twice.
table inet %[2]s
delete table inet %[2]s

table inet %[2]s {
`, b.RelayUnit(), HijackTable)

	s.WriteString(`    chain prerouting {
        type nat hook prerouting priority dstnat; policy accept;

        # DHCP option 6 is advice; this is enforcement. A device that ignored
        # it — or upgraded itself to a hardcoded public resolver, which
        # browsers and Apple devices now do by default — is not merely
        # unlogged. The proxy that was meant to route it by domain never sees a
        # name it recognises, so domain policy silently stops applying
        # (docs/dns.md §2.2).
        #
        # Traffic already addressed to us is excluded, or the rule would rewrite
        # the destination of queries that were never going anywhere else.
`)
	if hasV4 {
		fmt.Fprintf(&s, "        iifname %s ip daddr != %s udp dport 53 dnat ip to %s\n",
			ifaces, v4.Addr(), v4)
		fmt.Fprintf(&s, "        iifname %s ip daddr != %s tcp dport 53 dnat ip to %s\n",
			ifaces, v4.Addr(), v4)
	} else {
		s.WriteString("        # No IPv4 listen address, so IPv4 queries are not captured.\n")
	}
	if hasV6 {
		fmt.Fprintf(&s, "        iifname %s ip6 daddr != %s udp dport 53 dnat ip6 to %s\n",
			ifaces, v6.Addr(), v6)
		fmt.Fprintf(&s, "        iifname %s ip6 daddr != %s tcp dport 53 dnat ip6 to %s\n",
			ifaces, v6.Addr(), v6)
	} else {
		s.WriteString(`        # No IPv6 listen address, so IPv6 queries are not captured. On a
        # dual-stack network that is a real gap, not a rounding error: a client
        # that resolves over IPv6 bypasses all of this at full speed.
`)
	}
	s.WriteString("    }\n")

	if c.Hijack.BlockDoT {
		fmt.Fprintf(&s, `
    chain forward {
        type filter hook forward priority filter; policy accept;

        # DNS-over-TLS, dropped rather than rejected. A reject tells the client
        # to fall back immediately; a black hole makes it wait out a timeout and
        # then use the resolver it was given, which is the outcome we want. The
        # rude answer is the one that works (docs/dns.md §2.2).
        #
        # DoH on :443 is not addressed here. It costs an IP and SNI blocklist
        # kept current over both TCP and UDP, which is ongoing maintenance
        # rather than a rule, and it is v2 in the design.
        iifname %s tcp dport %d drop
        iifname %[1]s udp dport %[2]d drop
    }
`, ifaces, DefaultDoTPort)
	}

	s.WriteString("}\n")
	return []byte(s.String()), nil
}

func quoteAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = `"` + s + `"`
	}
	return out
}
