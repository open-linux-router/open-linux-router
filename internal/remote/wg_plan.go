package remote

import (
	"fmt"
	"sort"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Planning is pure and reads *observed* state rather than a cached copy of what
// we last wrote, which is what makes drift free (design.md §5.4): "have we
// drifted?" is "plan unchanged intent against the system and see if the diff is
// empty".
//
// The unit of comparison is the kernel, not a rendered file — the same as
// internal/gateway and for the same reason. There is no file: `wg setconf` is
// handed a configuration over a pipe and the kernel is the only place it lands,
// so somebody who ran `wg set wg0 peer … remove` by hand shows up here exactly
// as a hand-edited config file does in the file-rendering modules.

// Impact says what applying a change will cost, so a UI can warn instead of
// spinning (design.md §5.3.3).
//
// The same four words every other module uses, and the rung that means
// something different here is the last one: what this module can take away is
// not a connection but a *file somebody already has on their phone*. A change
// that invalidates a client configuration is disruptive even though nothing is
// dropped at the instant it applies, because no retry anywhere will fix it.
type Impact int

const (
	// ImpactNone changes nothing the kernel holds.
	ImpactNone Impact = iota

	// ImpactReload is picked up without disturbing a live tunnel. `wg setconf`
	// keeps the session state of every peer whose key is unchanged, so adding a
	// peer — the ordinary case — costs nobody anything.
	ImpactReload

	// ImpactRestart drops live tunnels and lets them come back by themselves. A
	// WireGuard client retries forever, so moving the listen port or this box's
	// address is an interruption rather than a loss.
	ImpactRestart

	// ImpactDisruptive takes access away, or invalidates a configuration that
	// has already left the box.
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

// UnmarshalText parses that string back. The pair has to exist together, or a
// plan can be sent over the API and never decoded by a Go client of it — and
// `olr remote` is a client of its own module's API.
func (i *Impact) UnmarshalText(text []byte) error {
	switch string(text) {
	case "none":
		*i = ImpactNone
	case "reload":
		*i = ImpactReload
	case "restart":
		*i = ImpactRestart
	case "disruptive":
		*i = ImpactDisruptive
	default:
		return fmt.Errorf("unknown impact %q (want none, reload, restart or disruptive)", text)
	}
	return nil
}

// ChangeKind is what happens to a line of kernel state.
type ChangeKind string

// The operations a plan can contain.
const (
	ChangeAdd    ChangeKind = "add"
	ChangeRemove ChangeKind = "remove"
)

// Change is one line's worth of pending work, in the canonical form.
type Change struct {
	Kind ChangeKind `json:"kind"`
	Line string     `json:"line"`
}

// Plan is the full answer to "what would applying this config do?".
type Plan struct {
	Changes []Change `json:"changes"`
	Impact  Impact   `json:"impact"`

	// Reasons explains the impact in the operator's terms — above all, which
	// device loses its way in, and which client configurations stop being
	// valid.
	Reasons []string `json:"reasons,omitempty"`

	// Blocked is set when the plan cannot proceed at all: the interface name is
	// taken by something that is not ours. Carried on the plan rather than only
	// on status, because the operator needs to see it next to what they were
	// trying to do.
	Blocked string `json:"blocked,omitempty"`

	// Known reports whether the kernel could be read at all.
	//
	// Carried on the plan rather than only on status, because without it a
	// client cannot tell "there is nothing to do" from "we could not look" —
	// and an empty plan means opposite things in those two cases. internal/
	// gateway publishes the same field for the same reason.
	Known bool `json:"known"`

	// Validation carries warnings even on success.
	Validation Result `json:"-"`
}

// Empty reports whether applying would change nothing — the drift check
// (design.md §5.4).
func (p Plan) Empty() bool { return len(p.Changes) == 0 }

// Diff renders the change as a unified-style diff, using core's differ — the
// one `olr diff` is built on for every module.
//
// The header is ours rather than core's for the reason core/diff.go gives: only
// the module knows what a change to its own state costs, so the impact
// annotation is not core's to write.
func (p Plan) Diff(before, after []string) string {
	var b strings.Builder
	b.WriteString("--- kernel tunnel state\n")
	b.WriteString("+++ kernel tunnel state (" + p.Impact.String() + ")\n")
	for _, l := range core.LineDiff(joinLines(before), joinLines(after)) {
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
}

func joinLines(lines []string) []byte {
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

// BuildPlan renders the desired state and diffs it against what the kernel has.
//
// It validates first and returns the error rather than planning against a
// config that cannot be applied — the whole value of validation is that it
// happens before anything is written (design.md §5.3.1).
//
// `previous` is the config as it is stored right now, and it is here for one
// reason: **the kernel does not know a peer's name.** A device being removed is
// in the kernel and not in `c`, so without the config it is being removed
// *from*, the only thing a confirmation dialog could say is 44 characters of
// base64 — and "5vVv…= loses access" tells an operator nothing they can weigh.
// Pass `c` itself where there is no edit (drift, status).
func BuildPlan(c, previous Config, networks NetworkView, obs Observed) (Plan, Desired, error) {
	result := ValidateWireGuard(c, networks)
	validateEndpoint(&result, c)
	if err := result.Err(); err != nil {
		return Plan{Validation: result}, Desired{}, err
	}

	desired := Render(c)
	plan := Plan{Validation: result, Known: obs.Known}

	// The one refusal. An interface with our name that is not WireGuard belongs
	// to somebody else, and design.md §3.4's adopt-only rule says we do not
	// touch it — not even to find out what it is. Checked only when there is
	// something to install: tearing our own tunnel down never conflicts with
	// anybody, and refusing to would leave an operator unable to back out.
	if obs.Foreign && desired.Enabled {
		plan.Blocked = fmt.Sprintf(
			"%s already exists on this box and is not a WireGuard interface, so olr will not "+
				"configure it. Give the tunnel a different name with `olr remote set --interface <name>`",
			desired.Interface)
	}

	if !obs.Known {
		// Nothing to compare against. Reporting "everything must be created"
		// would be a plan nobody can act on, and reporting "no change" would be
		// a lie; an empty plan plus the unknown flag on the response is the
		// honest pair.
		return plan, desired, nil
	}

	plan.Changes = diffLines(obs.Lines, desired.Lines())
	plan.Impact, plan.Reasons = classify(c, previous, desired, plan, obs)
	return plan, desired, nil
}

// diffLines reduces two canonical states to the lines that differ.
//
// Set difference rather than an ordered diff, because these lines are kernel
// objects rather than a file: a peer is the same peer whether it is read back
// second or fifth, and treating a reordering as a change would make every plan
// on a busy tunnel look non-empty.
func diffLines(before, after []string) []Change {
	have := make(map[string]int, len(before))
	for _, l := range before {
		have[l]++
	}
	want := make(map[string]int, len(after))
	for _, l := range after {
		want[l]++
	}

	var out []Change
	for _, l := range after {
		if want[l] > 0 && have[l] == 0 {
			out = append(out, Change{Kind: ChangeAdd, Line: l})
			want[l]--
			continue
		}
		if have[l] > 0 {
			have[l]--
			want[l]--
		}
	}
	for _, l := range before {
		if have[l] > 0 {
			out = append(out, Change{Kind: ChangeRemove, Line: l})
			have[l]--
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// classify reduces the plan to one impact plus the reasons behind it.
//
// §5.3.3 insists `disruptive` be a fact rather than a guess, and the reason is
// pointed: a classification that cried wolf would train the operator to click
// through the one dialog that matters. So each rung below is tied to something
// the kernel actually holds — a peer it is accepting, an address it has — and
// never to a field having changed in the document.
func classify(c, previous Config, d Desired, plan Plan, obs Observed) (Impact, []string) {
	if len(plan.Changes) == 0 {
		return ImpactNone, nil
	}

	impact := ImpactReload
	var reasons []string

	// Turning the tunnel off, when there is a tunnel to turn off.
	if !d.Enabled && obs.Present {
		return ImpactDisruptive, []string{
			"remote access is turned off, so every device loses its way in until it is turned back on",
		}
	}

	names := peerNames(previous, c)
	gone, moved := peerChanges(obs, d)

	if len(gone) > 0 {
		impact = ImpactDisruptive
		reasons = append(reasons, describeRevoked(gone, names, obs))
	}
	if len(moved) > 0 {
		// The peer keeps its key and gets a different address: the file on the
		// device says the old one, and nothing on the device will notice.
		impact = ImpactDisruptive
		subject, theirs := "devices are", "them"
		if len(moved) == 1 {
			subject, theirs = "device is", "it"
		}
		reasons = append(reasons, fmt.Sprintf(
			"%d %s moving to a different address, so the configuration already on %s is wrong "+
				"and has to be replaced: %s",
			len(moved), subject, theirs, strings.Join(describeKeys(moved, names), ", ")))
	}

	if changedLine(plan.Changes, "public-key ") {
		impact = ImpactDisruptive
		reasons = append(reasons,
			"this box's key changes, and every client configuration names the old one — "+
				"every peer has to be issued a new file")
	}

	if changedLine(plan.Changes, "address ") && impact < ImpactRestart {
		impact = ImpactRestart
		reasons = append(reasons,
			"this box's address inside the tunnel moves, so open connections through it break "+
				"and re-establish by themselves")
	}

	if changedLine(plan.Changes, "listen-port ") {
		if impact < ImpactRestart {
			impact = ImpactRestart
		}
		// The port a client dials is in its configuration, so the question is
		// not whether the *listen* port moved — it is whether the address those
		// files name still works. An operator who moved the listen port and set
		// `public_port` to the old one has changed nothing a client can see.
		if previous.DialAddress(previous.WireGuard.DialPort()) !=
			c.DialAddress(c.WireGuard.DialPort()) && len(d.Peers) > 0 {
			impact = ImpactDisruptive
			reasons = append(reasons,
				"the address devices dial changes, so every configuration already handed out "+
					"points at the old one and has to be replaced")
		} else {
			reasons = append(reasons,
				"the listen port changes, so tunnels drop until each device reconnects")
		}
	}

	return impact, reasons
}

// peerChanges reports which observed peers are losing access, and which are
// keeping their key while their address moves.
//
// Both are read from the kernel rather than from the stored list. "Which peers
// exist in the config" is not the question — the question is what the box is
// accepting right now, because that is what an operator is about to lose.
func peerChanges(obs Observed, d Desired) (gone, moved []string) {
	want := make(map[string]string, len(d.Peers))
	for _, p := range d.Peers {
		want[p.PublicKey] = peerLine(p.PublicKey, p.AllowedIPs)
	}

	for _, line := range obs.Lines {
		key, ok := peerKeyOf(line)
		if !ok {
			continue
		}
		desired, kept := want[key]
		switch {
		case !kept:
			gone = append(gone, key)
		case desired != line:
			moved = append(moved, key)
		}
	}
	sort.Strings(gone)
	sort.Strings(moved)
	return gone, moved
}

// peerKeyOf extracts the public key from a canonical peer line.
func peerKeyOf(line string) (string, bool) {
	rest, ok := strings.CutPrefix(line, "peer ")
	if !ok {
		return "", false
	}
	key, _, _ := strings.Cut(rest, " ")
	return key, key != ""
}

// peerNames maps public keys back to the operator's words. The kernel knows
// only keys, and "5vVv…=" is not something to put in a confirmation dialog.
//
// Both configs are read, and the order matters: the stored one first, so that a
// device being *removed* is still nameable, then the desired one on top so a
// rename is reported by its new name rather than its old one.
func peerNames(configs ...Config) map[string]string {
	out := map[string]string{}
	for _, c := range configs {
		for _, p := range c.WireGuard.Peers {
			out[p.PublicKey] = p.Name
		}
	}
	return out
}

func describeKeys(keys []string, names map[string]string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if name, ok := names[k]; ok && name != "" {
			out = append(out, name)
			continue
		}
		// A key the kernel holds and the config does not name: added by hand,
		// or through the escape hatch. Truncated, because the full value is 44
		// characters of base64 and the first few are enough to find it in
		// `wg show`.
		out = append(out, shortKey(k))
	}
	return out
}

// describeRevoked says what is being taken away, and whether it was ever used.
//
// The second half matters: removing a peer that has connected is revoking
// somebody's access, while removing one that never handshaked is usually
// cleaning up a configuration that was never imported. Both are refused without
// confirmation — the operator asked for one of them — but only one of them
// deserves the word "loses".
func describeRevoked(keys []string, names map[string]string, obs Observed) string {
	used := map[string]bool{}
	for _, p := range obs.Peers {
		if !p.LastHandshake.IsZero() {
			used[p.PublicKey] = true
		}
	}
	connected := 0
	for _, k := range keys {
		if used[k] {
			connected++
		}
	}

	line := fmt.Sprintf("%s losing access and cannot dial in again without a new configuration: %s",
		plural(len(keys), "device is", "devices are"), strings.Join(describeKeys(keys, names), ", "))
	switch {
	case connected == 0 && len(keys) == 1:
		return line + " (it has never connected)"
	case connected == 0:
		return line + " (none of them has ever connected)"
	case connected == len(keys):
		return line
	default:
		return line + fmt.Sprintf(" (%d of them has connected before)", connected)
	}
}

func shortKey(k string) string {
	if len(k) <= 8 {
		return k
	}
	return k[:8] + "…"
}

// changedLine reports whether any change touches a line with this prefix.
func changedLine(changes []Change, prefix string) bool {
	for _, c := range changes {
		if strings.HasPrefix(c.Line, prefix) {
			return true
		}
	}
	return false
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
