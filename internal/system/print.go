package system

import (
	"fmt"
	"io"
)

// printAccess renders the access posture for a human.
//
// The unclaimed case leads with what to do rather than with the state, because
// somebody reading this on a fresh box is not asking "what is my posture", they
// are asking "why is nothing working from my laptop".
func printAccess(w io.Writer, v AccessView) {
	switch {
	case !v.Claimed:
		fmt.Fprintf(w, "This router has not been set up yet.\n\n"+
			"It is serving its web UI and refusing everything else over the network.\n"+
			"Open it in a browser to finish, or run one of:\n\n"+
			"  sudo olr claim --no-password\n"+
			"  sudo olr claim --password\n")

	case v.PasswordSet:
		fmt.Fprintf(w, "Set up, and a password is required over the network.\n")

	default:
		// Said plainly, every time it is asked. The operator chose this and the
		// choice was recorded — but it is the kind of fact that should never
		// have to be inferred from the absence of another one.
		fmt.Fprintf(w, "Set up, with no password: anyone who can reach this box on the\n"+
			"network can configure it.\n")
	}
}
