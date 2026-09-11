//go:build linux

package ingress

import (
	"github.com/open-linux-router/open-linux-router/internal/core"
)

// PortConflict reports which of the proxy's ports are already held.
//
// This is the most likely reason enabling this module fails, and it fails on
// exactly the boxes most likely to try it: anything that has ever run nginx,
// Apache, or the distro's own Caddy is already on :80. Letting systemd discover
// it produces "olr-caddy.service: Failed with result 'exit-code'", which names
// neither the cause nor the fix.
//
// As in `dhcp`, we look first and refuse with an explanation rather than
// declaring Conflicts= in the unit and stopping somebody else's daemon — that is
// machine-wide interference of exactly the kind design.md §3.4 forbids.
//
// The procfs scan itself is core's, shared with `dhcp` and `dns`, because a
// second copy is a second place to fix a parsing bug.
func PortConflict() ([]uint64, error) {
	var held []uint64
	for _, p := range proxyPorts {
		inUse, err := core.TCPPortInUse(p)
		if err != nil {
			return nil, err
		}
		if inUse {
			held = append(held, p)
		}
	}
	return held, nil
}
