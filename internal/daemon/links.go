package daemon

import (
	"fmt"
	"net/netip"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/dhcp"
	"github.com/open-linux-router/open-linux-router/internal/dial"
	"github.com/open-linux-router/open-linux-router/internal/dns"
	"github.com/open-linux-router/open-linux-router/internal/gateway"
	"github.com/open-linux-router/open-linux-router/internal/gateway/nat"
	"github.com/open-linux-router/open-linux-router/internal/link"
)

// Adapters joining the link module to the five modules that read it.
//
// They live here, in the binary that mounts them all, for the reason devices.go
// gives about its own: it keeps design.md §4.1's arrow pointing one way. Each of
// `dhcp`, `dns`, `gateway`, `gateway/nat` and `dial` declares the interface facts
// it needs as its own LinkView and never imports `link`; `link` stays unaware
// that anything consumes it. They are introduced at the one place that already
// knows the whole module list.
//
// The five LinkInfo structs happen to have identical fields today, so these read
// as five copies of one conversion. They are not one type for the same reason
// the interfaces are five: the day `gateway` needs an MTU is the day the others
// should not grow a field they do not use, and collapsing them now would make
// that change a six-module edit instead of a one-module one.
//
// This replaced three separate readers of a hand-written `--links` file. That
// file was a second copy of what the kernel already knows, with nothing keeping
// the two in step — an operator who renumbered an interface got pools validated
// against the address it used to have.

// dhcpGroupView is the dhcp module's window onto link's networks.
//
// The only one of these adapters keyed by network rather than by interface, and
// deliberately so: a DHCP pool serves a network, and what it needs to know is
// the subnet somebody declared — not the address an interface happens to hold.
// The other four still read interfaces because what they configure really is
// per-interface; moving them is a separate retrofit (design.md §4.4).
type dhcpGroupView struct{ facts link.Facts }

func (l dhcpGroupView) Group(name string) (dhcp.GroupInfo, error) {
	info, err := l.facts.Group(name)
	if err != nil {
		// The sentinel is re-wrapped as the consumer's own, so that a caller
		// testing errors.Is against dhcp.ErrNoSuchGroup still gets a true
		// answer. Passing ours through would make that test silently false.
		return dhcp.GroupInfo{}, fmt.Errorf("%q: %w", name, dhcp.ErrNoSuchGroup)
	}
	return dhcpGroup(info), nil
}

func (l dhcpGroupView) Groups() ([]dhcp.GroupInfo, error) {
	all, err := l.facts.Groups()
	if err != nil {
		return nil, err
	}
	out := make([]dhcp.GroupInfo, 0, len(all))
	for _, info := range all {
		out = append(out, dhcpGroup(info))
	}
	return out, nil
}

func dhcpGroup(info link.GroupInfo) dhcp.GroupInfo {
	return dhcp.GroupInfo{
		Name:    info.Name,
		Members: info.Members,
		Subnet:  info.Subnet,
		Router:  info.Router,
		Up:      info.Up,
	}
}

// dnsLinkView is the dns module's window onto link.
type dnsLinkView struct{ facts link.Facts }

func (l dnsLinkView) Interface(name string) (dns.LinkInfo, error) {
	info, err := l.facts.Interface(name)
	if err != nil {
		return dns.LinkInfo{}, fmt.Errorf("%q: %w", name, dns.ErrNoSuchInterface)
	}
	return dns.LinkInfo{
		Name:     info.Name,
		Adopted:  info.Adopted,
		Up:       info.Up,
		Prefixes: info.Prefixes,
	}, nil
}

func (l dnsLinkView) Interfaces() ([]dns.LinkInfo, error) {
	all, err := l.facts.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]dns.LinkInfo, 0, len(all))
	for _, info := range all {
		out = append(out, dns.LinkInfo{
			Name:     info.Name,
			Adopted:  info.Adopted,
			Up:       info.Up,
			Prefixes: info.Prefixes,
		})
	}
	return out, nil
}

// gatewayLinkView is the gateway module's window onto link.
type gatewayLinkView struct{ facts link.Facts }

func (l gatewayLinkView) Interface(name string) (gateway.LinkInfo, error) {
	info, err := l.facts.Interface(name)
	if err != nil {
		return gateway.LinkInfo{}, fmt.Errorf("%q: %w", name, gateway.ErrNoSuchInterface)
	}
	return gateway.LinkInfo{
		Name:     info.Name,
		Adopted:  info.Adopted,
		Up:       info.Up,
		Prefixes: info.Prefixes,
	}, nil
}

func (l gatewayLinkView) Interfaces() ([]gateway.LinkInfo, error) {
	all, err := l.facts.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]gateway.LinkInfo, 0, len(all))
	for _, info := range all {
		out = append(out, gateway.LinkInfo{
			Name:     info.Name,
			Adopted:  info.Adopted,
			Up:       info.Up,
			Prefixes: info.Prefixes,
		})
	}
	return out, nil
}

// Networks implements gateway.LinkView: the subnets link declares, which is
// what the egress masquerade's source set is built from.
//
// Intent rather than observation — `link.Facts.Groups` reads the stored
// networks — which is design.md §4.1's instruction that dependents read a
// *group* and do not restate a subnet link already owns.
func (l gatewayLinkView) Networks() ([]netip.Prefix, error) {
	all, err := l.facts.Groups()
	if err != nil {
		return nil, err
	}
	out := make([]netip.Prefix, 0, len(all))
	for _, g := range all {
		if g.Subnet.IsValid() {
			out = append(out, g.Subnet.Masked())
		}
	}
	return out, nil
}

// gatewayUplink is gateway's window onto `dial`, for the one fact the egress
// masquerade needs.
//
// It reads the document directly rather than going through a dial.Applier,
// because what it wants is stored intent and nothing else — which interface the
// operator declared as the way out. An Applier would bring a LinkView and a
// kernel writer with it for a field lookup.
//
// A missing or unreadable `dial` section is not an error: it means olr does not
// own the way out, which is the reference topology and most boxes. The caller
// then writes no egress rule, which is the correct answer rather than a
// degraded one.
type gatewayUplink struct{ store *core.Store }

// Uplink implements gateway.UplinkView.
func (u gatewayUplink) Uplink() (string, error) {
	doc, err := u.store.Load()
	if err != nil {
		return "", err
	}
	cfg, err := dial.FromDocument(doc)
	if err != nil {
		return "", err
	}
	if cfg.Uplink == nil {
		return "", nil
	}
	return cfg.Uplink.Interface, nil
}

// natLinkView is the NAT half of gateway's window onto link.
//
// What it uses the prefixes for is different from what the routing half does
// with them: that one matches a source range to classify it, while this one
// needs to know which of our networks hold a forward's destination — the set
// whose replies would otherwise bypass the router (docs/port-forwarding.md §4).
//
// Two adapters for one module, because the two halves are two packages and each
// declares the facts it needs. That is the same rule the five modules follow,
// applied one level down.
type natLinkView struct{ facts link.Facts }

func (l natLinkView) Interface(name string) (nat.LinkInfo, error) {
	info, err := l.facts.Interface(name)
	if err != nil {
		return nat.LinkInfo{}, fmt.Errorf("%q: %w", name, nat.ErrNoSuchInterface)
	}
	return nat.LinkInfo{
		Name:     info.Name,
		Adopted:  info.Adopted,
		Up:       info.Up,
		Prefixes: info.Prefixes,
	}, nil
}

func (l natLinkView) Interfaces() ([]nat.LinkInfo, error) {
	all, err := l.facts.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]nat.LinkInfo, 0, len(all))
	for _, info := range all {
		out = append(out, nat.LinkInfo{
			Name:     info.Name,
			Adopted:  info.Adopted,
			Up:       info.Up,
			Prefixes: info.Prefixes,
		})
	}
	return out, nil
}

// dialLinkView is the dial module's window onto link.
//
// `dial` reads an uplink's address to publish it, and checks adoption because
// reading an address off an interface nobody handed over is exactly the
// surprise design.md §3.4 exists to prevent. It is also the only one of these
// five that reads *networks* as well as interfaces, and for a boundary rather
// than a feature: the uplink's interface may not also be a network's member, so
// `dial` has to be able to see what the networks claim.
type dialLinkView struct{ facts link.Facts }

func (l dialLinkView) Interface(name string) (dial.LinkInfo, error) {
	info, err := l.facts.Interface(name)
	if err != nil {
		return dial.LinkInfo{}, fmt.Errorf("%q: %w", name, dial.ErrNoSuchInterface)
	}
	return dial.LinkInfo{
		Name:     info.Name,
		Adopted:  info.Adopted,
		Up:       info.Up,
		Prefixes: info.Prefixes,
	}, nil
}

func (l dialLinkView) Interfaces() ([]dial.LinkInfo, error) {
	all, err := l.facts.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]dial.LinkInfo, 0, len(all))
	for _, info := range all {
		out = append(out, dial.LinkInfo{
			Name:     info.Name,
			Adopted:  info.Adopted,
			Up:       info.Up,
			Prefixes: info.Prefixes,
		})
	}
	return out, nil
}

func (l dialLinkView) Groups() ([]dial.GroupInfo, error) {
	all, err := l.facts.Groups()
	if err != nil {
		return nil, err
	}
	out := make([]dial.GroupInfo, 0, len(all))
	for _, info := range all {
		out = append(out, dial.GroupInfo{Name: info.Name, Members: info.Members})
	}
	return out, nil
}

// interfaceSource resolves the --links flag.
//
// Empty means the kernel, which is what a packaged install always uses. A path
// is the development override: it lets olrd run against a plausible network on
// a machine that has none, which is what `make dev` needs and what CI would
// otherwise have to invent. Adoption is *not* read from it — that lives in the
// configuration document now, so `make dev` exercises the same adopt flow an
// operator does rather than a shortcut nobody else has.
func interfaceSource(path string) (link.Source, error) {
	if path == "" {
		return nil, nil
	}
	source, err := link.LoadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return source, nil
}
