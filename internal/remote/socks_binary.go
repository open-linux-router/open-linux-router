package remote

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// SocksSearchPath is where a 3proxy binary is looked for, in order.
//
// The first entry is checked before PATH for the reason the other two backends
// use the same layout: a binary olr found there is unambiguously the one an
// operator put there for olr, and it wins over whatever a distribution may
// later drop in.
var SocksSearchPath = []string{
	"/usr/lib/open-linux-router/3proxy",
}

// ErrNoSocks reports that no 3proxy binary could be found.
var ErrNoSocks = errors.New("no 3proxy server found")

// FindSocks returns the 3proxy binary to use, or ErrNoSocks.
func FindSocks() (string, error) {
	for _, path := range SocksSearchPath {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		return path, nil
	}
	if path, err := exec.LookPath("3proxy"); err == nil {
		return path, nil
	}
	// The sbin directories too, for the reason core.LookTool searches them:
	// 3proxy is a daemon, Debian puts daemons in /usr/sbin, and /usr/sbin is not
	// on an ordinary user's PATH there. Skipping this is how olr would tell an
	// operator with 3proxy installed *and running* that it is not installed.
	if path := core.LookTool("3proxy"); path != "" {
		return path, nil
	}
	return "", ErrNoSocks
}

// socksInstallHint is the half of every message on this path an operator can
// act on. Written once because it is needed twice — as a blocker's fix and as
// an apply refusal — and those must not drift apart.
//
// It names the repository and the channel rather than pasting an apt-key
// incantation, deliberately: the exact signing-key path and sources.list line
// are the repository's to specify and ours to get wrong, and a stale command
// that half-works is worse than a URL that is always right. What olr does assert
// is the part that is olr's judgement — **use the lts channel**.
func socksInstallHint() string {
	return fmt.Sprintf(`# 1. Add 3proxy's official signed apt repository, choosing the **lts** channel
#    (the 0.9 branch — the one whose configuration format stays put across
#    upgrades, which is what a router wants):
#
#      https://3proxy.org/repo/
#
# 2. Then install it:

sudo apt update && sudo apt install 3proxy

# If you would rather not add a repository, put the binary here instead:
#
#   sudo install -m 0755 3proxy %s`, SocksSearchPath[0])
}

func ErrSocksMissing() error {
	return fmt.Errorf(
		"no 3proxy server found at %s, and no `3proxy` on PATH.\n\n"+
			"olr does not ship one. 3proxy is not in Debian, but it publishes its own signed\n"+
			"apt repository — so this is two steps rather than one, and doing them out of\n"+
			"order gives you \"unable to locate package\".\n\n"+
			"%s",
		SocksSearchPath[0], socksInstallHint())
}
