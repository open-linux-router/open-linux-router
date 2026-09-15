package daemon

import (
	"fmt"
	"log/slog"
	"net/netip"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/dhcp"
	"github.com/open-linux-router/open-linux-router/internal/link"
)

// Migrating a pre-0.3 document, in the one place that can.
//
// A pool used to be keyed by kernel interface and to carry its own IPv4 range.
// It is now keyed by a network, and the network owns the subnet. Converting the
// pool itself is `dhcp`'s job and it does it while parsing (dhcp.LegacyPool) —
// but the *key* names an object `dhcp` does not own and cannot invent. Only
// this package sees both modules, so this is where a network gets created for
// each interface that had a pool.
//
// # Why at startup rather than on first write
//
// The alternative is to leave the old document alone and convert on the way
// past, every time. That means every surface carries the old shape forever, the
// two shapes have to agree about everything, and `olr.json` — which design.md
// §10 says is the recovery path you SSH in and read — has a schema that depends
// on when the box was installed. One conversion, written back once, keeps the
// file honest.
//
// # What it deliberately does not do
//
// It does not touch the kernel. The network it creates carries the subnet the
// interface *already has*, so applying it is a no-op on a box that was working
// — nobody's addresses move because olr was upgraded. If the interface has no
// IPv4 address the network is created without a subnet, which validates as a
// warning rather than an error and leaves the operator a row to fix instead of
// a parse failure.

// migrateDocument converts a pre-0.3 configuration in place, returning whether
// anything changed.
//
// It is idempotent: a document already in the current shape parses as one, so
// dhcp.Config.Legacy is empty and there is nothing to do.
func migrateDocument(store *core.Store, facts link.Facts, log *slog.Logger) (bool, error) {
	doc, err := store.Load()
	if err != nil {
		return false, err
	}

	dhcpCfg, err := dhcp.FromDocument(doc)
	if err != nil {
		return false, err
	}
	if len(dhcpCfg.Legacy) == 0 {
		return false, nil
	}

	linkCfg, err := link.FromDocument(doc)
	if err != nil {
		return false, err
	}

	observed, err := facts.Interfaces()
	if err != nil {
		// Without the interface list there is no subnet to give the networks,
		// and a migration that silently dropped every subnet would renumber the
		// box on the next apply. Refusing leaves the old document intact and
		// readable, which is the recoverable failure.
		return false, fmt.Errorf("reading interfaces to migrate the dhcp pools: %w", err)
	}
	prefixes := map[string]netip.Prefix{}
	for _, info := range observed {
		if p, ok := core.FirstIPv4(info.Prefixes); ok {
			prefixes[info.Name] = p
		}
	}

	for i, old := range dhcpCfg.Legacy {
		iface := old.Interface
		if iface == "" {
			continue
		}

		// The network takes the interface's name. It is not a pretty name, and
		// that is the right trade: the operator can rename it, and anything else
		// would be us inventing a name they never chose for a network they
		// already have.
		name := iface
		if _, exists := linkCfg.Group(name); !exists {
			g := link.Group{Name: name, Members: []string{iface}}
			if prefix, ok := prefixes[iface]; ok {
				// The address the interface holds *now*, so this migration
				// changes nothing on the box. A router address different from
				// the one already configured would move it on the next apply.
				router := prefix.Addr()
				g.IPv4 = &link.GroupIPv4{Subnet: prefix.Masked(), Router: &router}
			} else {
				log.Warn("migrated a dhcp pool whose interface has no IPv4 address",
					"interface", iface, "network", name)
			}
			linkCfg.SetGroup(g)
			// A pool on an interface nobody adopted could not have been applied,
			// but it could have been stored — and dropping the adoption here
			// would turn a config that was merely invalid into one that is
			// invalid for a second, more confusing reason.
			linkCfg.Adopt(iface)
		}
		dhcpCfg.Pools[i].Group = name
	}

	dhcpCfg.Legacy = nil
	dhcpCfg.Normalize()
	linkCfg.Normalize()

	linkData, err := link.MarshalConfig(linkCfg)
	if err != nil {
		return false, err
	}
	dhcpData, err := dhcp.MarshalConfig(dhcpCfg)
	if err != nil {
		return false, err
	}

	// Both modules in one write. The store is a single document and a single
	// rename (core/store.go), so there is no window where the pools are keyed by
	// network and the networks do not exist yet — which would be a box that
	// refuses to serve DHCP until somebody noticed.
	doc.Set(link.ModuleName, linkData)
	doc.Set(dhcp.ModuleName, dhcpData)
	if err := store.Save(doc); err != nil {
		return false, fmt.Errorf("storing the migrated configuration in %s: %w", store.Path(), err)
	}

	log.Info("migrated dhcp pools onto networks",
		"pools", len(dhcpCfg.Pools), "networks", len(linkCfg.Groups))
	return true, nil
}
