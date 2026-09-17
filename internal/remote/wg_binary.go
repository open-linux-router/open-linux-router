package remote

import (
	"errors"
	"fmt"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Finding the one binary this module drives.
//
// olr does not ship WireGuard and there is nothing to ship: the data path is in
// the kernel. What has to be installed is `wireguard-tools`, which is two
// binaries and no service, in every distribution's repository — see deps.go for
// why that makes it the easiest dependency in olr rather than the hardest.
//
// The same relationship every other module has with its backend, with one
// difference worth noting because it makes the failure friendlier: `dhcp`
// without dnsmasq cannot serve an address and `ingress` without a proxy cannot
// obtain a certificate, but the thing missing here is a *configuration* tool.
// An operator who installs it later loses nothing that was already running.

// ErrNoBinary reports that `wg` could not be found.
var ErrNoBinary = errors.New("wireguard-tools is not installed")

// FindBinary returns the `wg` binary, or ErrNoBinary.
//
// core.LookTool rather than exec.LookPath, because it also searches the sbin
// directories — which is where several distributions put network tools, and not
// on an ordinary user's $PATH.
func FindBinary() (string, error) {
	if path := core.LookTool("wg"); path != "" {
		return path, nil
	}
	return "", ErrNoBinary
}

// ErrBinaryMissing explains how to satisfy FindBinary.
//
// The install command itself is not here: `core.Dependency` already produces
// one for the distribution actually running, and a second hand-written command
// in this message would be the copy that goes stale (deps.go).
func ErrBinaryMissing() error {
	return fmt.Errorf(
		"%w, so olr cannot configure the tunnel.\n\n"+
			"The tunnel itself is in the kernel — what is missing is `wg`, the tool that "+
			"loads keys into it. It is in every distribution's repository and installing it "+
			"starts nothing.\n"+
			"`olr remote status` names the package for this box", ErrNoBinary)
}
