package remote

import (
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
