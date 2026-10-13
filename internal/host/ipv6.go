package host

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

type ipv6Defaults struct {
	AcceptRA string `json:"accept_ra"`
	Autoconf string `json:"autoconf"`
}

// CheckIPv6Auto refuses managers we cannot configure, rather than displaying a
// successful switch while a userspace client keeps acquiring IPv6 addresses.
func (a Applier) CheckIPv6Auto(settings map[string]bool) error {
	for iface := range settings {
		if err := ipv6InterfaceName(iface); err != nil {
			return err
		}
		if findings := a.otherManagers(iface); len(findings) > 0 {
			return fmt.Errorf("cannot control automatic IPv6 on %s while another network manager configures it; configure IPv6 in that manager first", iface)
		}
	}
	return nil
}

func ipv6InterfaceName(iface string) error {
	if iface == "" || iface == "." || iface == ".." || strings.ContainsAny(iface, "/\\\x00\n\r") || len(iface) > 15 {
		return fmt.Errorf("invalid interface name %q", iface)
	}
	return nil
}

func (a Applier) ipv6Key(iface, key string) string {
	return a.path("/proc/sys/net/ipv6/conf/" + iface + "/" + key)
}

// Save the original kernel settings before the first write. Removing an
// override (including releasing an interface) restores exactly those values.
// Missing interfaces retain their originals until the interface returns.
func (a Applier) applyIPv6Auto(settings map[string]bool, beforeHost ...bool) []core.Step {
	if err := a.CheckIPv6Auto(settings); err != nil {
		return []core.Step{{Description: "configure automatic IPv6 addressing", Error: err.Error()}}
	}
	statePath := filepath.Join(a.stateDir(), "ipv6-auto.json")
	originals := map[string]ipv6Defaults{}
	data, err := os.ReadFile(statePath)
	if err == nil {
		err = json.Unmarshal(data, &originals)
	}
	if err != nil && !os.IsNotExist(err) {
		return []core.Step{{Description: "read original IPv6 settings", Error: err.Error()}}
	}
	if originals == nil {
		originals = map[string]ipv6Defaults{}
	}
	names := make([]string, 0, len(settings)+len(originals))
	for iface := range settings {
		names = append(names, iface)
	}
	for iface := range originals {
		names = append(names, iface)
	}
	slices.Sort(names)
	var steps []core.Step
	save := func() error {
		data, err := json.Marshal(originals)
		if err != nil {
			return err
		}
		return core.WriteFileAtomic(statePath, data, 0o600)
	}
	for _, iface := range slices.Compact(names) {
		if _, controlled := settings[iface]; !controlled && len(beforeHost) > 0 && beforeHost[0] {
			continue // restore released settings only after the host client reloads
		}
		steps = append(steps, step("configure automatic IPv6 addressing on "+iface, func() error {
			if err := ipv6InterfaceName(iface); err != nil {
				return err
			}
			ra, err := os.ReadFile(a.ipv6Key(iface, "accept_ra"))
			if os.IsNotExist(err) {
				return fmt.Errorf("interface %s has no IPv6 settings; reapply when it is available", iface)
			}
			if err != nil {
				return err
			}
			autoconf, err := os.ReadFile(a.ipv6Key(iface, "autoconf"))
			if err != nil {
				return err
			}
			want, controlled := settings[iface]
			original, saved := originals[iface]
			if controlled && !saved {
				original = ipv6Defaults{AcceptRA: strings.TrimSpace(string(ra)), Autoconf: strings.TrimSpace(string(autoconf))}
				originals[iface] = original
				if err := save(); err != nil {
					return err
				}
			}
			next := original
			if controlled {
				next = ipv6Defaults{AcceptRA: "0", Autoconf: "0"}
				if want {
					next = ipv6Defaults{AcceptRA: "2", Autoconf: "1"}
				}
			}
			// Disable address generation before RA acceptance. Enable RA only
			// after generation is enabled, including on a forwarding router.
			for _, setting := range []struct{ key, value string }{{"autoconf", next.Autoconf}, {"accept_ra", next.AcceptRA}} {
				if err := os.WriteFile(a.ipv6Key(iface, setting.key), []byte(setting.value+"\n"), 0o644); err != nil {
					return err
				}
			}
			if !controlled {
				delete(originals, iface)
				return save()
			}
			return nil
		}))
	}
	return steps
}
