package ingress

import (
	"fmt"
	"strings"
)

// Only accept a complete repair of an existing service, with a strict reduction
// in existing errors and no new errors. Normal edits still require valid config.
func canSaveRepair(before, after Config, name string, dns DNSView, devices DeviceView) bool {
	name = strings.TrimSuffix(strings.ToLower(name), "."+dns.LocalDomain())
	old := Validate(before, dns, devices).Errors
	next := Validate(after, dns, devices).Errors
	if len(next) >= len(old) {
		return false
	}
	found := false
	for _, s := range before.Services {
		if s.Name == name {
			found = true
		}
	}
	if !found {
		return false
	}
	known := make(map[Problem]bool, len(old))
	for _, p := range old {
		known[p] = true
	}
	for _, p := range next {
		if !known[p] {
			return false
		}
		for i, s := range after.Services {
			if s.Name == name && strings.HasPrefix(p.Path, fmt.Sprintf("services[%d]", i)) {
				return false
			}
		}
	}
	return true
}
