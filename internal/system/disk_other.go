//go:build !linux

package system

func readRootDisk() (*uint64, *uint64) { return nil, nil }
