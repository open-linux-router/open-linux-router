package core

import (
	"fmt"
	"strings"
)

// Holder names the process holding a port, for a message to an operator.
//
// Knowing that :53 is taken is not actionable; knowing that `dnsmasq.service`
// has it is. The modules that refuse to start used to guess at the culprit —
// internal/dns named systemd-resolved, because that is the usual incumbent —
// and a guess sends the operator to edit the wrong file when it is wrong. On an
// olr box it is wrong often: we tell people to install dnsmasq, and Debian's
// dnsmasq package ships a service that binds :53 the moment it is installed. So
// we look, rather than guess.
//
// Unit is the actionable field. A pid can be killed and comes back at the next
// boot; a unit can be disabled.
//
// The type and its rendering are deliberately outside the build tags that cover
// the lookup. Only *finding* a holder needs procfs; what an operator reads must
// be the same text everywhere, and a String() that existed only on Linux is how
// the refusal messages came to render as "It is held by ." off it.
type Holder struct {
	PID  int
	Name string // /proc/<pid>/comm
	Unit string // the systemd unit from the cgroup path, empty if not under one
}

// String renders a holder for an error message, degrading as it learns less.
func (h Holder) String() string {
	switch {
	case h.Name == "" && h.PID == 0:
		return ""
	case h.Name == "":
		return fmt.Sprintf("pid %d", h.PID)
	case h.Unit == "":
		return fmt.Sprintf("%s (pid %d)", h.Name, h.PID)
	default:
		return fmt.Sprintf("%s (pid %d, %s)", h.Name, h.PID, h.Unit)
	}
}

// unitOf pulls the systemd unit out of a process's cgroup file.
//
// Handles both hierarchies, because a box running cgroup v1 is exactly the kind
// of older system where an unexpected daemon is holding :53. v2 lines are
// "0::<path>"; v1 lines are "<id>:<controllers>:<path>". Either way the unit is
// a path component, and the *last* matching one is the answer — a service
// started from inside a user manager sits below `user@1000.service`, and the
// leaf is the one an operator can act on.
func unitOf(cgroup string) string {
	unit := ""
	for _, line := range strings.Split(cgroup, "\n") {
		path := line
		if i := strings.LastIndex(line, ":"); i >= 0 {
			path = line[i+1:]
		}
		for _, part := range strings.Split(path, "/") {
			if strings.HasSuffix(part, ".service") {
				unit = part
			}
		}
	}
	return unit
}
