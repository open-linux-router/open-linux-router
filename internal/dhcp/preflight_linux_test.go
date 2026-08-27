//go:build linux

package dhcp

import (
	"os"
	"path/filepath"
	"testing"
)

// A real /proc/net/udp header plus rows. Port 67 is 0x0043 and 53 is 0x0035,
// which is the whole reason this is parsed by hand rather than shelled out to
// ss: the format is fixed and the field is hex.
const procNetUDP = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode ref pointer drops
  123: 00000000:0035 00000000:0000 07 00000000:00000000 00:00000000 00000000   101        0 34567 2 0000000000000000 0
  456: 0100007F:9CA1 00000000:0000 07 00000000:00000000 00:00000000 00000000  1000        0 45678 2 0000000000000000 0
`

const procNetUDPWithDHCP = procNetUDP +
	"  789: 00000000:0043 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 56789 2 0000000000000000 0\n"

func TestPortListedIn(t *testing.T) {
	dir := t.TempDir()

	free := filepath.Join(dir, "udp-free")
	busy := filepath.Join(dir, "udp-busy")
	if err := os.WriteFile(free, []byte(procNetUDP), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(busy, []byte(procNetUDPWithDHCP), 0o644); err != nil {
		t.Fatal(err)
	}

	if inUse, err := portListedIn(free, dhcpServerPort); err != nil || inUse {
		t.Errorf("portListedIn(free) = %v, %v; want false, nil", inUse, err)
	}
	if inUse, err := portListedIn(busy, dhcpServerPort); err != nil || !inUse {
		t.Errorf("portListedIn(busy) = %v, %v; want true, nil", inUse, err)
	}

	// DNS is on 53 in both fixtures — proves the hex parse is not matching by
	// accident.
	if inUse, err := portListedIn(free, 53); err != nil || !inUse {
		t.Errorf("portListedIn(free, 53) = %v, %v; want true, nil", inUse, err)
	}
}

// A missing procfs entry must read as "no conflict", not as an error that
// blocks a legitimate start.
func TestPortListedInHandlesMissingFile(t *testing.T) {
	inUse, err := portListedIn(filepath.Join(t.TempDir(), "absent"), dhcpServerPort)
	if err != nil || inUse {
		t.Errorf("got %v, %v; want false, nil", inUse, err)
	}
}

func TestPortListedInIgnoresGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "udp")
	garbage := "header\nnot a row\n  1: nocolon 0 0\n  2: ZZZZ:ZZZZ 0 0\n"
	if err := os.WriteFile(path, []byte(garbage), 0o644); err != nil {
		t.Fatal(err)
	}
	if inUse, err := portListedIn(path, dhcpServerPort); err != nil || inUse {
		t.Errorf("got %v, %v; want false, nil", inUse, err)
	}
}

// The real procfs must at least parse. Whether anything holds 67 here is not
// asserted — that is the host's business, not the test's.
func TestPortConflictReadsRealProcfs(t *testing.T) {
	if _, err := os.Stat("/proc/net/udp"); os.IsNotExist(err) {
		t.Skip("no /proc/net/udp")
	}
	if _, err := PortConflict(); err != nil {
		t.Errorf("PortConflict() on the live system: %v", err)
	}
}
