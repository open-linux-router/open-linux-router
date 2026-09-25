package dns

import (
	"fmt"
	"slices"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/dnsrelay"
)

// This module's read-only window onto the names `ingress` publishes.
//
// docs/ingress.md §3 promised that a published service is "one more name in a
// namespace that is already served", and for a while nothing made that true:
// the names were in the Caddyfile and nowhere in DNS. A query for one fell
// through to the internet, and if the operator had put a public record there
// pointing at the router, unbound's rebinding protection stripped it — as it
// should. The service answered on :443 and could not be found by name on the
// network it was published for.
//
// The answer is the relay's, not unbound's; internal/dnsrelay/published.go has
// why. This file is the part that decides *which* names.
//
// # Why this one does render, when dhcp.go's does not
//
// ReservationView feeds validation only, and dhcp.go argues it must, on two
// costs: rendered bytes built from a sibling's facts drift when the sibling
// moves, and fixing that by re-applying dns from dhcp would let a dhcp plan
// restart the resolver behind the operator's back. Neither cost applies here.
// The names *have* to be rendered — a warning cannot answer a query — and the
// file they land in is re-read on SIGHUP, so the re-apply ingress triggers after
// its own apply (design.md §4.1, "an arrow is walked when the fact at its tail
// changes") reloads the relay and interrupts nobody. The resolver is never
// touched.
type PublishedView interface {
	// Published returns the names published under the local domain, relative
	// to it — "nas", not "nas.home.example.com" — the way `ingress` stores
	// them. Empty when ingress is disabled.
	Published() ([]string, error)
}

// renderPublished writes the relay's published-names file, or nothing when no
// name is published. Nothing rather than an empty list, so a box that has never
// used ingress carries no file for it; the relay reads a missing file as none.
func (b Backend) renderPublished(c Config, published PublishedView) (*File, error) {
	if published == nil {
		return nil, nil
	}
	names, err := published.Published()
	if err != nil {
		return nil, fmt.Errorf("reading the names ingress publishes: %w", err)
	}

	var fqdns []string
	for _, name := range names {
		name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
		if name == "" {
			continue
		}
		fqdns = append(fqdns, c.FQDN(name))
	}
	if len(fqdns) == 0 {
		return nil, nil
	}
	slices.Sort(fqdns)
	fqdns = slices.Compact(fqdns)

	data, err := dnsrelay.MarshalPublished(dnsrelay.Published{Names: fqdns})
	if err != nil {
		return nil, fmt.Errorf("rendering published names: %w", err)
	}
	return &File{
		Path: b.Paths.Published,
		Mode: 0o644,
		Data: append(data, '\n'),
		// Re-read on SIGHUP, like the policies: publishing a service must not
		// restart DNS for the house.
		Reloadable: true,
		Unit:       b.RelayUnit(),
	}, nil
}
