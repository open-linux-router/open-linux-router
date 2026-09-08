package link

import (
	"fmt"
	"net/netip"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The API's own shapes, for the reason internal/dhcp/view.go gives: what the
// HTTP API returns should be a deliberate decision rather than a side effect of
// which fields happened to be exported.

// interfaceView is one row of the interface list.
type interfaceView struct {
	Name string `json:"name"`

	// Adopted is the stored half: the operator handed this interface to olr.
	Adopted bool `json:"adopted"`

	// Present reports whether the kernel currently has it. False with Adopted
	// true is the typo case, and the UI has to say so rather than showing a row
	// that looks like any other.
	Present bool `json:"present"`

	// Up is administrative state, Running is carrier. Both, because "up with no
	// cable" is the most common reason a freshly configured DHCP server appears
	// to do nothing, and one boolean cannot say it.
	Up      bool `json:"up"`
	Running bool `json:"running"`

	Loopback bool   `json:"loopback"`
	MAC      string `json:"mac,omitempty"`

	// Prefixes are every address on the interface, link-local excluded.
	Prefixes []string `json:"prefixes,omitempty"`

	// Address and Subnet are the interface's first IPv4 prefix, split into the
	// two halves an operator reads separately: "this router is 192.168.1.2" and
	// "the network is 192.168.1.0/24". Empty when the interface has no IPv4
	// address, which is exactly when no address range can be served on it.
	Address string `json:"address,omitempty"`
	Subnet  string `json:"subnet,omitempty"`

	// SuggestedStart and SuggestedEnd are a range inside Subnet that excludes
	// the network address, the broadcast address, and this interface's own
	// address — the three a range must not contain.
	//
	// A hint for a form to prefill, and nothing more. It encodes subnet
	// arithmetic, not policy: `dhcp` validates any range it is given against its
	// own rules regardless of where the numbers came from, so this cannot become
	// a second opinion that disagrees with the first.
	SuggestedStart string `json:"suggested_start,omitempty"`
	SuggestedEnd   string `json:"suggested_end,omitempty"`
}

func viewInterface(info Info, observed map[string]Interface) interfaceView {
	v := interfaceView{
		Name:     info.Name,
		Adopted:  info.Adopted,
		Present:  info.Present,
		Up:       info.Up,
		Loopback: false,
		Prefixes: make([]string, 0, len(info.Prefixes)),
	}
	for _, p := range info.Prefixes {
		v.Prefixes = append(v.Prefixes, p.String())
	}
	if iface, ok := observed[info.Name]; ok {
		v.Running = iface.Running
		v.Loopback = iface.Loopback
		v.MAC = iface.HardwareAddr
	}

	if prefix, ok := firstIPv4(info.Prefixes); ok {
		v.Address = prefix.Addr().String()
		v.Subnet = prefix.Masked().String()
		if start, end, ok := suggestRange(prefix); ok {
			v.SuggestedStart, v.SuggestedEnd = start.String(), end.String()
		}
	}
	return v
}

type listResponse struct {
	Interfaces []interfaceView `json:"interfaces"`

	// Problems are the findings against the stored set — an adopted name with
	// no interface behind it, an interface with no address. Reported alongside
	// the list rather than as an error, because every one of them describes a
	// row that is still worth showing.
	Problems []core.Problem `json:"problems,omitempty"`

	// AsOf stamps the whole reply: the observed half is read per request and
	// never cached, so it carries its freshness (§4.5).
	AsOf time.Time `json:"as_of"`
}

// --- subnet arithmetic ------------------------------------------------------

// firstIPv4 returns the interface's first IPv4 prefix.
//
// First rather than only: an interface can carry several, and the list is
// sorted with IPv4 ahead of IPv6 (see prefixesOf). A box with two IPv4 subnets
// on one interface is unusual enough that prefilling from the first is a better
// answer than refusing to prefill at all — and the field is a hint.
func firstIPv4(prefixes []netip.Prefix) (netip.Prefix, bool) {
	for _, p := range prefixes {
		if p.Addr().Is4() {
			return p, true
		}
	}
	return netip.Prefix{}, false
}

// maxSuggested caps how many addresses the hint offers.
//
// Without it a /16 would suggest sixty-five thousand addresses, which is a
// legal range and an absurd default. The cap is not a limit on what an operator
// may configure — `dhcp` will take the whole subnet if asked.
const maxSuggested = 254

// suggestRange picks a usable range inside prefix, avoiding the three addresses
// that cannot be handed out: the network, the broadcast, and the router's own.
//
// The interface's address splits the host range in two, and the larger side
// wins. On the ordinary case — a /24 with the router at either end — that is
// simply "everything except the router".
func suggestRange(prefix netip.Prefix) (netip.Addr, netip.Addr, bool) {
	lo, hi, ok := hostRange(prefix)
	if !ok {
		return netip.Addr{}, netip.Addr{}, false
	}

	self := prefix.Addr()
	start, end := lo, hi
	switch {
	case self.Compare(lo) < 0 || self.Compare(hi) > 0:
		// The interface's address is the network or broadcast address, which is
		// a misconfiguration rather than something to work around. The host
		// range is still the right answer.
	default:
		below := runLen(lo, self.Prev())
		above := runLen(self.Next(), hi)
		if above >= below {
			start, end = self.Next(), hi
		} else {
			start, end = lo, self.Prev()
		}
		if start.Compare(end) > 0 {
			return netip.Addr{}, netip.Addr{}, false
		}
	}

	if n := runLen(start, end); n > maxSuggested {
		end = addTo(start, maxSuggested-1)
	}
	return start, end, true
}

// hostRange returns the assignable addresses in an IPv4 prefix: everything
// between the network and broadcast addresses, exclusive of both.
//
// False for /31 and /32, which have no such addresses. Those are legitimate
// prefixes — a point-to-point link, a host route — and the honest answer is
// that there is nothing to suggest, not a range of zero.
func hostRange(prefix netip.Prefix) (netip.Addr, netip.Addr, bool) {
	if !prefix.Addr().Is4() || prefix.Bits() > 30 {
		return netip.Addr{}, netip.Addr{}, false
	}
	network := prefix.Masked().Addr().As4()

	var broadcast [4]byte
	host := 32 - prefix.Bits()
	for i := range broadcast {
		// Bits below the prefix length are set; the rest are copied.
		shift := 24 - 8*i
		var ones byte
		if host > shift {
			if n := host - shift; n >= 8 {
				ones = 0xff
			} else {
				ones = byte(1<<n - 1)
			}
		}
		broadcast[i] = network[i] | ones
	}

	return netip.AddrFrom4(network).Next(), netip.AddrFrom4(broadcast).Prev(), true
}

// addTo returns a advanced by n addresses.
func addTo(a netip.Addr, n int) netip.Addr {
	v := a.As4()
	total := uint32(v[0])<<24 | uint32(v[1])<<16 | uint32(v[2])<<8 | uint32(v[3])
	total += uint32(n)
	return netip.AddrFrom4([4]byte{
		byte(total >> 24), byte(total >> 16), byte(total >> 8), byte(total),
	})
}

// runLen counts the addresses from a to b inclusive, or 0 if b is below a.
func runLen(a, b netip.Addr) int {
	if !a.Is4() || !b.Is4() || a.Compare(b) > 0 {
		return 0
	}
	av, bv := a.As4(), b.As4()
	an := uint32(av[0])<<24 | uint32(av[1])<<16 | uint32(av[2])<<8 | uint32(av[3])
	bn := uint32(bv[0])<<24 | uint32(bv[1])<<16 | uint32(bv[2])<<8 | uint32(bv[3])
	return int(bn-an) + 1
}

// --- plan -------------------------------------------------------------------

// planView mirrors internal/dhcp's and internal/devices' plan shape on purpose.
//
// The field names are identical so that a client's Plan type and its plan
// preview render any module's answer without a second implementation. The
// values are what they honestly are for a module that writes only a document:
// no backend, no service action, and an impact of none — adopting an interface
// cannot drop a client, because by itself it does nothing at all.
type planView struct {
	Backend  string         `json:"backend"`
	Changes  []changeView   `json:"changes"`
	Action   string         `json:"action"`
	Impact   string         `json:"impact"`
	Empty    bool           `json:"empty"`
	Warnings []core.Problem `json:"warnings,omitempty"`
}

type changeView struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Impact string `json:"impact"`
	Diff   string `json:"diff"`
}

const (
	impactNone = "none"
	actionNone = "none"

	kindCreate = "create"
	kindDelete = "delete"
)

// buildPlan diffs stored adoption against a proposal.
//
// There is no update kind, only create and delete: the entries are bare names,
// so changing one is removing a name and adding another. Rendering that as an
// update would invent a relationship between two interfaces that have nothing
// to do with each other.
func buildPlan(stored, desired Config, observed []Interface) planView {
	stored.Normalize()
	desired.Normalize()

	res := Validate(desired, observed)
	plan := planView{
		Backend:  "",
		Changes:  []changeView{},
		Action:   actionNone,
		Impact:   impactNone,
		Warnings: problems(res.Warnings),
	}

	for _, name := range desired.Adopted {
		if !stored.IsAdopted(name) {
			plan.Changes = append(plan.Changes, changeView{
				Path:   itemPath(name),
				Kind:   kindCreate,
				Impact: impactNone,
				Diff:   fmt.Sprintf("+ %s\n", name),
			})
		}
	}
	for _, name := range stored.Adopted {
		if !desired.IsAdopted(name) {
			plan.Changes = append(plan.Changes, changeView{
				Path:   itemPath(name),
				Kind:   kindDelete,
				Impact: impactNone,
				Diff:   fmt.Sprintf("- %s\n", name),
			})
		}
	}

	plan.Empty = len(plan.Changes) == 0
	return plan
}

func itemPath(name string) string { return "adopted[" + name + "]" }

// observedByName indexes the kernel's list, for the view's second half.
func observedByName(in []Interface) map[string]Interface {
	out := make(map[string]Interface, len(in))
	for _, iface := range in {
		out[iface.Name] = iface
	}
	return out
}
