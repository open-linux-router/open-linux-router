package remote

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Finding the Shadowsocks server the operator supplied.
//
// olr does not ship one, which is the same relationship every other module has
// with its backend — and here it is forced rather than chosen. **No
// distribution packages shadowsocks-rust.** Debian carries shadowsocks-libev,
// which upstream has declared bug-fix-only and which was dropped from testing
// and restored in the space of a fortnight; the Rust implementation is the
// shadowsocks project's own and its stated direction, and it ships as a release
// binary and nothing else.
//
// So this is internal/ingress's situation rather than internal/dhcp's: not a
// package with a name per distribution, but a file somebody fetched. Two things
// follow, and both are stated where an operator meets them rather than only
// here:
//
//   - every failure along this path says exactly what is missing and where to
//     get it, because there is no `apt install` line to fall back on; and
//   - **security updates are the operator's.** A router nobody touches for two
//     years is exactly where that matters, and saying so is the honest price of
//     the choice rather than a footnote.

// ShadowsocksSearchPath is where a server binary is looked for, in order.
//
// The first entry is the one documented and the one to prefer: a binary olr
// found there is unambiguously the one an operator put there for olr.
// `ssserver` on PATH is a fallback for somebody who installed it their own way.
var ShadowsocksSearchPath = []string{
	"/usr/lib/open-linux-router/ssserver",
}

// ErrNoShadowsocks reports that no server binary could be found.
var ErrNoShadowsocks = errors.New("no Shadowsocks server found")

// FindShadowsocks returns the server binary to use, or ErrNoShadowsocks.
func FindShadowsocks() (string, error) {
	for _, path := range ShadowsocksSearchPath {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		return path, nil
	}
	if path, err := exec.LookPath("ssserver"); err == nil {
		return path, nil
	}
	// The sbin directories too, for the same reason core.LookTool searches
	// them: several distributions put network daemons there and it is not on an
	// ordinary user's $PATH.
	if path := core.LookTool("ssserver"); path != "" {
		return path, nil
	}
	return "", ErrNoShadowsocks
}

// shadowsocksInstallHint is the half of every message on this path that an
// operator can act on. Written once because it is needed twice — as a blocker's
// fix and as an apply refusal — and those must not drift apart.
func shadowsocksInstallHint() string {
	return fmt.Sprintf(`# Download the server for this machine's architecture from
#   https://github.com/shadowsocks/shadowsocks-rust/releases
# then put the ssserver binary here and make it executable:

sudo install -m 0755 ssserver %s`, ShadowsocksSearchPath[0])
}

// ErrShadowsocksMissing explains how to satisfy FindShadowsocks.
//
// Long, and deliberately so: this is the message standing between an operator
// and the feature, and there is no package manager command that would shorten
// it.
func ErrShadowsocksMissing() error {
	return fmt.Errorf(
		"no Shadowsocks server found at %s, and no `ssserver` on PATH.\n\n"+
			"olr does not ship one, and no distribution packages the implementation it drives —\n"+
			"shadowsocks-rust is the shadowsocks project's own and is published as a release\n"+
			"binary. Keeping it up to date is therefore yours rather than apt's.\n\n"+
			"%s",
		ShadowsocksSearchPath[0], shadowsocksInstallHint())
}
