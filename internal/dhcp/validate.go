package dhcp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

// Validation is the highest-value mechanism in the whole apply story
// (design.md §5.3.1). There is no cross-module transaction, so nothing unwinds
// a bad change — but atomic *validation* is cheap where atomic *apply* is not,
// because validation is pure reads and needs no coordination. Catching a pool
// that has drifted outside its interface's subnet before anything is written
// prevents most of the breakage a rollback would have had to repair.

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
//
// The distinction earns its keep: a reservation inside a dynamic range is a
// real hazard but dnsmasq permits it and some people rely on it, so refusing
// would be us overruling the operator on their own network. Warnings say so
// and get out of the way.
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
	return fmt.Errorf("invalid dhcp configuration:\n  %w", errors.Join(msgs...))
}

// ownedDirectives are the dnsmasq directives this module renders itself.
//
// The escape hatch (Config.ExtraConf) is additive by design. Letting it set one
// of these would let the rendered file contradict the config that produced it,
// which defeats the single-source rule the escape hatch exists to preserve —
// the whole point is that unusual settings stay revisioned rather than being
// hand-edited into the daemon's file.
var ownedDirectives = map[string]string{
	"port":             "DNS is disabled unconditionally; it belongs to the dns module (design.md §4.2)",
	"interface":        "set by the pool's interface field",
	"except-interface": "managed with interface",
	"bind-interfaces":  "managed with interface",
	"dhcp-range":       "set by the pool's start, end, lease_time and ra fields",
	"dhcp-host":        "use reservations",
	"dhcp-hostsfile":   "reservations are rendered into the module's hosts directory",
	"dhcp-hostsdir":    "reservations are rendered into the module's hosts directory",
	"dhcp-optsfile":    "use the pool's options field",
	"dhcp-optsdir":     "use the pool's options field",
	"dhcp-leasefile":   "the lease database location is fixed",
	"dhcp-script":      "reserved for publishing leases to the dns module",
	"conf-file":        "the module owns this file; including another would hide configuration from olr",
	"conf-dir":         "the module owns this file; including another would hide configuration from olr",
	"pid-file":         "managed with the service unit",
	"user":             "managed with the service unit",
	"group":            "managed with the service unit",
}

// renderedOptions are the DHCP options with a dedicated config field. Setting
// one through Pool.Options as well would emit two dhcp-option lines for the
// same tag, and which one dnsmasq honours is not something we want to depend on.
var renderedOptions = map[string]string{
	"3": "gateway", "router": "gateway",
	"6": "dns", "dns-server": "dns",
	"15": "domain", "domain-name": "domain",
	"42": "ntp", "ntp-server": "ntp",
}

// Validate checks a config against itself and against the link module's view of
// the interfaces it names.
//
// It is pure: no files, no netlink, no root. That is what lets the entire rule
// set be table-tested and lets `olr dhcp` check a config on a laptop.
func Validate(c Config, links LinkView) Result {
	var r Result

	if c.Enabled && len(c.Pools) == 0 {
		r.warnf("pools", "DHCP is enabled but no pool is configured, so nothing will be served")
	}

	// prefixes maps an interface to the subnet its pool sits in, for the
	// reservation checks below.
	prefixes := map[string]netip.Prefix{}
	seenIface := map[string]int{}

	for i, p := range c.Pools {
		path := fmt.Sprintf("pools[%d]", i)

		if p.Interface == "" {
			r.errorf(path+".interface", "required")
			continue
		}
		if first, dup := seenIface[p.Interface]; dup {
			r.errorf(path+".interface", "interface %q already has a pool at pools[%d]; one pool per interface", p.Interface, first)
			continue
		}
		seenIface[p.Interface] = i

		prefix, ok := validatePoolRange(&r, path, p, links)
		if ok {
			prefixes[p.Interface] = prefix
		}

		if lt := p.LeaseTime; lt != 0 && lt < MinLeaseTime {
			r.errorf(path+".lease_time", "%s is below dnsmasq's two minute minimum", lt)
		}
		if !p.RA.Valid() {
			r.errorf(path+".ra", "unknown mode %q (want %v)", p.RA, RAModes())
		}
		validateOptions(&r, path, p.Options)
	}

	validateOverlaps(&r, c.Pools)
	validateReservations(&r, c, prefixes)
	validateExtraConf(&r, c.ExtraConf)

	return r
}

// validatePoolRange checks the range against the interface it is served on, and
// returns the interface prefix it belongs to.
func validatePoolRange(r *Result, path string, p Pool, links LinkView) (netip.Prefix, bool) {
	switch {
	case !p.Start.IsValid():
		r.errorf(path+".start", "required")
		return netip.Prefix{}, false
	case !p.End.IsValid():
		r.errorf(path+".end", "required")
		return netip.Prefix{}, false
	case !p.Start.Is4() || !p.End.Is4():
		// IPv6 pools are expressed through the ra field instead, which lets
		// dnsmasq derive the prefix from the interface. A literal IPv6 range
		// would have to be re-typed every time a delegated prefix changed.
		r.errorf(path, "pool ranges are IPv4; configure IPv6 with the ra field")
		return netip.Prefix{}, false
	case p.Start.Compare(p.End) > 0:
		r.errorf(path, "start %s is above end %s", p.Start, p.End)
		return netip.Prefix{}, false
	}

	info, err := links.Interface(p.Interface)
	if err != nil {
		r.errorf(path+".interface", "%v", err)
		return netip.Prefix{}, false
	}
	if !info.Adopted {
		// design.md §3.4: adopt-only. Serving DHCP on an interface the operator
		// never handed us is the exact surprise that rule forbids.
		r.errorf(path+".interface", "%q is not adopted; run `olr adopt %s` first", p.Interface, p.Interface)
		return netip.Prefix{}, false
	}
	if !info.Up {
		r.warnf(path+".interface", "%q is down; the pool is configured but will not serve until it comes up", p.Interface)
	}

	prefix, ok := info.FindPrefix(p.Start)
	if !ok {
		r.errorf(path+".start", "%s is outside every subnet on %s (%s)", p.Start, p.Interface, formatPrefixes(info.Prefixes))
		return netip.Prefix{}, false
	}
	if !prefix.Contains(p.End) {
		r.errorf(path+".end", "%s is outside %s, the subnet the range starts in; a pool cannot span subnets", p.End, prefix)
		return netip.Prefix{}, false
	}

	// The router's own address must not be handed to a client.
	if router := prefix.Addr(); inRange(p.Start, p.End, router) {
		r.errorf(path, "the range contains %s, which is %s's own address", router, p.Interface)
	}
	if network := prefix.Masked().Addr(); inRange(p.Start, p.End, network) {
		r.errorf(path, "the range contains the network address %s", network)
	}
	if bcast, ok := broadcast(prefix); ok && inRange(p.Start, p.End, bcast) {
		r.errorf(path, "the range contains the broadcast address %s", bcast)
	}

	if p.Gateway != nil {
		switch {
		case !p.Gateway.IsValid():
			r.errorf(path+".gateway", "invalid address")
		case !prefix.Contains(*p.Gateway):
			r.errorf(path+".gateway", "%s is outside %s, so clients could not reach it", *p.Gateway, prefix)
		}
	}
	for j, dns := range p.DNS {
		if !dns.IsValid() {
			r.errorf(fmt.Sprintf("%s.dns[%d]", path, j), "invalid address")
		}
	}
	for j, ntp := range p.NTP {
		if !ntp.IsValid() {
			r.errorf(fmt.Sprintf("%s.ntp[%d]", path, j), "invalid address")
		}
	}

	return prefix, true
}

// validateOverlaps rejects two pools handing out the same address. Ranges on
// different interfaces cannot legitimately overlap either — if they do, the
// subnets themselves collide and routing is already broken.
func validateOverlaps(r *Result, pools []Pool) {
	for i := range pools {
		for j := i + 1; j < len(pools); j++ {
			a, b := pools[i], pools[j]
			if !a.Start.IsValid() || !a.End.IsValid() || !b.Start.IsValid() || !b.End.IsValid() {
				continue
			}
			if a.Start.Compare(b.End) <= 0 && b.Start.Compare(a.End) <= 0 {
				r.errorf(fmt.Sprintf("pools[%d]", j),
					"range %s-%s overlaps pools[%d] (%s) which serves %s-%s",
					b.Start, b.End, i, a.Interface, a.Start, a.End)
			}
		}
	}
}

func validateReservations(r *Result, c Config, prefixes map[string]netip.Prefix) {
	seenMAC := map[string]int{}
	seenIP := map[netip.Addr]int{}

	for i, res := range c.Reservations {
		path := fmt.Sprintf("reservations[%d]", i)

		mac, err := NormalizeMAC(res.MAC)
		if err != nil {
			r.errorf(path+".mac", "%v", err)
		} else if first, dup := seenMAC[mac]; dup {
			r.errorf(path+".mac", "%s is already reserved at reservations[%d]", mac, first)
		} else {
			seenMAC[mac] = i
		}

		if !res.IP.IsValid() {
			r.errorf(path+".ip", "required")
		} else if !res.IP.Is4() {
			r.errorf(path+".ip", "reservations are IPv4; IPv6 clients are addressed by the ra field")
		} else {
			if first, dup := seenIP[res.IP]; dup {
				r.errorf(path+".ip", "%s is already reserved at reservations[%d]", res.IP, first)
			} else {
				seenIP[res.IP] = i
			}
			validateReservationSubnet(r, path, c, res, prefixes)
		}

		if res.Hostname != "" {
			if err := validateHostname(res.Hostname); err != nil {
				r.errorf(path+".hostname", "%v", err)
			}
		}
		if lt := res.LeaseTime; lt != 0 && lt < MinLeaseTime {
			r.errorf(path+".lease_time", "%s is below dnsmasq's two minute minimum", lt)
		}
	}
}

// validateReservationSubnet enforces dnsmasq's own rule — a dhcp-host address
// must share a subnet with some dhcp-range, though it need not be inside the
// range itself.
func validateReservationSubnet(r *Result, path string, c Config, res Reservation, prefixes map[string]netip.Prefix) {
	for iface, prefix := range prefixes {
		if !prefix.Contains(res.IP) {
			continue
		}
		if pool, ok := c.Pool(iface); ok && inRange(pool.Start, pool.End, res.IP) {
			// Permitted by dnsmasq, and it does honour the reservation. But the
			// address is also in the pool it hands out from, so the margin for
			// error is one dnsmasq bug wide. Say so; do not refuse.
			r.warnf(path+".ip",
				"%s is inside %s's dynamic range (%s-%s); reserving an address outside the range removes any chance of a collision",
				res.IP, iface, pool.Start, pool.End)
		}
		return
	}
	r.errorf(path+".ip", "%s is not in the subnet of any configured pool, so dnsmasq would never offer it", res.IP)
}

func validateOptions(r *Result, path string, options []Option) {
	for j, o := range options {
		p := fmt.Sprintf("%s.options[%d]", path, j)
		name := strings.TrimSpace(strings.ToLower(o.Option))
		switch {
		case name == "":
			r.errorf(p+".option", "required")
		case strings.ContainsAny(name, ",="):
			r.errorf(p+".option", "%q may not contain a comma or an equals sign", o.Option)
		default:
			if field, owned := renderedOptions[strings.TrimPrefix(name, "option:")]; owned {
				r.errorf(p+".option", "option %q is rendered from the pool's %s field; set that instead", o.Option, field)
			}
		}
		if o.Value == "" {
			r.errorf(p+".value", "required")
		}
		if strings.ContainsAny(o.Value, "\n\r") {
			r.errorf(p+".value", "may not contain a newline")
		}
	}
}

func validateExtraConf(r *Result, extra string) {
	if extra == "" {
		return
	}
	for i, line := range strings.Split(extra, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		directive := strings.ToLower(strings.TrimSpace(trimmed))
		if eq := strings.IndexByte(directive, '='); eq >= 0 {
			directive = strings.TrimSpace(directive[:eq])
		}
		directive = strings.TrimPrefix(directive, "--")
		if why, owned := ownedDirectives[directive]; owned {
			r.errorf(fmt.Sprintf("extra_dnsmasq_conf line %d", i+1),
				"%q is set by the dhcp module: %s", directive, why)
		}
	}
}

// validateHostname applies the RFC 1123 label rules dnsmasq will apply anyway,
// so the failure surfaces here with a field path rather than in the daemon's
// startup log.
func validateHostname(h string) error {
	if len(h) > 63 {
		return fmt.Errorf("%q is longer than 63 characters", h)
	}
	if strings.HasPrefix(h, "-") || strings.HasSuffix(h, "-") {
		return fmt.Errorf("%q may not start or end with a hyphen", h)
	}
	for _, c := range h {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-':
		default:
			return fmt.Errorf("%q contains %q; hostnames may use only letters, digits and hyphens", h, string(c))
		}
	}
	return nil
}

// inRange reports whether addr falls within [start, end] inclusive.
func inRange(start, end, addr netip.Addr) bool {
	if !start.IsValid() || !end.IsValid() || !addr.IsValid() {
		return false
	}
	return start.Compare(addr) <= 0 && addr.Compare(end) <= 0
}

// broadcast returns the IPv4 broadcast address of a prefix.
func broadcast(p netip.Prefix) (netip.Addr, bool) {
	p = p.Masked()
	if !p.Addr().Is4() {
		return netip.Addr{}, false
	}
	b := p.Addr().As4()
	host := uint(32 - p.Bits())
	// A shift of 32 yields 0 in Go, so 1<<32-1 is the all-ones mask a /0 wants.
	v := binary.BigEndian.Uint32(b[:]) | (uint32(1)<<host - 1)
	binary.BigEndian.PutUint32(b[:], v)
	return netip.AddrFrom4(b), true
}

func formatPrefixes(prefixes []netip.Prefix) string {
	if len(prefixes) == 0 {
		return "it has no addresses"
	}
	parts := make([]string, len(prefixes))
	for i, p := range prefixes {
		parts[i] = p.String()
	}
	return strings.Join(parts, ", ")
}
