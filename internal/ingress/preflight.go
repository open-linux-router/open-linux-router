package ingress

import (
	"fmt"
	"strings"
)

// The ports the proxy binds. 443 serves everything; 80 exists only to redirect
// to it (docs/ingress.md §6).
var proxyPorts = []uint64{80, 443}

// ErrPortInUse explains a refused start, naming the command that finds the
// incumbent.
//
// Deliberately *not* behind a build tag, even though PortConflict is. Detecting
// a held port needs procfs; explaining one is string formatting. Splitting them
// keeps the message identical on every platform, which matters because the
// refusal text is a thing worth testing and the alternative is a test that only
// passes on Linux — one of those already exists in `dhcp`, red on the dev
// machine for no reason anyone benefits from.
func ErrPortInUse(held []uint64) error {
	ports := make([]string, len(held))
	filters := make([]string, len(held))
	for i, p := range held {
		ports[i] = fmt.Sprintf("TCP/%d", p)
		filters[i] = fmt.Sprintf("sport = :%d", p)
	}
	return fmt.Errorf(
		"%s already in use, so another web server is running on this box.\n"+
			"olr runs its own proxy and will not stop somebody else's daemon.\n"+
			"Find the holder with `ss -ltnp '%s'` and stop it, or leave the port to it",
		strings.Join(ports, " and "), strings.Join(filters, " or "))
}
