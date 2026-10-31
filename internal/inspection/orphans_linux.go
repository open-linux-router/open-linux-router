//go:build linux

package inspection

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
)

// killOrphans stops a mitmweb left behind by a hard kill. SIGKILL, a panic or
// the OOM killer runs no shutdown, so the child keeps our ports and the
// resumed switch could never bind them. The confdir is part of the command
// line we start it with and unique to this router, so it identifies our
// process and nothing else's.
func (s *Service) killOrphans() {
	for _, pid := range orphanPIDs("/proc", s.Dir) {
		terminate(pid)
	}
}

// orphanPIDs lists mitmweb processes whose command line names dir. procRoot is
// a parameter so a test can stage a fake /proc.
func orphanPIDs(procRoot, dir string) []int {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil
	}
	want := []byte("confdir=" + dir)
	var out []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join(procRoot, entry.Name(), "cmdline"))
		if err != nil || !bytes.Contains(cmdline, []byte("mitmweb")) {
			continue
		}
		if bytes.Contains(cmdline, want) {
			out = append(out, pid)
		}
	}
	return out
}
