package link

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Problem is one validation finding, addressed by a JSON-ish path so a UI can
// attach it to the field that caused it. Mirrors internal/dhcp's and
// internal/devices' shape, and is converted to core.Problem at the view
// boundary — one renderer for every module's complaints.
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

// OK reports whether the config can be stored.
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
	return fmt.Errorf("invalid link configuration:\n  %w", errors.Join(msgs...))
}

// Validate checks the adopted set against the interfaces that exist.
//
// `observed` may be nil, in which case only the name rules are checked. That is
// not a loophole: it keeps the rule set testable without a network the way
// design.md §5.3.1 asks, and the presence rules it skips are warnings rather
// than errors anyway.
//
// What is deliberately *not* checked here is whether releasing an interface
// breaks another module — whether a pool, a resolver or an exit still names it.
// Answering that would mean importing `dhcp`, `dns` and `gateway`, inverting
// §4.1's arrow and making this module depend on every module that depends on
// it. Those three already refuse an unadopted interface with a message naming
// their own field, which is where the complaint belongs; the UI warns before
// the fact by joining the two configs it has already fetched.
func Validate(c Config, observed []Interface) Result {
	var r Result

	byName := make(map[string]Interface, len(observed))
	for _, iface := range observed {
		byName[iface.Name] = iface
	}

	for i, name := range c.Adopted {
		path := fmt.Sprintf("adopted[%d]", i)

		if problem := checkName(name); problem != "" {
			r.errorf(path, "%s", problem)
			continue
		}

		iface, present := byName[name]
		switch {
		case !present && len(observed) > 0:
			// A warning, not an error. A VLAN or a USB adapter can legitimately
			// be adopted before it exists, and refusing would mean the config
			// could not be written until the hardware was plugged in. The
			// interface list shows it as absent, which is the honest answer.
			r.warnf(path, "%q is adopted but this machine has no such interface; "+
				"it will do nothing until one appears", name)
		case present && iface.Loopback:
			r.errorf(path, "%q is the loopback interface; there are no clients on it "+
				"and nothing olr can usefully serve there", name)
		case present && !iface.Up:
			r.warnf(path, "%q is down; it can be adopted, but nothing will be served "+
				"on it until it comes up", name)
		case present && len(iface.Prefixes) == 0 && !inAnyGroup(c, name):
			// An interface with no address and no network to give it one cannot
			// carry a pool. Before groups existed this warning had to end in
			// "give it one" with nowhere to do that; now it names the command.
			r.warnf(path, "%q has no address configured and belongs to no network; "+
				"run `olr net add <name> --member %s --subnet <cidr>` to give it one", name, name)
		}
	}

	validateGroups(&r, c, byName, len(observed) > 0)

	return r
}

// inAnyGroup reports whether an interface is a member of some network, and so
// will be given an address by the next apply.
func inAnyGroup(c Config, iface string) bool {
	_, ok := c.GroupFor(iface)
	return ok
}

// validateGroups checks the networks against themselves and against the
// interfaces that exist.
//
// Almost none of it needs the kernel: a subnet, a router address inside it and
// a member that has been adopted are all answerable from the document alone.
// That is the property the whole change exists to create — `dhcp` gets to
// validate a range against stored intent instead of against an observation, and
// this is the module where that intent becomes checkable.
func validateGroups(r *Result, c Config, observed map[string]Interface, haveObserved bool) {
	member := map[string]int{}

	for i, g := range c.Groups {
		path := fmt.Sprintf("groups[%d]", i)

		if problem := checkGroupName(g.Name); problem != "" {
			r.errorf(path+".name", "%s", problem)
			continue
		}

		switch {
		case len(g.Members) == 0:
			r.errorf(path+".members", "a network needs an interface to live on")
		case len(g.Members) > 1:
			// The schema allows it because §4.4 says a group has bridge members.
			// Nothing creates a bridge yet, and two interfaces in one subnet
			// without one is a broken network rather than a configured one.
			r.errorf(path+".members", "%q names %d interfaces; a network is limited to one "+
				"until bridging lands, because without a bridge the two would be separate "+
				"L2 segments sharing a subnet", g.Name, len(g.Members))
		}

		for j, m := range g.Members {
			mpath := fmt.Sprintf("%s.members[%d]", path, j)

			if problem := checkName(m); problem != "" {
				r.errorf(mpath, "%s", problem)
				continue
			}
			if !c.IsAdopted(m) {
				// design.md §3.4, adopt-only. Writing an address onto an
				// interface nobody handed us is a larger version of the exact
				// surprise that rule forbids.
				r.errorf(mpath, "%q is not adopted; run `olr adopt %s` first", m, m)
			}
			if first, dup := member[m]; dup {
				r.errorf(mpath, "%q is already a member of %q; an interface carries one network",
					m, c.Groups[first].Name)
			} else {
				member[m] = i
			}
			if iface, present := observed[m]; haveObserved {
				switch {
				case !present:
					r.warnf(mpath, "%q does not exist on this machine yet; the network is "+
						"configured but nothing is served until it appears", m)
				case iface.Loopback:
					r.errorf(mpath, "%q is the loopback interface and cannot carry a network", m)
				}
			}
		}

		validateGroupIPv4(r, path, g)
	}

	validateSubnetOverlap(r, c.Groups)
}

// validateGroupIPv4 checks a network's addressing against itself.
func validateGroupIPv4(r *Result, path string, g Group) {
	if g.IPv4 == nil {
		// Legitimate: a network that serves only RA. It is worth remarking on
		// because it is far more often a half-finished config than a choice.
		r.warnf(path+".ipv4", "%q has no IPv4 subnet, so it serves no IPv4 addresses", g.Name)
		return
	}

	v4 := *g.IPv4
	switch {
	case !v4.Subnet.IsValid():
		r.errorf(path+".ipv4.subnet", "required, as a network is defined by its subnet")
		return
	case !v4.Subnet.Addr().Is4():
		r.errorf(path+".ipv4.subnet", "%s is IPv6; the ipv4 block takes an IPv4 subnet", v4.Subnet)
		return
	case v4.Subnet.Bits() > 30:
		// /31 and /32 have no host addresses, so there is no router address to
		// assign and nothing for DHCP to hand out.
		r.errorf(path+".ipv4.subnet", "%s has no assignable addresses; a network needs /30 or larger",
			v4.Subnet)
		return
	}

	router := v4.RouterAddr()
	if !router.IsValid() {
		r.errorf(path+".ipv4.router", "%s has no usable router address", v4.Subnet)
		return
	}
	lo, hi, ok := core.HostRange(v4.Subnet)
	if !ok || !core.InRange(lo, hi, router) {
		// Catches the two cases a form produces: an address in a different
		// subnet entirely, and the network or broadcast address itself.
		r.errorf(path+".ipv4.router", "%s is not an assignable address in %s", router, v4.Subnet)
	}
	if v4.Subnet.Bits() < 16 {
		r.warnf(path+".ipv4.subnet", "%s is %s addresses; a prefix this large is rarely "+
			"intended on a LAN and every one of them is a host this router will ARP for",
			v4.Subnet, core.Plural(1<<(32-v4.Subnet.Bits()), "address"))
	}
}

// validateSubnetOverlap rejects two networks claiming the same addresses.
//
// Not a style rule: overlapping subnets on one box means the routing table has
// two entries that match, and which one wins is not something any surface above
// here could explain.
func validateSubnetOverlap(r *Result, groups []Group) {
	for i := range groups {
		a := groups[i]
		if a.IPv4 == nil || !a.IPv4.Subnet.IsValid() {
			continue
		}
		for j := i + 1; j < len(groups); j++ {
			b := groups[j]
			if b.IPv4 == nil || !b.IPv4.Subnet.IsValid() {
				continue
			}
			if a.IPv4.Subnet.Overlaps(b.IPv4.Subnet) {
				r.errorf(fmt.Sprintf("groups[%d].ipv4.subnet", j),
					"%s overlaps %s on %q; two networks cannot share addresses",
					b.IPv4.Subnet, a.IPv4.Subnet, a.Name)
			}
		}
	}
}

// checkGroupName returns why a string cannot name a network, or "".
//
// Stricter than an interface name, and for a different reason: this one is
// operator-chosen, appears in URLs and in dnsmasq tag names, and gets typed on
// a command line. Letters, digits and hyphens keep it usable in all three
// without any escaping anywhere.
func checkGroupName(name string) string {
	switch {
	case strings.TrimSpace(name) == "":
		return "a network needs a name"
	case name != strings.TrimSpace(name):
		return fmt.Sprintf("%q has leading or trailing whitespace", name)
	case len(name) > MaxGroupNameLen:
		return fmt.Sprintf("%q is longer than %d characters", name, MaxGroupNameLen)
	case strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-"):
		return fmt.Sprintf("%q may not start or end with a hyphen", name)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		case r >= 'A' && r <= 'Z':
			return fmt.Sprintf("%q has a capital letter; network names are lowercase so that "+
				"`guest` and `Guest` cannot be two networks", name)
		default:
			return fmt.Sprintf("%q contains %q; a network name may use lowercase letters, "+
				"digits and hyphens", name, string(r))
		}
	}
	return ""
}

// checkName returns why a name cannot be an interface, or "".
func checkName(name string) string {
	switch {
	case strings.TrimSpace(name) == "":
		return "an interface name cannot be empty"
	case name != strings.TrimSpace(name):
		return fmt.Sprintf("%q has leading or trailing whitespace", name)
	case utf8.RuneCountInString(name) > MaxInterfaceNameLen:
		return fmt.Sprintf("%q is longer than %d characters, so it cannot name an interface",
			name, MaxInterfaceNameLen)
	case name == "." || name == "..":
		return fmt.Sprintf("%q is not an interface name", name)
	case strings.ContainsRune(name, '/'):
		return fmt.Sprintf("%q contains a slash, which an interface name cannot", name)
	}
	for _, r := range name {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Sprintf("%q contains whitespace or a control character (%q)", name, r)
		}
	}
	return ""
}

// problems converts this module's findings into core's wire shape, so that
// every module reports a bad field the same way.
func problems(in []Problem) []core.Problem {
	if len(in) == 0 {
		return nil
	}
	out := make([]core.Problem, 0, len(in))
	for _, p := range in {
		out = append(out, core.Problem{Path: p.Path, Message: p.Message})
	}
	return out
}
