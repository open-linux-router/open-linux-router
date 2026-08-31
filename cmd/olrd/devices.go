package main

import (
	"context"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/devices"
	"github.com/open-linux-router/open-linux-router/internal/dhcp"
)

// Adapters joining the dhcp module to the devices module.
//
// They live here, in the binary that mounts both, rather than inside either
// module. That is what keeps design.md §4.1's arrow pointing one way: `devices`
// declares the interfaces it needs (PresenceSource, FixedAddressView) and never
// imports `dhcp`, while `dhcp` stays unaware that an inventory exists. The two
// are introduced at the one place that already knows the whole module list.

// dhcpPresence reads the lease database as sightings.
type dhcpPresence struct {
	applier dhcp.Applier
}

func (d dhcpPresence) Name() devices.Source { return devices.SourceDHCPLease }

func (d dhcpPresence) Presence(_ context.Context) ([]devices.Sighting, []devices.Problem, error) {
	leases, bad, err := d.applier.Leases()
	if err != nil {
		return nil, nil, err
	}

	// Unparseable lease lines are carried across rather than dropped, so a
	// corrupt lease file is visible on the device list instead of quietly
	// shortening it.
	problems := make([]devices.Problem, 0, len(bad))
	for _, p := range bad {
		problems = append(problems, devices.Problem{
			Path:    string(devices.SourceDHCPLease),
			Message: p.Message,
		})
	}

	now := time.Now()
	out := make([]devices.Sighting, 0, len(leases))
	for _, l := range leases {
		// A DHCPv6 lease frequently has no hardware address at all — it is
		// keyed by IAID and DUID instead. A device is keyed by MAC (§4.4), so
		// there is nothing here to join on and the honest thing is to skip it
		// rather than invent a key. The consequence is that a v6-only client is
		// invisible to this source, which is the same gap ARP has and the same
		// one a netlink ND reader would close.
		if l.MAC == "" {
			continue
		}

		s := devices.Sighting{
			MAC:      l.MAC,
			Hostname: l.Hostname,
			Source:   devices.SourceDHCPLease,
			Active:   l.Active(now),
		}
		if l.IP.IsValid() {
			s.IP = l.IP.String()
		}
		if !l.Expires.IsZero() {
			expires := l.Expires
			s.Expires = &expires
		}
		out = append(out, s)
	}

	return out, problems, nil
}

// dhcpFixedAddresses reads reservations as MAC → address.
//
// Read through dhcp's own Load rather than from a copy: `dhcp` remains the
// single owner of a fixed address, and the device list joins against it per
// request. §11.1 asked that the fixed-address *workflow* start from the device;
// it did not ask for the fact to move, and moving it would be the private copy
// §4.1 forbids.
type dhcpFixedAddresses struct {
	applier dhcp.Applier
}

func (d dhcpFixedAddresses) FixedAddresses(_ context.Context) (map[string]string, error) {
	cfg, err := d.applier.Load()
	if err != nil {
		return nil, err
	}

	out := make(map[string]string, len(cfg.Reservations))
	for _, r := range cfg.Reservations {
		mac, err := core.NormalizeMAC(r.MAC)
		if err != nil {
			// Validation rejects these on the way in, so one here means a
			// hand-edited file. Skipped rather than failing the whole list: the
			// dhcp module's own surface is where that complaint belongs.
			continue
		}
		if r.IP.IsValid() {
			out[mac] = r.IP.String()
		}
	}
	return out, nil
}
