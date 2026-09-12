package daemon

import (
	"fmt"

	"github.com/open-linux-router/open-linux-router/internal/dhcp"
	"github.com/open-linux-router/open-linux-router/internal/dial"
	"github.com/open-linux-router/open-linux-router/internal/dns"
	"github.com/open-linux-router/open-linux-router/internal/firewall"
	"github.com/open-linux-router/open-linux-router/internal/gateway"
	"github.com/open-linux-router/open-linux-router/internal/link"
)

// Adapters joining the link module to the five modules that read it.
//
// They live here, in the binary that mounts them all, for the reason devices.go
// gives about its own: it keeps design.md §4.1's arrow pointing one way. Each of
// `dhcp`, `dns`, `gateway`, `firewall` and `dial` declares the interface facts
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

// dhcpLinkView is the dhcp module's window onto link.
type dhcpLinkView struct{ facts link.Facts }

func (l dhcpLinkView) Interface(name string) (dhcp.LinkInfo, error) {
	info, err := l.facts.Interface(name)
	if err != nil {
		// The sentinel is re-wrapped as the consumer's own, so that a caller
		// testing errors.Is against dhcp.ErrNoSuchInterface still gets a true
		// answer. Passing ours through would make that test silently false.
		return dhcp.LinkInfo{}, fmt.Errorf("%q: %w", name, dhcp.ErrNoSuchInterface)
	}
	return dhcp.LinkInfo{
		Name:     info.Name,
		Adopted:  info.Adopted,
		Up:       info.Up,
		Prefixes: info.Prefixes,
	}, nil
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

// firewallLinkView is the firewall module's window onto link.
//
// What it uses the prefixes for is different from what gateway does with them:
// gateway matches a source range to classify it, while firewall needs to know
// which of its own networks hold a forward's destination, because that is the
// set whose replies would otherwise bypass the router (docs/firewall.md §4).
type firewallLinkView struct{ facts link.Facts }

func (l firewallLinkView) Interface(name string) (firewall.LinkInfo, error) {
	info, err := l.facts.Interface(name)
	if err != nil {
		return firewall.LinkInfo{}, fmt.Errorf("%q: %w", name, firewall.ErrNoSuchInterface)
	}
	return firewall.LinkInfo{
		Name:     info.Name,
		Adopted:  info.Adopted,
		Up:       info.Up,
		Prefixes: info.Prefixes,
	}, nil
}

func (l firewallLinkView) Interfaces() ([]firewall.LinkInfo, error) {
	all, err := l.facts.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]firewall.LinkInfo, 0, len(all))
	for _, info := range all {
		out = append(out, firewall.LinkInfo{
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
// The narrowest use of these facts in the tree: `dial` reads an uplink's address
// to publish it, and checks adoption because reading an address off an interface
// nobody handed over is exactly the surprise design.md §3.4 exists to prevent.
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
