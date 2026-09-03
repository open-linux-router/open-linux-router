package dns

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

// Validation is the highest-value mechanism in the whole apply story
// (design.md §5.3.1). There is no cross-module transaction, so nothing unwinds
// a bad change — but atomic *validation* is cheap where atomic *apply* is not,
// because validation is pure reads and needs no coordination.
//
// For this module the stakes are unusually lopsided. A bad pool costs one
// device an address; a bad resolver costs the whole building the internet, and
// presents to every occupant not as "DNS is down" but as "everything is
// broken". So the rules below lean towards refusing rather than warning
// wherever the failure would be silent or house-wide.

// Problem is one validation finding, addressed by a JSON-ish path so a UI can
// attach it to the field that caused it.
type Problem struct {
	Path    string
	Message string
}

func (p Problem) String() string {
	if p.Path == "" {
		return p.Message
	}
	return p.Path + ": " + p.Message
}

// Result separates the fatal from the merely suspect.
type Result struct {
	Errors   []Problem
	Warnings []Problem
}

func (r *Result) errorf(path, format string, args ...any) {
	r.Errors = append(r.Errors, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
}

func (r *Result) warnf(path, format string, args ...any) {
	r.Warnings = append(r.Warnings, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
}

// OK reports whether the config can be applied.
func (r Result) OK() bool { return len(r.Errors) == 0 }

// Err collapses the errors into one, or nil.
func (r Result) Err() error {
	if r.OK() {
		return nil
	}
	msgs := make([]error, len(r.Errors))
	for i, p := range r.Errors {
		msgs[i] = errors.New(p.String())
	}
	return errors.Join(msgs...)
}

// MaxPolicyNameLen bounds a policy name. It becomes a filename, and it is a
// label in a UI table, so it is short by design rather than by accident.
const MaxPolicyNameLen = 64

// Validate checks a config against itself and against the link module's view of
// the interfaces it names.
//
// It is pure: no files, no netlink, no root. That is what lets the entire rule
// set be table-tested and lets `olr dns` check a config on a laptop.
func Validate(c Config, links LinkView) Result {
	var r Result

	validateListen(&r, c, links)
	validateAllowFrom(&r, c, links)
	validateUpstream(&r, c)
	validatePolicies(&r, c)
	validateHijack(&r, c, links)
	validateQueryLog(&r, c)
	validateExtraConf(&r, c.ExtraConf)

	return r
}

// validateListen checks where the relay would answer.
func validateListen(r *Result, c Config, links LinkView) {
	if len(c.Listen) == 0 {
		if c.Enabled {
			r.errorf("listen",
				"DNS is enabled but no listen address is configured, so nothing would answer. "+
					"Set it to the router's address on the network it serves")
		}
		return
	}

	seen := map[netip.AddrPort]bool{}
	for i, l := range c.Listen {
		path := fmt.Sprintf("listen[%d]", i)

		if !l.Addr().IsValid() {
			r.errorf(path, "invalid address")
			continue
		}
		if l.Port() == 0 {
			r.errorf(path, "a port is required; DNS is 53")
			continue
		}
		if seen[l] {
			r.errorf(path, "%s is listed twice", l)
			continue
		}
		seen[l] = true

		if l.Addr().IsUnspecified() {
			r.errorf(path,
				"%s is a wildcard, which would answer on every interface including the WAN. "+
					"Name the router's address on each network it should serve instead", l.Addr())
			continue
		}
		if l.Addr().IsLoopback() {
			if l.Port() == ResolverPort {
				r.errorf(path,
					"%d on loopback is where this module runs unbound, so the relay cannot also "+
						"have it", ResolverPort)
				continue
			}
			r.warnf(path, "%s is loopback, so only this box can resolve through it", l.Addr())
			continue
		}

		info, ok := InterfaceWithAddress(links, l.Addr())
		if !ok {
			r.errorf(path,
				"%s is not an address on any interface olr knows about, so nothing could bind it",
				l.Addr())
			continue
		}
		if !info.Adopted {
			// design.md §3.4: adopt-only. A resolver appearing on an interface
			// the operator never handed us is the exact surprise that forbids,
			// and on a WAN-facing interface it is an open resolver.
			r.errorf(path, "%s is on %q, which is not adopted; run `olr adopt %s` first",
				l.Addr(), info.Name, info.Name)
			continue
		}
		if !info.Up {
			r.warnf(path, "%s is on %q, which is down; DNS is configured but will not serve until it comes up",
				l.Addr(), info.Name)
		}
	}
}

// validateAllowFrom checks who may ask.
func validateAllowFrom(r *Result, c Config, links LinkView) {
	if len(c.AllowFrom) == 0 {
		if !c.Enabled || len(c.Listen) == 0 {
			return
		}
		// Empty is legal and means "the networks I listen on". It is only a
		// problem when that derivation comes up empty, because then the relay
		// starts and answers nobody — a silent outage that looks like a DNS bug.
		if len(LANPrefixes(links, c.Listen)) == 0 {
			r.errorf("allow_from",
				"no source networks are allowed and none could be derived from the listen "+
					"addresses, so every query would be dropped. List the networks that should "+
					"be able to resolve")
		}
		return
	}

	for i, p := range c.AllowFrom {
		path := fmt.Sprintf("allow_from[%d]", i)
		if !p.IsValid() {
			r.errorf(path, "invalid prefix")
			continue
		}
		if p.Bits() == 0 {
			// Not a warning. An open resolver is not a risk the operator takes
			// with their own network — it is a reflector pointed at somebody
			// else's, and they find out when their uplink is saturated.
			r.errorf(path,
				"%s allows the whole internet to resolve through this box, which makes it an "+
					"amplifier for attacks on other networks. List the networks that should be "+
					"able to resolve instead", p)
			continue
		}
		if p != p.Masked() {
			r.warnf(path, "%s has host bits set; it covers %s", p, p.Masked())
		}
	}
}

// validateUpstream checks how names get resolved.
func validateUpstream(r *Result, c Config) {
	u := c.Upstream
	if !u.Mode.Valid() {
		r.errorf("upstream.mode", "unknown mode %q (want %v)", u.Mode, UpstreamModes())
		return
	}

	if u.Mode.OrDefault() == ModeRecurse {
		if len(u.Servers) > 0 {
			r.warnf("upstream.servers",
				"%d server(s) are listed but the mode is %q, so they are not used", len(u.Servers), ModeRecurse)
		}
		if u.TLS {
			r.warnf("upstream.tls", "there is no forwarder to encrypt to in mode %q", ModeRecurse)
		}
		return
	}

	if len(u.Servers) == 0 {
		r.errorf("upstream.servers", "mode is %q but no servers are listed", ModeForward)
		return
	}
	if u.TLS && u.TLSName == "" {
		// Worth saying plainly, because "TLS: on" reads like it settled the
		// question and it did not.
		r.warnf("upstream.tls_name",
			"without a certificate name the connection is encrypted but the server is not "+
				"authenticated, so anything that can intercept the traffic can still answer")
	}

	listening := map[netip.Addr]bool{}
	for _, l := range c.Listen {
		listening[l.Addr()] = true
	}
	for i, srv := range u.Servers {
		path := fmt.Sprintf("upstream.servers[%d]", i)
		if !srv.Addr().IsValid() {
			r.errorf(path, "invalid address")
			continue
		}
		if listening[srv.Addr()] && (srv.Port() == 0 || srv.Port() == 53) {
			r.errorf(path,
				"%s is this box's own DNS address, so every query would be forwarded back to "+
					"itself", srv.Addr())
		}
	}
}

// validatePolicies checks what clients may look up.
func validatePolicies(r *Result, c Config) {
	seenName := map[string]int{}
	seenClient := map[netip.Prefix]int{}
	defaultAt := -1

	for i, p := range c.Policies {
		path := fmt.Sprintf("policies[%d]", i)

		switch {
		case p.Name == "":
			r.errorf(path+".name", "required")
		case len(p.Name) > MaxPolicyNameLen:
			r.errorf(path+".name", "longer than %d characters", MaxPolicyNameLen)
		case !isSlug(p.Name):
			// It becomes a filename under policy.d, so a value with a slash or
			// a dot-dot in it would be a path traversal rather than a typo.
			r.errorf(path+".name",
				"%q must be lowercase letters, digits, dashes and underscores", p.Name)
		}
		if first, dup := seenName[p.Name]; dup {
			r.errorf(path+".name", "%q is already used at policies[%d]", p.Name, first)
		} else if p.Name != "" {
			seenName[p.Name] = i
		}

		if !p.Response.Valid() {
			r.errorf(path+".response", "unknown response %q (want %v)", p.Response, BlockResponses())
		}

		if len(p.Clients) == 0 {
			if defaultAt >= 0 {
				r.errorf(path+".clients",
					"policies[%d] (%q) is already the default policy; only one policy may have no "+
						"clients, or which one applies would depend on the order they happen to be in",
					defaultAt, c.Policies[defaultAt].Name)
			} else {
				defaultAt = i
			}
		}
		for j, cl := range p.Clients {
			cpath := fmt.Sprintf("%s.clients[%d]", path, j)
			if !cl.IsValid() {
				r.errorf(cpath, "invalid prefix")
				continue
			}
			if first, dup := seenClient[cl]; dup && first != i {
				// Same prefix in two policies: the most-specific rule cannot
				// break the tie, so which one wins would be an accident.
				r.errorf(cpath, "%s is also claimed by policies[%d] (%q)", cl, first, c.Policies[first].Name)
				continue
			}
			seenClient[cl] = i
			if cl != cl.Masked() {
				r.warnf(cpath, "%s has host bits set; it covers %s", cl, cl.Masked())
			}
		}

		validateNames(r, path+".block", p.Block)
		validateNames(r, path+".allow", p.Allow)

		for _, a := range p.Allow {
			for _, blk := range p.Block {
				if a == blk {
					r.warnf(path+".allow",
						"%q is in both block and allow; allow wins, so it is not blocked", a)
				}
			}
		}
		if len(p.Block) == 0 && len(p.Allow) > 0 {
			r.warnf(path+".allow", "there is nothing to make an exception to; block is empty")
		}
	}

	if c.Enabled && len(c.Policies) > 0 && !c.Hijack.Enabled {
		// The gap docs/dns.md §2 calls the only dangerous misconfiguration:
		// blocking applies to clients that ask us, and nothing makes them.
		r.warnf("hijack.enabled",
			"policies only apply to clients that use this resolver, and nothing currently forces "+
				"them to. A device configured with a public resolver ignores every rule above")
	}
}

// validateHijack checks the enforcement half.
func validateHijack(r *Result, c Config, links LinkView) {
	h := c.Hijack
	if !h.Enabled {
		return
	}

	if len(c.Listen) == 0 {
		r.errorf("hijack.enabled",
			"there is no listen address to redirect queries to; redirecting the network's DNS at "+
				"an address nothing answers on would take resolution down for every device")
		return
	}
	if len(h.Interfaces) == 0 {
		r.errorf("hijack.interfaces",
			"required when the hijack is on. Naming no interfaces cannot mean \"all of them\" "+
				"here, because that would capture the WAN side too")
	}

	for i, name := range h.Interfaces {
		path := fmt.Sprintf("hijack.interfaces[%d]", i)
		info, err := links.Interface(name)
		if err != nil {
			r.errorf(path, "%v", err)
			continue
		}
		if !info.Adopted {
			r.errorf(path, "%q is not adopted; run `olr adopt %s` first", name, name)
		}
	}

	if _, hasV4 := c.RedirectTarget(false); !hasV4 {
		if _, hasV6 := c.RedirectTarget(true); hasV6 {
			r.warnf("listen",
				"there is no IPv4 listen address, so IPv4 queries are not captured at all")
		}
	}
	if _, hasV6 := c.RedirectTarget(true); !hasV6 {
		r.warnf("listen",
			"there is no IPv6 listen address, so a client resolving over IPv6 bypasses the "+
				"redirect entirely. On a dual-stack network that is most of the traffic")
	}

	if !h.BlockDoT {
		// Ranked by cost to defeat in docs/dns.md §2.2: plaintext :53 is cheap
		// to hijack, DoT is cheap to drop, DoH is expensive. Leaving the cheap
		// one open makes the expensive work pointless.
		r.warnf("hijack.block_dot",
			"plaintext DNS is redirected but DNS-over-TLS on 853 is not blocked, so a client can "+
				"still reach a public resolver by switching to it — which browsers and phones do "+
				"on their own")
	}
}

func validateQueryLog(r *Result, c Config) {
	if c.QueryLog.Entries < 0 {
		r.errorf("query_log.entries", "must not be negative")
	}
	// Held in the relay's memory, so this is a memory budget rather than a
	// retention policy. Said as a warning because it is the operator's box.
	const large = 200000
	if c.QueryLog.Entries > large {
		r.warnf("query_log.entries",
			"%d entries are kept in the relay's memory; a log this large is a real cost on a "+
				"small router", c.QueryLog.Entries)
	}
	if !c.QueryLog.Enabled && len(c.Policies) > 0 {
		r.warnf("query_log.enabled",
			"names are being blocked but nothing is recorded, so \"why can this device not reach "+
				"that site\" has no answer to look up")
	}
}

// deniedDirectives are unbound settings the module renders itself. Letting the
// escape hatch set one again would produce a file with two answers, and which
// one unbound honours is not something we want to depend on.
var deniedDirectives = map[string]string{
	"interface":              "the relay owns :53; unbound's address is fixed",
	"port":                   "the relay owns :53; unbound's address is fixed",
	"access-control":         "unbound is reachable from loopback only, by construction",
	"auto-trust-anchor-file": "the trust anchor location is fixed and bootstrapped by the unit",
	"trust-anchor-file":      "the trust anchor location is fixed and bootstrapped by the unit",
	"chroot":                 "managed with the service unit",
	"username":               "managed with the service unit",
	"pidfile":                "managed with the service unit",
	"logfile":                "logs go to the journal (design.md §3.4)",
	"use-syslog":             "logs go to the journal (design.md §3.4)",
	"include":                "the module owns this file; including another would hide configuration from olr",
	"include-toplevel":       "the module owns this file; including another would hide configuration from olr",
}

// forwardDirectives are the ones that would fight the upstream field.
var forwardDirectives = map[string]string{
	"forward-zone":         "use the upstream field",
	"forward-addr":         "use the upstream field",
	"forward-tls-upstream": "use the upstream field",
}

// validateExtraConf keeps the escape hatch from re-answering questions the
// module already answered.
//
// design.md §3.2 rule 5 promises a passthrough, not a free-for-all: the point is
// that unusual settings stay visible to olr, and a directive that silently
// overrode a rendered one would do the opposite.
func validateExtraConf(r *Result, extra string) {
	for i, line := range strings.Split(extra, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		directive, _, _ := strings.Cut(trimmed, ":")
		directive = strings.ToLower(strings.TrimSpace(directive))

		path := fmt.Sprintf("extra_unbound_conf:%d", i+1)
		if why, denied := deniedDirectives[directive]; denied {
			r.errorf(path, "%s is set by olr: %s", directive, why)
			continue
		}
		if why, denied := forwardDirectives[directive]; denied {
			r.errorf(path, "%s is set by olr: %s", directive, why)
		}
	}
}

// validateNames checks a blocklist.
func validateNames(r *Result, path string, names []string) {
	for i, n := range names {
		if err := checkName(n); err != nil {
			r.errorf(fmt.Sprintf("%s[%d]", path, i), "%q: %v", n, err)
		}
	}
}

// MaxNameLen is the DNS limit on a presentation-form name.
const MaxNameLen = 253

// checkName rejects things that are not domain names.
//
// Lenient about what a label may contain — underscores appear in real names and
// refusing them would be us knowing better — and strict about the shapes that
// mean somebody pasted the wrong thing, which is the actual failure mode: a URL
// in a blocklist blocks nothing and says nothing.
func checkName(n string) error {
	switch {
	case n == "":
		return errors.New("empty")
	case len(n) > MaxNameLen:
		return fmt.Errorf("longer than %d characters", MaxNameLen)
	case strings.Contains(n, "/"):
		return errors.New("looks like a URL; use just the host name")
	case strings.ContainsAny(n, " \t"):
		return errors.New("contains whitespace")
	case strings.Contains(n, ".."):
		return errors.New("has an empty label")
	case strings.HasPrefix(n, "."):
		return errors.New("starts with a dot")
	}
	for _, label := range strings.Split(n, ".") {
		switch {
		case label == "":
			return errors.New("has an empty label")
		case len(label) > 63:
			return fmt.Errorf("label %q is longer than 63 characters", label)
		case strings.HasPrefix(label, "-"), strings.HasSuffix(label, "-"):
			return fmt.Errorf("label %q starts or ends with a dash", label)
		}
		for _, c := range label {
			switch {
			case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
			default:
				return fmt.Errorf("label %q contains %q", label, c)
			}
		}
	}
	return nil
}

// isSlug reports whether a policy name is safe as a filename and readable as a
// label.
func isSlug(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}
