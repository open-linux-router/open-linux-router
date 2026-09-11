package ingress

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Unit is the systemd unit for the bundled proxy.
//
// Not `caddy.service`. The binary we ship is our build (docs/ingress.md §5.2),
// and a box that already runs the distro's Caddy must keep running it — unit
// name, binary path and config path all differ from the packaged ones, so there
// is nothing for the two installs to fight over. This is the difference between
// working and not on any machine that has ever served a web page.
const Unit = "olr-caddy.service"

// TokenEnv is the environment variable the Caddyfile reads the provider
// credential from.
//
// The indirection is the point. The Caddyfile is the file an operator reads
// when something is wrong, the file `olr ingress plan` diffs, and the file that
// ends up pasted into a forum post. The credential is in none of those because
// it is never written there — only `{env.OLR_INGRESS_DNS_TOKEN}` is.
const TokenEnv = "OLR_INGRESS_DNS_TOKEN"

// Paths locates everything the backend reads or writes.
//
// A struct rather than constants so tests can render into a temporary directory
// and still get a config whose internal references are self-consistent.
type Paths struct {
	// Conf is the Caddyfile.
	Conf string

	// Env holds the provider credential, read by the unit through
	// EnvironmentFile= and never by the Caddyfile directly.
	Env string

	// Data is Caddy's storage: the ACME account key, and the certificate
	// itself. It must survive a package upgrade and a reboot, and it must not
	// be wiped casually — an operator who deletes it is re-issuing from the CA
	// and has rate limits waiting for them.
	Data string
}

// DefaultPaths is the on-disk layout for a real install.
func DefaultPaths() Paths { return RootedPaths("") }

// RootedPaths is the same layout relocated under root, for development against
// a scratch directory without root or systemd.
func RootedPaths(root string) Paths {
	rendered := filepath.Join(root, "/etc/open-linux-router/rendered/ingress")
	return Paths{
		Conf: filepath.Join(rendered, "Caddyfile"),
		Env:  filepath.Join(rendered, "caddy.env"),
		Data: filepath.Join(root, "/var/lib/open-linux-router/ingress"),
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
	// Secret suppresses the contents on every surface that displays a file:
	// the plan diff, the drift report, the logs. The file is still written and
	// still compared byte for byte — only its *display* is withheld, because a
	// credential that is correct is no less a credential.
	Secret bool
}

// Rendered is the complete set of files a config produces, sorted by path so
// that drift detection compares like with like.
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

// Caddy renders a config into the proxy's files.
//
// There is no Backend interface, for the same reason internal/dhcp has none:
// docs/ingress.md §10 chose Caddy and rejected the alternatives on the record,
// so by §3.1's test there is exactly one implementation and nothing to iterate
// over.
type Caddy struct {
	Paths Paths

	// Source is the intent file named in every generated file's ownership
	// header. An operator who finds one of these needs to be pointed at the
	// file that produced it (design.md §3.4).
	Source string
}

// NewCaddy returns a backend writing to the given layout.
func NewCaddy(p Paths) Caddy { return Caddy{Paths: p, Source: core.ConfigPath} }

// WithSource names the intent file in generated headers.
func (c Caddy) WithSource(path string) Caddy { c.Source = path; return c }

// Render turns intent into files.
//
// Addresses are resolved here rather than stored, so the rendered file is a
// snapshot of where devices are at the moment it was written and re-rendering
// is what picks up a move. Validate has already refused any device without a
// fixed address, so in practice these do not change — but the direction of the
// dependency is what matters (§4.1), not how often it pays off.
func (c Caddy) Render(cfg Config, dns DNSView, devices DeviceView) (Rendered, error) {
	domain := strings.ToLower(strings.TrimSuffix(dns.LocalDomain(), "."))

	conf, err := c.caddyfile(cfg, domain, devices)
	if err != nil {
		return Rendered{}, err
	}

	out := Rendered{Files: []File{
		{Path: c.Paths.Conf, Mode: 0o644, Data: []byte(conf)},
		{
			Path: c.Paths.Env,
			// 0600 and not a byte looser. This is the whole of olr's secrets
			// story for now (docs/ingress.md §4.1), and calling it a stopgap
			// in a document does not excuse rendering it world-readable.
			Mode:   0o600,
			Data:   []byte(c.env(cfg)),
			Secret: true,
		},
	}}
	slices.SortFunc(out.Files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	return out, nil
}

// header is the ownership banner every generated file carries (design.md §7).
func (c Caddy) header(what string) string {
	source := c.Source
	if source == "" {
		source = core.ConfigPath
	}
	return fmt.Sprintf(`# %s
#
# Generated by open-linux-router from
#   %s
# Do not edit: this file is rewritten on every apply and your changes will be
# lost. Use `+"`olr ingress add`"+`, or the raw_caddyfile field for settings olr
# does not model.
`, what, source)
}

// Canonical reduces a rendered file to what Caddy actually reads: comments and
// blank lines removed.
//
// This is what stops a comment from being a config change, and this module
// needs it more than most — most of the Caddyfile above is explanation, all of
// it string literals in this file. Without normalisation, rewording one of
// those comments in a new olr release would mark every deployed box as drifted
// and schedule a proxy reload, which here means dropping every connection
// through it for the sake of a sentence nobody read. The file is still
// rewritten; it just stops being a reason to signal the daemon.
func Canonical(data []byte) string {
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		out = append(out, trimmed)
	}
	return strings.Join(out, "\n")
}

func (c Caddy) env(cfg Config) string {
	var b strings.Builder
	b.WriteString(c.header("environment for " + Unit))
	fmt.Fprintf(&b, "%s=%s\n", TokenEnv, cfg.Certificate.Token)
	return b.String()
}

func (c Caddy) caddyfile(cfg Config, domain string, devices DeviceView) (string, error) {
	var b strings.Builder
	b.WriteString(c.header("Caddyfile for " + Unit))

	c.globals(&b, cfg)

	if !cfg.Enabled || domain == "" {
		// Disabled keeps the configuration and stops the service (Config.Enabled),
		// so what is rendered here is a file that is valid and serves nothing —
		// never a file that fails to parse, because a disabled module must not
		// be the reason a later `caddy validate` fails for an unrelated change.
		b.WriteString("\n# ingress is disabled; no service is published.\n")
		return b.String(), nil
	}

	site := qualify("*", domain)
	fmt.Fprintf(&b, "\n%s {\n", site)
	c.tls(&b, cfg, site)

	for _, s := range cfg.Services {
		if err := c.service(&b, s, domain, devices); err != nil {
			return "", err
		}
	}

	// Anything under the wildcard that is not published closes the connection
	// rather than answering. Without this Caddy's own fallback answers 200 with
	// an empty body, which tells a scanner that the name exists and tells an
	// operator testing a typo that something is listening. `abort` says
	// neither.
	b.WriteString("\n\thandle {\n\t\tabort\n\t}\n")
	b.WriteString("}\n")

	if extra := strings.TrimSpace(cfg.ExtraConf); extra != "" {
		b.WriteString("\n# --- raw_caddyfile (design.md §3.2 rule 5) ---\n")
		b.WriteString(extra)
		b.WriteString("\n")
	}

	return b.String(), nil
}

func (c Caddy) globals(b *strings.Builder, cfg Config) {
	b.WriteString("\n{\n")

	// Caddy's admin API can mutate configuration without touching this file.
	// docs/ingress.md §7.2 rejects using it — config that lives only in a
	// running process is not revisioned and not readable over SSH during an
	// outage — and having rejected it, leaving the endpoint listening would be
	// an unauthenticated local control surface kept for nobody's benefit.
	b.WriteString("\tadmin off\n")

	if cfg.Certificate.Email != "" {
		fmt.Fprintf(b, "\temail %s\n", cfg.Certificate.Email)
	}
	b.WriteString("}\n")
}

func (c Caddy) tls(b *strings.Builder, cfg Config, site string) {
	b.WriteString("\n\t# One wildcard certificate serves every published name, which is what\n")
	b.WriteString("\t# makes publishing a service a two-field operation rather than a\n")
	b.WriteString("\t# certificate ceremony (docs/ingress.md §1).\n")
	b.WriteString("\ttls {\n")
	fmt.Fprintf(b, "\t\tdns %s {env.%s}\n", cfg.Certificate.Provider, TokenEnv)

	// The single most important line in this file, and the one whose absence
	// fails in the least obvious way. `dns` serves the local suffix as an
	// unbound `local-zone ... static` zone, so this box answers the whole zone
	// itself and forwards none of it — and the `_acme-challenge` record the
	// provider API just wrote into the *public* zone gets an authoritative
	// NXDOMAIN from our own resolver. Caddy would wait for propagation of a
	// record it can never observe, and issuance would hang forever on a box
	// whose DNS is working exactly as designed. So the propagation check asks
	// somebody else, explicitly.
	b.WriteString("\n\t\t# Ask public resolvers, never this box: olr's own resolver is\n")
	b.WriteString("\t\t# authoritative for this zone and would answer NXDOMAIN for the\n")
	b.WriteString("\t\t# challenge record, which the CA can see and we could not.\n")
	fmt.Fprintf(b, "\t\tresolvers %s\n", strings.Join(cfg.Certificate.ResolversOrDefault(), " "))
	b.WriteString("\t}\n")
}

func (c Caddy) service(b *strings.Builder, s Service, domain string, devices DeviceView) error {
	fqdn := qualify(s.Name, domain)

	resolved := ""
	if s.Upstream.Device != "" {
		info, err := devices.Device(s.Upstream.Device)
		if err != nil {
			return fmt.Errorf("service %q: %w", s.Name, err)
		}
		if !info.Addr.IsValid() {
			return fmt.Errorf("service %q: device %q has no address", s.Name, s.Upstream.Device)
		}
		resolved = info.Addr.String()
	}
	target := s.Upstream.Target(resolved)

	// A host matcher inside one wildcard site block, rather than a site block
	// per name. Separate blocks would each ask for their own certificate and
	// defeat the wildcard — the reason §1 can promise that adding a service
	// involves no certificate step is that no new name is ever requested.
	fmt.Fprintf(b, "\n\t@%s host %s\n", matcherName(s.Name), fqdn)
	fmt.Fprintf(b, "\thandle @%s {\n", matcherName(s.Name))

	if s.Upstream.Scheme.OrDefault() == SchemeHTTPS {
		fmt.Fprintf(b, "\t\treverse_proxy https://%s {\n", target)
		b.WriteString("\t\t\ttransport http {\n")
		// Verification is not attempted, deliberately. The NAS and hypervisor
		// UIs this exists for all present a self-signed certificate, and
		// demanding a valid one would make the common case impossible in
		// exchange for protection against an attacker who is already on the
		// operator's LAN, between olr and a device the operator named.
		b.WriteString("\t\t\t\ttls\n")
		b.WriteString("\t\t\t\ttls_insecure_skip_verify\n")
		b.WriteString("\t\t\t}\n")
		b.WriteString("\t\t}\n")
	} else {
		fmt.Fprintf(b, "\t\treverse_proxy %s\n", target)
	}

	b.WriteString("\t}\n")
	return nil
}

// matcherName makes a Caddy matcher label out of a service name. Names are
// already validated to letters, digits and hyphens, so this only has to avoid
// colliding with Caddy's own tokens.
func matcherName(name string) string { return "svc_" + strings.ReplaceAll(name, "-", "_") }
