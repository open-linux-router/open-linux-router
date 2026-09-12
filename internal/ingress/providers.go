package ingress

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// Which DNS providers are available is a property of the operator's binary, so
// it is asked rather than declared.
//
// This file used to hold a hand-written list, and that list was a defect with a
// note attached: the legal set is whatever the proxy binary was linked with, so
// anything written down here was a second copy that could disagree with it. The
// copy is gone. `caddy list-modules` reports what a binary actually has, and no
// other answer is authoritative.
//
// The consequence runs backwards through the module and is worth stating once,
// because three other files got smaller because of it:
//
//   - the config schema publishes no enum (schema.go), because the set is not
//     known at reflection time and a stale enum is worse than none;
//   - Validate does not check the provider name (validate.go), because it is
//     pure by design and cannot run a subprocess; and
//   - the real check is the one that was already there — `caddy validate` on
//     the rendered file (apply.go), which rejects `dns <provider>` outright when
//     that module is not linked, in the binary's own words.
//
// So the check moved from a place that could be wrong to a place that cannot.

// providerPrefix is how Caddy names a DNS provider module.
const providerPrefix = "dns.providers."

// ListProviders asks a proxy binary which DNS providers it was built with.
func ListProviders(ctx context.Context, binary string) ([]string, error) {
	if binary == "" {
		return nil, ErrNoBinary
	}
	out, err := exec.CommandContext(ctx, binary, "list-modules").Output()
	if err != nil {
		return nil, fmt.Errorf("asking %s which DNS providers it has: %w", binary, err)
	}
	return parseProviders(string(out)), nil
}

// parseProviders pulls the provider names out of `caddy list-modules` output.
//
// The command prints one module per line, plus headings and a trailing summary.
// Taking the first field of lines carrying the prefix ignores all of that
// without having to model any of it, and a format change costs an empty list
// rather than a wrong one.
func parseProviders(out string) []string {
	names := []string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name, ok := strings.CutPrefix(fields[0], providerPrefix)
		if !ok || name == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// HasProviders reports whether a binary can obtain a certificate at all.
//
// Its own function because "found a proxy" and "found a proxy that can do the
// job" are different answers, and on this path the second is the one that is
// usually false — see ErrNoProviders.
func HasProviders(providers []string) bool { return len(providers) > 0 }
