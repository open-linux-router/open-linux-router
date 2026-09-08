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
// Answering that would mean importing `dhcp`, `dns` and `routing`, inverting
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
		case present && len(iface.Prefixes) == 0:
			// The one that actually bites: an interface with no address has no
			// subnet for a pool's range to fall inside, so `dhcp` will refuse
			// every range on it with a message about subnets rather than about
			// the missing address.
			r.warnf(path, "%q has no address configured; give it one before adding "+
				"an address range, or the range will have no subnet to sit in", name)
		}
	}

	return r
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
