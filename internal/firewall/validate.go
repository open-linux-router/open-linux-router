package firewall

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Validation is pure: no files, no netlink, no root. That is what lets the whole
// rule set be tested without a network (design.md §5.3.1), and it is why every
// check that needs the system reads it through LinkView rather than looking for
// itself.
//
// The rules here are docs/firewall.md §5.1's refusals, which are all the same
// sentence from the operator's side — *"I set up the port forward and it does
// not work"*. That is why they are checks rather than troubleshooting notes:
// each one is cheap to answer before applying and expensive to diagnose after,
// because a NAT rule that is subtly wrong does not fail, it delivers somewhere
// else.
//
// What is deliberately *not* here is anything about the running system: a
// foreign forward-chain policy and a local service on the port are facts about
// the kernel rather than about the configuration, so they belong to the plan
// (plan.go) where they can be reported against what is actually there.

// Problem is one validation finding, addressed by a JSON-ish path so a UI can
// attach it to the field that caused it. Mirrors the other modules' shape, and
// is converted to core.Problem at the view boundary, so a UI needs one renderer
// for every module's complaints rather than one per module.
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
	return fmt.Errorf("invalid firewall configuration:\n  %w", errors.Join(msgs...))
}

// Validate checks intent against itself and against link's facts.
func Validate(c Config, links LinkView) Result {
	var r Result

	seen := map[string]int{}
	slots := map[int]int{}

	for i, f := range c.Forwards {
		path := fmt.Sprintf("forwards[%d]", i)

		validateName(&r, path, f, seen, i)
		validateSlot(&r, path, f, slots, i)
		validateIn(&r, path, f, links)
		validateProtocol(&r, path, f)
		validatePorts(&r, path, f)
		validateTo(&r, path, f, links)
		validateHairpin(&r, path, f, links)
	}

	validateOverlaps(&r, c)

	return r
}

func validateName(r *Result, path string, f Forward, seen map[string]int, i int) {
	switch {
	case f.Name == "":
		r.errorf(path+".name", "a forward needs a name; it is how everything else refers to it")
	case utf8.RuneCountInString(f.Name) > MaxNameLen:
		r.errorf(path+".name", "name is %d characters; the limit is %d",
			utf8.RuneCountInString(f.Name), MaxNameLen)
	default:
		if bad, ok := hasControlChar(f.Name); ok {
			// A newline in a name would break every single-line rendering of
			// it, including the CLI's table and the nftables comment we render
			// it into.
			r.errorf(path+".name", "name contains a control character (%q)", bad)
		}
		if first, dup := seen[f.Name]; dup {
			r.errorf(path+".name",
				"%q is already a forward at forwards[%d]; forwards are referred to by name, so each appears once",
				f.Name, first)
		} else {
			seen[f.Name] = i
		}
	}
}

func validateSlot(r *Result, path string, f Forward, slots map[int]int, i int) {
	switch {
	case f.Slot == 0:
		r.errorf(path+".slot",
			"no slot could be allocated; the box is at its limit of %d forwards", MaxSlot)
	case !Slot(f.Slot).Valid():
		r.errorf(path+".slot",
			"slot %d is outside the range 1–%d that this module owns", f.Slot, MaxSlot)
	default:
		if first, dup := slots[f.Slot]; dup {
			// Only reachable from a hand-edited file — allocateSlots clears
			// duplicates — but worth a real message, because two forwards
			// sharing a slot means two forwards sharing a counter, and the
			// symptom is a number that counts twice as much traffic as it
			// should.
			r.errorf(path+".slot",
				"slot %d is already used by forwards[%d]; each forward needs its own counter",
				f.Slot, first)
		} else {
			slots[f.Slot] = i
		}
	}
}

func validateIn(r *Result, path string, f Forward, links LinkView) {
	if f.In == "" {
		r.errorf(path+".in",
			"a forward needs the interface connections arrive on, for example wan0")
		return
	}

	info, err := links.Interface(f.In)
	switch {
	case err != nil:
		r.errorf(path+".in", "%q is not an interface this box knows about", f.In)
	case !info.Adopted:
		r.errorf(path+".in",
			"%q has not been adopted; run `olr adopt %s` before forwarding a port on it",
			f.In, f.In)
	case !info.Up:
		r.warnf(path+".in", "%q is down; the forward will take effect when it comes up", f.In)
	}
}

func validateProtocol(r *Result, path string, f Forward) {
	if !f.Protocol.Valid() {
		r.errorf(path+".protocol", "unknown protocol %q; valid values are %s",
			f.Protocol, join(Protocols()))
	}
}

func validatePorts(r *Result, path string, f Forward) {
	if !f.Port.Valid() {
		r.errorf(path+".port",
			"a forward needs the port connections arrive on, for example 8080 or 30000-30010")
		return
	}
	if !f.To.IsValid() || f.To.Port() == 0 {
		// The address half is reported by validateTo; this is the port half,
		// and it is separate because "192.168.1.10" with no port is the shape
		// somebody types when they expect the outside port to be reused.
		r.errorf(path+".to",
			"a forward needs the port to deliver to, as address:port — for example %s:%d",
			addrOrPlaceholder(f), f.Port.From)
		return
	}

	// docs/firewall.md §1.3, and the most important check in this file because
	// what it prevents is a rule that *works* and delivers to the wrong place.
	//
	// Netfilter keeps the original port when it already falls inside the range
	// it was given, which makes an identity range mapping exact. Ask it to
	// shift a range instead and the original port is not in range, so it falls
	// through to picking a free port arbitrarily — connections arrive, on
	// unpredictable ports, and nothing in the configuration looks wrong.
	if !f.Port.Single() && f.To.Port() != f.Port.From {
		r.errorf(path+".to",
			"a range of ports can only be forwarded to the same range: %s would have to go to "+
				"%s:%s, not %s:%d. The kernel keeps each connection's own port when the range "+
				"matches and picks an arbitrary one when it does not, so a shifted range "+
				"delivers to ports nobody chose. Write one forward per port to remap them",
			f.Port, f.To.Addr(), f.Port, f.To.Addr(), f.To.Port())
	}
}

func validateTo(r *Result, path string, f Forward, links LinkView) {
	if !f.To.IsValid() || !f.To.Addr().IsValid() {
		r.errorf(path+".to",
			"a forward needs somewhere to deliver to, as address:port — for example 192.168.1.10:80")
		return
	}

	addr := f.To.Addr().Unmap()

	// docs/firewall.md §7. IPv6 has no NAT: a device inside already has a
	// globally routable address, so "forwarding" a v6 port is a filtering
	// decision rather than a translation — and this module has no filtering
	// policy for it to be a decision within. Refused with the reason rather
	// than accepted into a rule that would do nothing.
	if addr.Is6() {
		r.errorf(path+".to",
			"%s is an IPv6 address, and olr does not forward IPv6. There is no NAT in IPv6 — a "+
				"device inside already has a reachable address — so this would be a firewall "+
				"permission rather than a translation, and olr has no firewall policy to permit "+
				"it within yet",
			addr)
		return
	}

	switch {
	case addr.IsUnspecified():
		r.errorf(path+".to", "the unspecified address is not somewhere to deliver to")
		return
	case addr.IsLoopback():
		r.errorf(path+".to",
			"%s is this box's own loopback, so this would forward the port to olr itself. "+
				"A service running on this router needs no forward — it is already reachable "+
				"on the port it listens on",
			addr)
		return
	case addr.IsMulticast():
		r.errorf(path+".to", "%s is a multicast address; a forward delivers to one device", addr)
		return
	}

	if IsLocalAddress(links, addr) {
		r.errorf(path+".to",
			"%s is one of this box's own addresses, so this forward would point at the router. "+
				"A service running here is already reachable on its own port; a forward is for "+
				"reaching a device on your network",
			addr)
		return
	}

	// Not an error. The device may legitimately be on a network this box has
	// no address on yet — an interface configured after the forward, a static
	// route to a downstream segment — and refusing would make the correct
	// order of operations "bring the network up, then describe the forward",
	// which is backwards for something an operator sets up once.
	if len(PrefixesContaining(links, addr)) == 0 {
		r.warnf(path+".to",
			"%s is not on any network this box has an address on, so the forwarded connection "+
				"has nowhere to be delivered unless a route to it exists",
			addr)
	}
}

// validateHairpin is docs/firewall.md §4.1, said every time rather than once in
// a document.
//
// What hairpin costs is that the device sees the router's address instead of the
// real client's, and the places that bites — an access log with one address in
// it, per-client rules that stop discriminating, fail2ban locking out the whole
// house at once — are all places the operator finds out much later. So the
// warning is attached to the setting, on every surface, rather than left to
// whoever reads the design document.
func validateHairpin(r *Result, path string, f Forward, links LinkView) {
	if !f.HairpinOrDefault() {
		return
	}
	if !f.To.IsValid() || len(PrefixesContaining(links, f.To.Addr().Unmap())) == 0 {
		// Nothing to hairpin from: no network of ours holds the destination, so
		// no masquerade rule will be rendered and the cost below is not paid.
		return
	}
	r.warnf(path+".hairpin",
		"connections to %q from inside your own network will reach %s with this router's "+
			"address as the source rather than the real client's, so that device cannot tell "+
			"its own clients apart. Turn hairpin off if it needs to, and reach the service by "+
			"its internal address from inside",
		f.Name, f.To.Addr())
}

// validateOverlaps is docs/firewall.md §5.1's first row.
//
// Two forwards claiming the same (interface, protocol, port) are genuinely
// ambiguous. nftables would resolve it by rule order — first match wins — which
// is a precedence model the operator cannot see on the screen and cannot
// predict, because the order is the sorted-by-name order rather than the order
// they typed. Refusing is docs/gateway.md §2.3's *conflicts are refused, not
// resolved*, one module across.
func validateOverlaps(r *Result, c Config) {
	for i, a := range c.Forwards {
		if a.In == "" || !a.Port.Valid() {
			continue
		}
		for j := i + 1; j < len(c.Forwards); j++ {
			b := c.Forwards[j]
			if b.In != a.In || !b.Port.Valid() {
				continue
			}
			if !sharesProtocol(a, b) || !a.Port.Overlaps(b.Port) {
				continue
			}
			r.errorf(fmt.Sprintf("forwards[%d].port", j),
				"%q and %q both claim %s port %s on %s; a packet cannot go to two places, "+
					"so give them different ports or different interfaces",
				b.Name, a.Name, sharedProtocols(a, b), overlap(a.Port, b.Port), a.In)
		}
	}
}

func sharesProtocol(a, b Forward) bool {
	for _, p := range a.ProtocolOrDefault().Each() {
		for _, q := range b.ProtocolOrDefault().Each() {
			if p == q {
				return true
			}
		}
	}
	return false
}

// sharedProtocols names the transports two forwards actually collide on, so the
// message says "tcp" rather than "both" when only half of a `both` overlaps.
func sharedProtocols(a, b Forward) string {
	var out []string
	for _, p := range a.ProtocolOrDefault().Each() {
		for _, q := range b.ProtocolOrDefault().Each() {
			if p == q {
				out = append(out, string(p))
			}
		}
	}
	return strings.Join(out, " and ")
}

// overlap is the ports two ranges actually share, which is what the operator has
// to change — naming either range in full would leave them comparing two numbers
// to find the collision.
func overlap(a, b PortRange) PortRange {
	out := PortRange{From: max(a.From, b.From), To: min(a.To, b.To)}
	return out
}

func addrOrPlaceholder(f Forward) string {
	if f.To.IsValid() && f.To.Addr().IsValid() {
		return f.To.Addr().String()
	}
	return "192.168.1.10"
}

func hasControlChar(s string) (rune, bool) {
	for _, r := range s {
		// Tab included: it is a control character that would misalign a table.
		if unicode.IsControl(r) {
			return r, true
		}
	}
	return 0, false
}

// join renders a vocabulary for an error message. Generic so that adding a value
// to the enum cannot leave a message behind.
func join[T ~string](values []T) string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, string(v))
	}
	return strings.Join(out, ", ")
}

// problems converts this module's findings into core's wire shape, so that every
// module reports a bad field the same way (see internal/dhcp/view.go).
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
