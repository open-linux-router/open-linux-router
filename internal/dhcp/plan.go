package dhcp

import (
	"bytes"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// Planning is deliberately pure and reads *observed* state rather than a cached
// copy of what we last wrote. That is what makes drift free (design.md §5.4):
// "have we drifted?" is just "plan against unchanged intent and see if the diff
// is empty", so there is no separate health machinery to keep in step.

// Impact says what applying a change will cost, so a UI can warn instead of
// spinning (design.md §5.3.3).
type Impact int

const (
	// ImpactNone changes nothing that is running.
	ImpactNone Impact = iota
	// ImpactReload is picked up by a SIGHUP. Clients notice nothing.
	ImpactReload
	// ImpactRestart bounces the daemon. Leases survive in the database, so
	// clients keep their addresses; a request arriving during the restart is
	// retried by the client.
	ImpactRestart
	// ImpactDisruptive will take addresses away from clients that hold them.
	ImpactDisruptive
)

func (i Impact) String() string {
	switch i {
	case ImpactNone:
		return "none"
	case ImpactReload:
		return "reload"
	case ImpactRestart:
		return "restart"
	case ImpactDisruptive:
		return "disruptive"
	}
	return fmt.Sprintf("Impact(%d)", int(i))
}

// MarshalText makes Impact a JSON string.
func (i Impact) MarshalText() ([]byte, error) { return []byte(i.String()), nil }

// ChangeKind is what happens to a file.
type ChangeKind string

const (
	ChangeCreate ChangeKind = "create"
	ChangeUpdate ChangeKind = "update"
	ChangeDelete ChangeKind = "delete"
)

// Change is one file's worth of pending work.
type Change struct {
	Path   string     `json:"path"`
	Kind   ChangeKind `json:"kind"`
	Impact Impact     `json:"impact"`

	// Before and After are the file contents. Omitted from JSON by the CLI
	// unless asked for, since a rendered config is long.
	Before []byte `json:"-"`
	After  []byte `json:"-"`
}

// ServiceAction is what the daemon needs after the files are written.
type ServiceAction string

const (
	ActionNone    ServiceAction = "none"
	ActionStart   ServiceAction = "start"
	ActionStop    ServiceAction = "stop"
	ActionReload  ServiceAction = "reload"
	ActionRestart ServiceAction = "restart"
)

// Observed is the actual state of the system, read fresh.
type Observed struct {
	// Files is the current content of every file under the module's rendered
	// directories. A path absent from the map does not exist on disk. It must
	// include files we did not render, or a stale reservation left behind by a
	// previous config would never be noticed.
	Files map[string][]byte

	// Running reports whether the backend's unit is active.
	Running bool

	// Leases is the current lease database, used to tell a restart from a
	// genuine disruption.
	Leases []Lease
}

// Plan is the full answer to "what would applying this config do?".
type Plan struct {
	Backend string        `json:"backend"`
	Changes []Change      `json:"changes"`
	Action  ServiceAction `json:"action"`
	Impact  Impact        `json:"impact"`

	// Reasons explains the impact in the operator's terms — most importantly
	// which clients a disruptive change would drop.
	Reasons []string `json:"reasons,omitempty"`

	// Validation carries warnings even on success.
	Validation Result `json:"-"`
}

// Empty reports whether applying would do nothing at all. This is the drift
// check (design.md §5.4).
func (p Plan) Empty() bool { return len(p.Changes) == 0 && p.Action == ActionNone }

// BuildPlan renders the desired config and diffs it against what is actually
// on disk and running.
//
// It validates first and returns the error rather than planning against a
// config that cannot be applied — the whole value of validation is that it
// happens before anything is written (design.md §5.3.1).
func BuildPlan(b Dnsmasq, desired Config, links LinkView, obs Observed, now time.Time) (Plan, error) {
	result := Validate(desired, links)
	if err := result.Err(); err != nil {
		return Plan{Validation: result}, err
	}

	rendered, err := b.Render(desired, links)
	if err != nil {
		return Plan{Validation: result}, err
	}

	plan := Plan{Backend: b.Name(), Validation: result}
	wantReloadOnly := true

	for _, f := range rendered.Files {
		before, exists := obs.Files[f.Path]
		if exists && bytes.Equal(before, f.Data) {
			continue
		}
		kind := ChangeCreate
		if exists {
			kind = ChangeUpdate
		}
		impact := ImpactRestart
		if f.Reloadable {
			impact = ImpactReload
		} else {
			wantReloadOnly = false
		}
		plan.Changes = append(plan.Changes, Change{
			Path: f.Path, Kind: kind, Impact: impact, Before: before, After: f.Data,
		})
	}

	// Files on disk that the current config no longer produces. Without this a
	// removed reservation would keep being served: dnsmasq reads whatever is in
	// the directory, not whatever we meant to put there.
	wanted := rendered.Paths()
	for path := range obs.Files {
		if slices.Contains(wanted, path) {
			continue
		}
		reloadable := b.reloadable(path)
		if !reloadable {
			wantReloadOnly = false
		}
		impact := ImpactRestart
		if reloadable {
			impact = ImpactReload
		}
		plan.Changes = append(plan.Changes, Change{
			Path: path, Kind: ChangeDelete, Impact: impact, Before: obs.Files[path],
		})
	}

	sort.Slice(plan.Changes, func(i, j int) bool { return plan.Changes[i].Path < plan.Changes[j].Path })

	plan.Action = serviceAction(desired.Enabled, obs.Running, len(plan.Changes) > 0, wantReloadOnly)
	plan.Impact, plan.Reasons = classify(desired, plan, obs, now)

	return plan, nil
}

// serviceAction decides what to do with the daemon after writing files.
func serviceAction(enabled, running, changed, reloadOnly bool) ServiceAction {
	switch {
	case enabled && !running:
		return ActionStart
	case !enabled && running:
		return ActionStop
	case !enabled:
		// Already stopped. Files may still change so that enabling later is a
		// plain start, but nothing needs signalling.
		return ActionNone
	case !changed:
		return ActionNone
	case reloadOnly:
		return ActionReload
	default:
		return ActionRestart
	}
}

// classify reduces the plan to a single impact plus the reasons behind it.
func classify(desired Config, plan Plan, obs Observed, now time.Time) (Impact, []string) {
	impact := ImpactNone
	for _, c := range plan.Changes {
		impact = max(impact, c.Impact)
	}
	switch plan.Action {
	case ActionRestart, ActionStart:
		impact = max(impact, ImpactRestart)
	case ActionReload:
		impact = max(impact, ImpactReload)
	}

	var reasons []string

	// The honest question is not "did a range field change" but "will a client
	// lose the address it is using". Answering it from the live lease database
	// is what makes `disruptive` a fact rather than a guess.
	if dropped := Dropped(desired, obs.Leases, now); len(dropped) > 0 && obs.Running {
		impact = ImpactDisruptive
		reasons = append(reasons, describeDropped(desired, dropped))
	}

	if plan.Action == ActionStop {
		impact = ImpactDisruptive
		reasons = append(reasons, "the DHCP service will stop; no client will be able to renew")
	}

	return impact, reasons
}

// Dropped returns the active leases that the desired config would stop serving.
//
// The question is deliberately "will this client keep the address it is holding
// right now", not "did a range field change". Answering it from the live lease
// database is what makes `disruptive` a fact rather than a guess (design.md
// §11.3), so the cases below are about the client's experience, not ours.
func Dropped(c Config, leases []Lease, now time.Time) []Lease {
	var dropped []Lease
	for _, l := range leases {
		if !l.Active(now) {
			continue
		}
		if !c.Enabled {
			// Nothing renews, so every live lease is lost regardless of family.
			dropped = append(dropped, l)
			continue
		}
		if !l.IP.Is4() {
			// IPv6 leases are governed by the ra field rather than by a range,
			// and dnsmasq's lease file does not record which interface a lease
			// was handed out on — so there is nothing here to compare them
			// against. Counting them as dropped would mark *every* apply on an
			// IPv6-enabled box disruptive, which would train operators to
			// ignore the word. Left for when the lease event stream (§11.5)
			// supplies the interface.
			continue
		}
		if servedBy(c, l) {
			continue
		}
		dropped = append(dropped, l)
	}
	return dropped
}

// servedBy reports whether the client holding this lease keeps this address.
func servedBy(c Config, l Lease) bool {
	// A reservation matching the client's MAC is decisive and outranks every
	// range: dnsmasq will hand that client exactly the reserved address and
	// nothing else. So the lease survives only if the reservation names the
	// address the client already holds — pinning a client to a *different*
	// address moves it, which is a client losing the address it holds.
	if l.MAC != "" {
		for _, r := range c.Reservations {
			if r.MAC == l.MAC {
				return r.IP == l.IP
			}
		}
	}

	// The address is reserved for somebody else, so this holder loses it.
	for _, r := range c.Reservations {
		if r.IP == l.IP {
			return false
		}
	}

	for _, p := range c.Pools {
		if inRange(p.Start, p.End, l.IP) {
			return true
		}
	}
	return false
}

func describeDropped(c Config, dropped []Lease) string {
	names := make([]string, 0, len(dropped))
	for _, l := range dropped {
		switch {
		case l.Hostname != "":
			names = append(names, fmt.Sprintf("%s (%s)", l.Hostname, l.IP))
		default:
			names = append(names, l.IP.String())
		}
	}
	sort.Strings(names)

	const show = 4
	listed := names
	suffix := ""
	if len(listed) > show {
		listed, suffix = listed[:show], fmt.Sprintf(" and %d more", len(names)-show)
	}

	verb := "will lose the address it holds"
	if len(dropped) > 1 {
		verb = "will lose the addresses they hold"
	}
	if !c.Enabled {
		return fmt.Sprintf("%d client(s) %s: %s%s", len(dropped), verb, strings.Join(listed, ", "), suffix)
	}
	return fmt.Sprintf("%d client(s) %s because no pool or reservation covers them any more: %s%s",
		len(dropped), verb, strings.Join(listed, ", "), suffix)
}
