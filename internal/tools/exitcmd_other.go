//go:build !linux

package tools

import (
	"errors"
	"os/exec"
)

// A way out is an fwmark and a routing table, which only Linux has. The
// default route still runs the external tools, so this only refuses the case
// that actually needs the kernel feature.
func markCommand(_ *exec.Cmd, mark uint32) (func(), error) {
	if mark == 0 {
		return func() {}, nil
	}
	return nil, errors.New("running an external speed test through a way out requires Linux")
}
