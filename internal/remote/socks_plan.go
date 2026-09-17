package remote

import (
	"bytes"
	"slices"
	"sort"
)

// The SOCKS5 proxy's plan.
//
// It reuses ss_plan.go's vocabulary — FileChange, ServiceAction, ProxyObserved,
// ProxyPlan — rather than defining a parallel set, and that is a different
// decision from the one the module makes about the tunnel. The tunnel is kernel
// state, so sharing a plan type with it would produce a struct whose every field
// is empty half the time. These two are both "render a file, drive a unit": the
// same mechanism, so the same plan shape, discovered rather than invented.
//
// What is *not* shared is what makes a change disruptive, because that is about
// the object rather than the mechanism. Shadowsocks turns on the secret, the
// cipher and the dialled address. Here the cipher does not exist, and one thing
// no Shadowsocks change can do appears instead: **moving the listener between
// the tunnel and the internet**, which changes who can reach the proxy at all.

// BuildSocksPlan renders the desired config and diffs it against what is on
// disk and running.
//
// `previous` is the config as stored, for the same reason the other two plans
// take one: "does every client need a new link" is answered by comparing what is
// about to be written against what produced the links people already hold.
func BuildSocksPlan(c, previous Config, paths Paths, obs ProxyObserved) (ProxyPlan, Rendered, error) {
	result := ValidateSocks(c)
	validateEndpoint(&result, c)
	if err := result.Err(); err != nil {
		return ProxyPlan{Validation: result}, Rendered{}, err
	}

	rendered, err := RenderSocks(c, paths)
	if err != nil {
		return ProxyPlan{Validation: result}, Rendered{}, err
	}

	plan := ProxyPlan{Validation: result}
	if !c.Socks.Enabled {
		// A disabled proxy renders no file, rather than a file that configures
		// nothing. There is no "valid and serves nobody" form of a 3proxy
		// configuration — `auth strong` with no `users` line is a server nobody
		// can authenticate to — so the honest rendering of "off" is the absence
		// of the file, and the unit stopping.
		rendered = Rendered{}
	}

	wanted := rendered.Paths()
	for _, f := range rendered.Files {
		before, exists := obs.Files[f.Path]
		if exists && bytes.Equal(before, f.Data) {
			continue
		}
		kind := FileCreate
		if exists {
			kind = FileUpdate
		}
		plan.Changes = append(plan.Changes, FileChange{
			Path: f.Path, Kind: kind, Impact: ImpactRestart,
			Secret: f.Secret, Before: before, After: f.Data,
		})
	}
	for path := range obs.Files {
		if slices.Contains(wanted, path) {
			continue
		}
		plan.Changes = append(plan.Changes, FileChange{
			Path: path, Kind: FileDelete, Impact: ImpactRestart, Secret: true, Before: obs.Files[path],
		})
	}
	sort.Slice(plan.Changes, func(i, j int) bool { return plan.Changes[i].Path < plan.Changes[j].Path })

	plan.Action = proxyAction(c.Socks.Enabled, obs.Running, len(plan.Changes) > 0)
	if obs.ServiceKnown && c.Socks.Enabled != obs.EnabledAtBoot {
		want := c.Socks.Enabled
		plan.Enable = &want
	}

	plan.Impact, plan.Reasons = classifySocks(c, previous, plan, obs)
	return plan, rendered, nil
}

// classifySocks reduces the plan to one impact plus the reasons behind it.
//
// Same test as its neighbour — §5.3.3 wants `disruptive` to be a fact, and the
// fact is whether a link people already hold still works — over a different set
// of values: the credential, the address, and the scope.
func classifySocks(c, previous Config, plan ProxyPlan, obs ProxyObserved) (Impact, []string) {
	if plan.Empty() {
		return ImpactNone, nil
	}

	impact := ImpactNone
	for _, ch := range plan.Changes {
		impact = max(impact, ch.Impact)
	}

	var reasons []string
	before, after := previous.Socks, c.Socks

	// Only if there was something to invalidate. A box being set up for the
	// first time has handed nobody a link, and calling that disruptive would be
	// the crying wolf §5.3.3 warns about.
	issued := before.Password != "" && before.Enabled

	if issued && before.Password != after.Password {
		impact = ImpactDisruptive
		reasons = append(reasons,
			"the password changes, so every link already handed out stops working and every "+
				"device has to be given the new one")
	}
	if issued && before.UserOrDefault() != after.UserOrDefault() {
		impact = ImpactDisruptive
		reasons = append(reasons,
			"the username changes, and a link carries it — every device has to be given a new one")
	}

	// The move this object has and the other does not. It is disruptive in both
	// directions and for different reasons, so each gets its own sentence rather
	// than a shared "the scope changed".
	if issued && before.ListenScopeOrDefault() != after.ListenScopeOrDefault() {
		impact = ImpactDisruptive
		switch after.ListenScopeOrDefault() {
		case ListenTunnel:
			reasons = append(reasons,
				"the proxy moves inside the tunnel, so it stops answering on this box's public "+
					"address — only devices already dialled in to the tunnel can reach it, and "+
					"every link has to be given out again")
		case ListenInternet:
			reasons = append(reasons,
				"the proxy moves out onto the internet, so it answers at this box's public "+
					"address in the clear, and every link has to be given out again")
		}
	}

	// The address a client dials depends on the scope, so this compares the
	// rendered links rather than the endpoint — a tunnel-scoped proxy does not
	// move when the public endpoint does, and saying it did would be a
	// disruptive warning about nothing.
	//
	// A link that cannot be produced on either side is not a change worth
	// reporting: there was nothing to invalidate, or there is nothing to hand
	// out yet.
	if issued && before.ListenScopeOrDefault() == after.ListenScopeOrDefault() {
		oldURL, oldErr := SocksClientURL(previous)
		newURL, newErr := SocksClientURL(c)
		if oldErr == nil && newErr == nil && oldURL != newURL &&
			before.Password == after.Password && before.UserOrDefault() == after.UserOrDefault() {
			impact = ImpactDisruptive
			reasons = append(reasons,
				"the address devices dial changes, so every link already handed out points at the old one")
		}
	}

	if plan.Action == ActionStop && obs.Running {
		impact = max(impact, ImpactDisruptive)
		reasons = append(reasons, "the proxy stops, so no device can use this box's way out")
	}

	if impact == ImpactRestart {
		reasons = append(reasons,
			"the proxy restarts, which cuts connections through it; clients reconnect by themselves")
	}

	return impact, reasons
}
