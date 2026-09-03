//go:build linux

package dhcp

import (
	"os"
	"strings"
	"testing"
)

// The procfs parsing itself is core's and is tested there, against fixtures.
// What is this module's to prove is that it asks about the right port and that
// the question can be put to the live system at all.

func TestPortConflictReadsRealProcfs(t *testing.T) {
	if _, err := os.Stat("/proc/net/udp"); os.IsNotExist(err) {
		t.Skip("no /proc/net/udp")
	}
	if _, err := PortConflict(); err != nil {
		t.Errorf("PortConflict() on the live system: %v", err)
	}
}

// The refusal has to name the port, the daemon and a way to find the incumbent.
// An operator who hits this is looking at a DHCP server that would not start,
// and "port in use" alone does not tell them whose it is.
func TestErrPortInUseNamesThePortAndAWayToFindTheHolder(t *testing.T) {
	msg := ErrPortInUse().Error()
	for _, want := range []string{"67", "dnsmasq", "ss -lunp"} {
		if !strings.Contains(msg, want) {
			t.Errorf("ErrPortInUse() missing %q:\n%s", want, msg)
		}
	}
}
