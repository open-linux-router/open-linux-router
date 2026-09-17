package remote

import (
	"bytes"
	"slices"
	"sort"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The proxy's plan: files on disk and a unit, which is internal/ingress's shape
// and the opposite of the tunnel's (wg_plan.go).
//
// The two share the Impact vocabulary and nothing else, and that is the whole
// argument for them being separate plans. A change here is "rewrite a file and
// bounce a daemon"; a change there is "add and remove lines of kernel state".
// One type covering both would have a `changes` field that is a list of files
// half the time and a list of lines the other half.

// FileKind is what happens to a file.
type FileKind string

// The file operations a plan can contain.
const (
	FileCreate FileKind = "create"
	FileUpdate FileKind = "update"
	FileDelete FileKind = "delete"
)

// FileChange is one file's worth of pending work.
type FileChange struct {
	Path   string   `json:"path"`
	Kind   FileKind `json:"kind"`
	Impact Impact   `json:"impact"`

	// Secret suppresses the contents everywhere this change is displayed. The
	// file is still planned, written and compared byte for byte; only its
	// display is withheld — and here that is every file, because the only one
	// there is holds the password.
	Secret bool `json:"secret,omitempty"`

	// Before and After are the file contents, omitted from JSON because a
	// rendered config is long and this one is secret anyway.
	Before []byte `json:"-"`
	After  []byte `json:"-"`
}

// ServiceAction is what the daemon needs after the files are written.
type ServiceAction string

// The service transitions a plan can ask for.
const (
	ActionNone    ServiceAction = "none"
	ActionStart   ServiceAction = "start"
	ActionStop    ServiceAction = "stop"
	ActionRestart ServiceAction = "restart"
)

// ProxyObserved is the actual state of the system, read fresh.
type ProxyObserved struct {
	// Files is the current content of every file under the module's rendered
	// directory. A path absent from the map does not exist on disk.
	Files map[string][]byte

	// Running reports whether the proxy's unit is active.
	Running bool

	// EnabledAtBoot reports whether the unit would start after a reboot. Only
	// meaningful when ServiceKnown.
	EnabledAtBoot bool

	// Installed reports whether the unit file exists at all.
	Installed bool

	// ServiceKnown reports whether the service manager answered. "We could not
	// tell" and "it is off" are different answers and only one of them is
	// honest (design.md §3.4).
	ServiceKnown bool
}

// ProxyPlan is the full answer to "what would applying this do?".
type ProxyPlan struct {
	Changes []FileChange  `json:"changes"`
	Action  ServiceAction `json:"action"`
	Impact  Impact        `json:"impact"`

	// Enable, when non-nil, is the boot-time state the unit has to be moved to.
	// Separate from Action because nothing a client holds changes when a unit
	// is enabled — but it is still work, and still drift when it disagrees with
	// intent.
	Enable *bool `json:"enable,omitempty"`

	// Reasons explains the impact in the operator's terms — above all, whether
	// every client has to be handed a new link.
	Reasons []string `json:"reasons,omitempty"`

	// Validation carries warnings even on success.
	Validation Result `json:"-"`
}

// Empty reports whether applying would change nothing — the drift check
// (design.md §5.4).
func (p ProxyPlan) Empty() bool {
	return len(p.Changes) == 0 && p.Action == ActionNone && p.Enable == nil
}

// BuildProxyPlan renders the desired config and diffs it against what is on
// disk and running.
//
// `previous` is the config as stored, and it is here for the same reason the
// tunnel's plan takes one: the question "does every client need a new link" is
// answered by comparing the secret about to be written against the secret that
// produced the links people already have — not by looking at the new one alone.
func BuildProxyPlan(c, previous Config, paths Paths, obs ProxyObserved) (ProxyPlan, Rendered, error) {
	result := ValidateShadowsocks(c)
	validateEndpoint(&result, c)
	if err := result.Err(); err != nil {
		return ProxyPlan{Validation: result}, Rendered{}, err
	}

	rendered, err := RenderShadowsocks(c, paths)
	if err != nil {
		return ProxyPlan{Validation: result}, Rendered{}, err
	}

	plan := ProxyPlan{Validation: result}
	if !c.Shadowsocks.Enabled {
		// A disabled proxy renders no file at all, rather than a file that
		// configures nothing. There is no "valid and serves nobody" form of a
		// Shadowsocks config — a server with no password is not a server — so
		// the honest rendering of "off" is the absence of the file, and the
		// unit stopping.
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

	plan.Action = proxyAction(c.Shadowsocks.Enabled, obs.Running, len(plan.Changes) > 0)
	if obs.ServiceKnown && c.Shadowsocks.Enabled != obs.EnabledAtBoot {
		want := c.Shadowsocks.Enabled
		plan.Enable = &want
	}

	plan.Impact, plan.Reasons = classifyProxy(c, previous, plan, obs)
	return plan, rendered, nil
}

// proxyAction decides what to do with the unit after writing files.
//
// There is no reload rung. shadowsocks-rust re-reads its configuration at
// start and has no signal that makes it do so again, so every change that
// reaches the file is a restart — which is why the impact ladder here has
// nothing between "nothing" and "connections drop".
func proxyAction(enabled, running, changed bool) ServiceAction {
	switch {
	case enabled && !running:
		return ActionStart
	case !enabled && running:
		return ActionStop
	case !enabled:
		return ActionNone
	case !changed:
		return ActionNone
	default:
		return ActionRestart
	}
}

// classifyProxy reduces the plan to one impact plus the reasons behind it.
//
// §5.3.3 insists `disruptive` be a fact rather than a guess. Here the fact is
// not about a connection — a Shadowsocks client reconnects on its own, and a
// restart costs it a few seconds. What it is about is **whether the link people
// already have still works**, which is decided by three values and nothing
// else: the secret, the cipher, and the address a client dials.
func classifyProxy(c, previous Config, plan ProxyPlan, obs ProxyObserved) (Impact, []string) {
	if plan.Empty() {
		return ImpactNone, nil
	}

	impact := ImpactNone
	for _, ch := range plan.Changes {
		impact = max(impact, ch.Impact)
	}

	var reasons []string
	before, after := previous.Shadowsocks, c.Shadowsocks

	// Only if there was something to invalidate. A box being set up for the
	// first time has handed nobody a link, and calling that disruptive would be
	// the crying wolf §5.3.3 warns about.
	issued := before.Password != "" && before.Enabled

	switch {
	case issued && before.Password != after.Password:
		impact = ImpactDisruptive
		reasons = append(reasons,
			"the password changes, so every link already handed out stops working and every "+
				"device has to be given the new one")
	case issued && before.Cipher.OrDefault() != after.Cipher.OrDefault():
		impact = ImpactDisruptive
		reasons = append(reasons,
			"the cipher changes, and a link carries the cipher — every device has to be given a new one")
	}

	if issued && previous.DialAddress(before.DialPort()) != c.DialAddress(after.DialPort()) {
		impact = ImpactDisruptive
		reasons = append(reasons,
			"the address devices dial changes, so every link already handed out points at the old one")
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

// Diff renders a change the way `olr diff` shows one, withholding the contents
// of a secret file.
//
// **Every file this object renders is secret**, so in practice this always
// withholds — which is the point rather than a limitation. The operator is told
// that the configuration changed, which is the fact they need; the password in
// it is not, and `olr remote show shadowsocks link` is the deliberate way to
// see that.
func (f FileChange) Diff() string {
	var b strings.Builder
	b.WriteString("--- " + f.Path + "\n")
	b.WriteString("+++ " + f.Path + " (" + string(f.Kind) + ", " + f.Impact.String() + ")\n")

	if f.Secret {
		switch f.Kind {
		case FileCreate:
			b.WriteString("@@ contents withheld: this file holds the password @@\n+ (created)\n")
		case FileDelete:
			b.WriteString("@@ contents withheld: this file holds the password @@\n- (removed)\n")
		default:
			b.WriteString("@@ contents withheld: this file holds the password @@\n~ (changed)\n")
		}
		return b.String()
	}

	for _, l := range core.LineDiff(f.Before, f.After) {
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
}
