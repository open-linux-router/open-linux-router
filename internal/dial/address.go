package dial

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"time"
)

// Where the address comes from (docs/ddns.md §3), which is the whole difficulty
// of this module. Everything after it is a periodic HTTP request.

// CGNAT is the carrier-grade NAT range, RFC 6598.
//
// An address in here is one the ISP handed to a customer behind their own NAT,
// and **a name pointing at it resolves to something nobody can reach**. That is
// the one diagnosis DDNS can offer that nothing else in this product can
// (docs/ddns.md §3.2), and it is the difference between an operator reading one
// line of status and spending an afternoon debugging a port forward that was
// never going to work.
var CGNAT = netip.MustParsePrefix("100.64.0.0/10")

// IsCGNAT reports whether addr is inside the carrier-grade NAT range.
func IsCGNAT(addr netip.Addr) bool { return addr.Is4() && CGNAT.Contains(addr) }

// reflectorTimeout bounds one request to a third party.
//
// Short, because a reflector that is slow is a reflector that is having
// problems, and the next check is minutes away — waiting is worth less than
// reporting "it did not answer" and trying again.
const reflectorTimeout = 10 * time.Second

// Discoverer reads the address a record should publish.
//
// Both halves read live state per call and neither caches: the interface's
// address comes through LinkView, which reads the kernel per request, and the
// reflector is asked each time. There is nothing stored here to go stale
// (§4.5), which is what lets status report *when* the address was last read as
// a separate fact from what it was.
type Discoverer struct {
	// Links is the window onto the link module. Required by SourceInterface and
	// by the bound form of SourceReflector.
	Links LinkView

	// Client is used for reflector requests when the record binds to no
	// interface. Nil means a client with reflectorTimeout.
	//
	// A record that *does* name an interface gets a client built per call, with
	// a dialer bound to that interface — a shared one could not be, because the
	// binding is a property of the socket rather than of the request.
	Client *http.Client
}

// Address reads the address for one record.
func (d Discoverer) Address(ctx context.Context, rec Record) (netip.Addr, error) {
	switch rec.Source {
	case SourceInterface:
		return d.fromInterface(rec)
	case SourceReflector:
		return d.fromReflector(ctx, rec)
	default:
		// Unreachable through the API, which validates first. Reached by a
		// hand-edited config document, which is exactly the case that must not
		// silently pick one.
		return netip.Addr{}, fmt.Errorf("%s has no address source; "+
			"set it to %q or %q", rec.Name, SourceInterface, SourceReflector)
	}
}

// fromInterface reads the address off the adopted uplink.
func (d Discoverer) fromInterface(rec Record) (netip.Addr, error) {
	if d.Links == nil {
		return netip.Addr{}, errors.New("no interface facts are available")
	}
	info, err := d.Links.Interface(rec.Interface)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("reading %s: %w", rec.Interface, err)
	}
	if !info.Adopted {
		// Re-checked here and not only in the validator, because the operator
		// can release an interface after the record was stored and the
		// publisher would otherwise keep reading it.
		return netip.Addr{}, fmt.Errorf("%s is no longer handed to olr", rec.Interface)
	}
	addr, ok := info.PublicIPv4()
	if !ok {
		return netip.Addr{}, fmt.Errorf("%s has no IPv4 address", rec.Interface)
	}
	return addr, nil
}

// fromReflector asks an endpoint what address it sees.
func (d Discoverer) fromReflector(ctx context.Context, rec Record) (netip.Addr, error) {
	client, err := d.reflectorClient(rec.Interface)
	if err != nil {
		return netip.Addr{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rec.ReflectorURL, nil)
	if err != nil {
		return netip.Addr{}, err
	}
	// Named, because the operator of a free reflector deserves to know who is
	// asking, and because it is the string they will grep for when they want to
	// tell us to stop.
	req.Header.Set("User-Agent", "open-linux-router")

	resp, err := client.Do(req)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("asking %s: %w", rec.ReflectorURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("reading the reply from %s: %w", rec.ReflectorURL, err)
	}
	if resp.StatusCode >= 300 {
		return netip.Addr{}, fmt.Errorf("%s answered %s", rec.ReflectorURL, resp.Status)
	}

	addr, ok := parseReflected(string(body))
	if !ok {
		return netip.Addr{}, fmt.Errorf("%s did not answer with an IPv4 address: %s",
			rec.ReflectorURL, firstLine(body))
	}
	return addr, nil
}

// reflectorClient returns the client one request is made with.
func (d Discoverer) reflectorClient(iface string) (*http.Client, error) {
	if iface == "" {
		if d.Client != nil {
			return d.Client, nil
		}
		return &http.Client{Timeout: reflectorTimeout}, nil
	}

	// Bound. Not shared and not cached: an interface's index can change under a
	// long-lived transport, and a pooled connection established through the old
	// one would keep answering with the wrong address — which is precisely the
	// failure the binding exists to prevent.
	dialer := &net.Dialer{Timeout: reflectorTimeout}
	if err := bindToDevice(dialer, iface); err != nil {
		return nil, fmt.Errorf("binding the request to %s: %w", iface, err)
	}
	return &http.Client{
		Timeout:   reflectorTimeout,
		Transport: &http.Transport{DialContext: dialer.DialContext},
	}, nil
}

// ipv4Pattern finds a dotted quad in a reply that is not only a dotted quad.
//
// Most reflectors answer with the address and a newline, which ParseAddr
// handles on its own. Some wrap it in JSON, and a couple in a whole HTML page.
// Searching is not an invitation to point this at an arbitrary web page — it is
// what keeps `{"ip":"203.0.113.9"}` working without a per-vendor parser.
var ipv4Pattern = regexp.MustCompile(`\b(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})\b`)

// parseReflected pulls the address out of a reflector's reply.
func parseReflected(body string) (netip.Addr, bool) {
	if addr, err := netip.ParseAddr(strings.TrimSpace(body)); err == nil && addr.Is4() {
		return addr, true
	}
	for _, match := range ipv4Pattern.FindAllString(body, 8) {
		addr, err := netip.ParseAddr(match)
		if err != nil || !addr.Is4() {
			continue
		}
		// Skip the addresses a page might mention that cannot be an answer.
		// Without this, an error page containing "127.0.0.1" would be published.
		if addr.IsLoopback() || addr.IsUnspecified() || addr.IsLinkLocalUnicast() {
			continue
		}
		return addr, true
	}
	return netip.Addr{}, false
}

// firstLine trims a reply down to something an error message can carry.
func firstLine(body []byte) string {
	s := strings.TrimSpace(string(body))
	if s == "" {
		return "(empty reply)"
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}
