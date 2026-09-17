package remote

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

// The validation vocabulary, shared by both objects.
//
// Shared because a caller does not care which object refused: the HTTP surface
// turns these into one 422 with a list of field paths, and the paths are what
// say where the problem is. The *rules* have nothing in common and live apart
// (wg_validate.go, ss_validate.go).

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
	return fmt.Errorf("invalid remote-access configuration:\n  %w", errors.Join(msgs...))
}

// merge folds another result in, for the module-level validation that runs both
// objects' rules over one document.
func (r *Result) merge(other Result) {
	r.Errors = append(r.Errors, other.Errors...)
	r.Warnings = append(r.Warnings, other.Warnings...)
}

// validateEndpoint checks the one field the two objects share.
//
// A refusal rather than a warning when something is enabled, because a client
// configuration without a reachable endpoint is not a degraded configuration —
// it is a file that cannot do anything at all, and the operator finds that out
// on a phone rather than here.
func validateEndpoint(r *Result, c Config) {
	host := c.EndpointHost()
	needed := c.WireGuard.Enabled || c.Shadowsocks.Enabled

	if host == "" {
		if needed {
			r.errorf("endpoint",
				"required: the public name or address your devices dial from outside. "+
					"If a name here already tracks this box's address, use it — that is what `olr dial show` lists")
		}
		return
	}

	// A port here could only ever be right for one of the two objects, and
	// there is no way to tell which was meant. Each object has `public_port`
	// for the case this spelling was reaching for.
	if _, _, ok := splitHostPort(host); ok {
		r.errorf("endpoint",
			"%q carries a port, and this field is shared by every way in — a port here could "+
				"only be right for one of them. Give the host alone, and set `public_port` on the "+
				"one whose forwarded port differs", host)
		return
	}
	if strings.ContainsAny(host, " \t/") {
		r.errorf("endpoint", "%q is neither an address nor a hostname", host)
		return
	}

	addr, err := netip.ParseAddr(host)
	if err != nil {
		// A name, which is the ordinary case and the one `dial` exists to keep
		// current. Nothing to check — whether it resolves to this box is a
		// question about the internet, not about the config.
		return
	}
	if addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		r.warnf("endpoint",
			"%s is not reachable from the internet, so only devices already on this network can "+
				"use it. That is a fine way to test and not a way to get home", addr)
	}
}

// splitHostPort reports whether a value carries a port, and splits it.
//
// Hand-rolled rather than net.SplitHostPort because that function accepts an
// empty port and reports a bare IPv6 address as an error whose text mentions
// "too many colons" — neither of which is useful to an operator here.
func splitHostPort(s string) (host, port string, ok bool) {
	if strings.HasPrefix(s, "[") {
		end := strings.LastIndex(s, "]")
		if end < 0 || end+1 >= len(s) || s[end+1] != ':' {
			return "", "", false
		}
		return s[1:end], s[end+2:], s[end+2:] != ""
	}
	i := strings.LastIndex(s, ":")
	if i < 0 || i+1 >= len(s) || strings.Contains(s[:i], ":") {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}
