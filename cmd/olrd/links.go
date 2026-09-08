package main

import (
	"fmt"

	"github.com/open-linux-router/open-linux-router/internal/dhcp"
	"github.com/open-linux-router/open-linux-router/internal/dns"
	"github.com/open-linux-router/open-linux-router/internal/link"
	"github.com/open-linux-router/open-linux-router/internal/routing"
)

// Adapters joining the link module to the three modules that read it.
//
// They live here, in the binary that mounts all four, for the reason
// devices.go gives about its own: it keeps design.md §4.1's arrow pointing one
// way. Each of `dhcp`, `dns` and `routing` declares the interface facts it
// needs as its own LinkView and never imports `link`; `link` stays unaware that
// anything consumes it. The four are introduced at the one place that already
// knows the whole module list.
//
// The three LinkInfo structs happen to have identical fields today, so these
// read as three copies of one conversion. They are not one type for the same
// reason the interfaces are three: the day `routing` needs an MTU is the day
// the other two should not grow a field they do not use, and collapsing them
// now would make that change a four-module edit instead of a one-module one.
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

// routingLinkView is the routing module's window onto link.
type routingLinkView struct{ facts link.Facts }

func (l routingLinkView) Interface(name string) (routing.LinkInfo, error) {
	info, err := l.facts.Interface(name)
	if err != nil {
		return routing.LinkInfo{}, fmt.Errorf("%q: %w", name, routing.ErrNoSuchInterface)
	}
	return routing.LinkInfo{
		Name:     info.Name,
		Adopted:  info.Adopted,
		Up:       info.Up,
		Prefixes: info.Prefixes,
	}, nil
}

func (l routingLinkView) Interfaces() ([]routing.LinkInfo, error) {
	all, err := l.facts.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]routing.LinkInfo, 0, len(all))
	for _, info := range all {
		out = append(out, routing.LinkInfo{
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
