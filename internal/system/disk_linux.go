package system

import "golang.org/x/sys/unix"

// readRootDisk reports the root filesystem; a failed read remains unknown.
func readRootDisk() (*uint64, *uint64) {
	var stat unix.Statfs_t
	if err := unix.Statfs("/", &stat); err != nil || stat.Bsize <= 0 || stat.Bfree > stat.Blocks {
		return nil, nil
	}
	total := stat.Blocks * uint64(stat.Bsize)
	used := (stat.Blocks - stat.Bfree) * uint64(stat.Bsize)
	return &used, &total
}
