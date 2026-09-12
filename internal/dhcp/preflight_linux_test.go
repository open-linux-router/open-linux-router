//go:build linux

package dhcp

import (
	"os"
	"testing"
)

// The procfs parsing itself is core's and is tested there, against fixtures.
// What is this module's to prove is that it asks about the right port and that
// the question can be put to the live system at all. The refusal text is not
// here: it no longer depends on the platform, so it is tested in
// preflight_test.go where a laptop runs it too.

func TestPortConflictReadsRealProcfs(t *testing.T) {
	if _, err := os.Stat("/proc/net/udp"); os.IsNotExist(err) {
		t.Skip("no /proc/net/udp")
	}
	if _, err := PortConflict(); err != nil {
		t.Errorf("PortConflict() on the live system: %v", err)
	}
}
