package system

import "testing"

func TestReadRootDisk(t *testing.T) {
	used, total := readRootDisk()
	if used == nil || total == nil || *total == 0 || *used > *total {
		t.Fatalf("invalid root filesystem usage: used=%v total=%v", used, total)
	}
}
