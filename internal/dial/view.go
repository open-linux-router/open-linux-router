package dial

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The API's own shapes, for the reason internal/dhcp/view.go gives: what the
// HTTP API returns should be a deliberate decision rather than a side effect of
// which fields happened to be exported.

// recordView is one row of the status list.
//
// Flat rather than nested, and the flatness is load-bearing: docs/ddns.md §6
// says status answers three separate questions, and a nested `health` object
// with one verdict in it is exactly the collapse that makes the interesting
// failure — read fine, published days ago, provider refusing since — invisible.
type recordView struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`

	// Source and From say where the address comes from, in the two halves an
	// operator reads separately: which mechanism, and which interface or
	// endpoint.
	Source Source `json:"source"`
	From   string `json:"from,omitempty"`

	// The three facts (docs/ddns.md §6). Spelled out rather than embedding
	// RecordState, for one reason: `omitempty` does nothing for a time.Time, so
	// an embedded struct would publish `"published": "0001-01-01T00:00:00Z"` for
	// a record that has never been published — a timestamp in the year 1 that a
	// client has to know to special-case. A pointer is absent instead, which is
	// what "it has not happened" should look like on the wire.
	Checked    *time.Time `json:"checked,omitempty"`
	CheckError string     `json:"check_error,omitempty"`

	Address string `json:"address,omitempty"`
	CGNAT   bool   `json:"cgnat,omitempty"`

	Published        *time.Time `json:"published,omitempty"`
	PublishedAddress string     `json:"published_address,omitempty"`
	PublishError     string     `json:"publish_error,omitempty"`

	Failures int        `json:"failures,omitempty"`
	Retry    *time.Time `json:"retry,omitempty"`

	// Problems are this record's findings from the validator, so a row that is
	// misconfigured says so beside itself rather than only in a list at the
	// bottom.
	Problems []core.Problem `json:"problems,omitempty"`

	// Watched reports whether the publisher currently has a loop for this
	// record. False with no problems means olrd has not picked the config up
	// yet, which is a real transient state right after a write.
	Watched bool `json:"watched"`
}

// viewState projects the publisher's state onto the wire shape.
func viewState(v *recordView, s RecordState) {
	v.Checked = stamp(s.Checked)
	v.CheckError = s.CheckError
	v.Address = s.Address
	v.CGNAT = s.CGNAT
	v.Published = stamp(s.Published)
	v.PublishedAddress = s.PublishedAddress
	v.PublishError = s.PublishError
	v.Failures = s.Failures
	v.Retry = stamp(s.Retry)
}

// stamp turns "never happened" into an absent field rather than a zero time.
func stamp(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// uplinkView is the way out, intent beside fact.
//
// Both halves in one object and never collapsed into a verdict, for the reason
// recordView is flat: the interesting failure is the one where the intent looks
// right and the box disagrees, and a single "ok" boolean is exactly what hides
// it. An operator reading this wants to see `gateway 192.168.2.1` next to
// `route via 192.168.2.254 on enp3s0` and draw their own conclusion.
type uplinkView struct {
	// Interface, Address and Gateway are stored intent.
	Interface string   `json:"interface"`
	Address   string   `json:"address,omitempty"`
	Gateway   string   `json:"gateway,omitempty"`
	DNS       []string `json:"dns,omitempty"`

	// Present and Up are what the kernel says about the interface.
	Present bool `json:"present"`
	Up      bool `json:"up"`

	// Addresses are every IPv4 address actually on it. More than one means
	// something else is addressing this interface too; olr reports that and
	// removes nothing (PlanUplink).
	Addresses []string `json:"addresses,omitempty"`

	// RouteVia and RouteDev are the default route actually in the main table,
	// whichever interface it leaves by. Empty means the box has no way out at
	// all, which is a different sentence from "the route goes somewhere else"
	// and has to read as one.
	RouteVia string `json:"route_via,omitempty"`
	RouteDev string `json:"route_dev,omitempty"`

	// GatewayState is whether RouteVia answers on RouteDev — "answers",
	// "silent", or absent when nothing has tried yet — and GatewaySeenOn is
	// another interface where it does answer. Observed.GatewayState has why a
	// route that matches the config is not the same as one that works.
	GatewayState  string `json:"gateway_state,omitempty"`
	GatewaySeenOn string `json:"gateway_seen_on,omitempty"`

	// ResolvingThrough are the name servers this box itself looks names up
	// through, read from /etc/resolv.conf whoever wrote it — beside DNS, which
	// is what olr was told, for the same reason the route is beside the
	// gateway.
	ResolvingThrough []string `json:"resolving_through,omitempty"`

	// Problems are the uplink's findings from the validator, so a section that
	// is misconfigured says so beside itself.
	Problems []core.Problem `json:"problems,omitempty"`
}

// statusResponse is the whole module's observed state.
type statusResponse struct {
	// Uplink is absent when olr does not own the way out.
	Uplink *uplinkView `json:"uplink,omitempty"`

	Records []recordView `json:"records"`

	// AsOf stamps the reply: nothing here is stored, so it carries its
	// freshness (§4.5).
	AsOf time.Time `json:"as_of"`
}

// uplinkResponse is what GET /uplink answers.
//
// Its own shape rather than statusResponse with the records left out: a reply
// carrying an empty `records` array from a route that is not about records is
// the kind of thing a generated client then has to have an opinion about.
type uplinkResponse struct {
	// Uplink is absent when olr does not own the way out, which is a state and
	// not an error — most boxes are in it.
	Uplink *uplinkView `json:"uplink,omitempty"`

	AsOf time.Time `json:"as_of"`
}

// viewUplink joins stored intent with what the kernel has.
func viewUplink(u *Uplink, obs Observed, problems []core.Problem) *uplinkView {
	if u == nil {
		return nil
	}
	v := &uplinkView{
		Interface: u.Interface,
		Present:   obs.Present,
		Up:        obs.Up,
		RouteDev:  obs.GatewayDev,
		Problems:  problems,
	}
	if u.IPv4 != nil {
		if u.IPv4.Address.IsValid() {
			v.Address = u.IPv4.Address.String()
		}
		if u.IPv4.Gateway.IsValid() {
			v.Gateway = u.IPv4.Gateway.String()
		}
	}
	for _, a := range u.DNS {
		v.DNS = append(v.DNS, a.String())
	}
	for _, p := range obs.Addrs {
		v.Addresses = append(v.Addresses, p.String())
	}
	if obs.Gateway.IsValid() {
		v.RouteVia = obs.Gateway.String()
	}
	v.GatewayState, v.GatewaySeenOn = obs.GatewayState, obs.GatewaySeenOn
	return v
}

// From renders the source's parameter for a status row.
func recordFrom(r Record) string {
	switch r.Source {
	case SourceInterface:
		return r.Interface
	case SourceReflector:
		if r.Interface != "" {
			return r.ReflectorURL + " (via " + r.Interface + ")"
		}
		return r.ReflectorURL
	default:
		return ""
	}
}

// --- plan -------------------------------------------------------------------

// planView mirrors internal/link's plan shape on purpose.
//
// The field names are identical so that a client's Plan type and its plan
// preview render any module's answer without a second implementation.
//
// The impact stopped being a constant `none` when the uplink landed, exactly as
// internal/link's did when networks landed. Two kinds of change now share one
// plan and they could not be further apart: adding a DDNS record still does
// nothing to the box, while moving the uplink's gateway replaces the default
// route and may take the operator's own session with it.
type planView struct {
	Backend  string         `json:"backend"`
	Changes  []changeView   `json:"changes"`
	Action   string         `json:"action"`
	Impact   string         `json:"impact"`
	Empty    bool           `json:"empty"`
	Warnings []core.Problem `json:"warnings,omitempty"`
}

type changeView struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Impact string `json:"impact"`
	Diff   string `json:"diff"`
}

const (
	impactNone       = "none"
	impactRestart    = "restart"
	impactDisruptive = "disruptive"

	actionNone      = "none"
	actionConfigure = "configure"

	kindCreate = "create"
	kindUpdate = "update"
	kindDelete = "delete"
)

// impactRank orders the vocabulary so a plan can report the worst of its parts.
var impactRank = map[string]int{impactNone: 0, impactRestart: 1, impactDisruptive: 2}

// buildPlan diffs stored intent against a proposal, and the uplink against the
// kernel.
//
// Three sources for the uplink half and one for the records, which is the
// asymmetry the module has everywhere: the document diff says what the operator
// changed, and the kernel diff says what has to happen on the box for the
// result to be true. They are not the same list — re-applying an unchanged
// uplink still has work to do if somebody ran `ip route del default` by hand,
// and that is drift (§5.4) rather than a no-op.
func buildPlan(stored, desired Config, links LinkView, obs Observed) planView {
	stored.Normalize()
	desired.Normalize()

	res := Validate(desired, links)
	plan := planView{
		Backend:  "",
		Changes:  []changeView{},
		Action:   actionNone,
		Impact:   impactNone,
		Warnings: problems(res.Warnings),
	}

	planUplink(&plan, stored, desired, links, obs)

	for _, want := range desired.Records {
		have, existed := stored.Find(want.Name)
		switch {
		case !existed:
			plan.Changes = append(plan.Changes, changeView{
				Path:   itemPath(want.Name),
				Kind:   kindCreate,
				Impact: impactNone,
				Diff:   diffRecord(Record{}, want),
			})
			// The one consequence the machine's own state does not show, said
			// at the moment the operator is looking at the preview: from here
			// on this box talks to somebody it did not talk to before, on a
			// timer, whether or not anything else changes.
			plan.Warnings = append(plan.Warnings, core.Problem{
				Path: itemPath(want.Name),
				Message: fmt.Sprintf(
					"olr will tell %s this box's address every %s from now on%s",
					want.Provider, Duration(want.ResolvedInterval()), reflectorNote(want)),
			})
		case have != want:
			plan.Changes = append(plan.Changes, changeView{
				Path:   itemPath(want.Name),
				Kind:   kindUpdate,
				Impact: impactNone,
				Diff:   diffRecord(have, want),
			})
		}
	}

	for _, have := range stored.Records {
		if _, kept := desired.Find(have.Name); kept {
			continue
		}
		plan.Changes = append(plan.Changes, changeView{
			Path:   itemPath(have.Name),
			Kind:   kindDelete,
			Impact: impactNone,
			Diff:   diffRecord(have, Record{}),
		})
		// Removing a record stops olr updating the name. It does not remove the
		// name: the last address published stays at the provider, answering,
		// until somebody deletes it there. Saying so here is the difference
		// between an operator who knows the name is now frozen and one who
		// believes it is gone.
		plan.Warnings = append(plan.Warnings, core.Problem{
			Path: itemPath(have.Name),
			Message: fmt.Sprintf("%s stops being kept current, but it stays at %s "+
				"pointing at the address last published; delete it there if you want it gone",
				have.Name, have.Provider),
		})
	}

	for _, c := range plan.Changes {
		if impactRank[c.Impact] > impactRank[plan.Impact] {
			plan.Impact = c.Impact
		}
	}
	if plan.Impact != impactNone {
		plan.Action = actionConfigure
	}
	plan.Empty = len(plan.Changes) == 0
	return plan
}

// planUplink adds the document diff and the kernel diff for the way out.
func planUplink(plan *planView, stored, desired Config, links LinkView, obs Observed) {
	changed := !stored.Uplink.Equal(desired.Uplink)

	switch {
	case desired.Uplink == nil && stored.Uplink == nil:
	case desired.Uplink == nil:
		// Removing the uplink is impact none, which reads as odd next to
		// link's "removing a network is disruptive" and is the honest answer
		// here: nothing is torn down. Config.RemoveUplink has the argument for
		// why, and the warning below is what stops "removed" reading as "this
		// box is offline now".
		plan.Changes = append(plan.Changes, changeView{
			Path:   UplinkPath,
			Kind:   kindDelete,
			Impact: impactNone,
			Diff:   describeUplink("- ", stored.Uplink),
		})
		plan.Warnings = append(plan.Warnings, core.Problem{
			Path: UplinkPath,
			Message: fmt.Sprintf("olr stops owning the way out. %s keeps the address and the "+
				"default route it has right now, so nothing goes offline — but olr will not "+
				"put them back after a reboot, and nothing else on this box knows them. "+
				"Give the interface to your distribution's network configuration, or set "+
				"the uplink again", stored.Uplink.Interface),
		})
	case stored.Uplink == nil:
		plan.Changes = append(plan.Changes, changeView{
			Path:   UplinkPath,
			Kind:   kindCreate,
			Impact: uplinkImpact(nil, desired.Uplink, obs),
			Diff:   describeUplink("+ ", desired.Uplink),
		})
	case changed:
		plan.Changes = append(plan.Changes, changeView{
			Path:   UplinkPath,
			Kind:   kindUpdate,
			Impact: uplinkImpact(stored.Uplink, desired.Uplink, obs),
			Diff:   describeUplink("- ", stored.Uplink) + describeUplink("+ ", desired.Uplink),
		})
	}

	// The consequence the machine's own state does not show, said once, at the
	// moment the operator is looking at what they asked for rather than on
	// every status read forever — unroutedNetworksNote has the argument for
	// why it lives here and not in the validator.
	if changed && desired.Uplink != nil {
		if note := unroutedNetworksNote(desired.Uplink, links); note != "" {
			plan.Warnings = append(plan.Warnings, core.Problem{Path: UplinkPath, Message: note})
		}
		if note := takeoverNote(desired.Uplink); note != "" {
			plan.Warnings = append(plan.Warnings, core.Problem{Path: UplinkPath, Message: note})
		}
	}

	// The kernel half: what has to happen for any of the above to be true.
	// Computed against the *desired* config, so it is the drift check as well
	// as the change preview — planning the stored config against itself leaves
	// exactly the lines the box disagrees about.
	kernel := PlanUplink(DesiredFor(desired), obs)
	if lines := DescribeUplinkPlan(kernel); len(lines) > 0 {
		plan.Changes = append(plan.Changes, changeView{
			Path: "interfaces[" + kernel.Interface + "]",
			Kind: kindUpdate,
			// Taking over a default route that already points somewhere else is
			// the one move here that can drop traffic that was working. Writing a
			// default route onto a box that had none takes nothing away from
			// anybody, and neither does adding an address.
			Impact: pick(kernel.ReplacesGateway.IsValid(), impactDisruptive, impactRestart),
			Diff:   strings.Join(lines, "\n") + "\n",
		})
	}

	// The address the previous uplink had and this one does not. Its own
	// change, on the interface it is on, because that may not be the one the
	// uplink is moving to — and disruptive, because a session arriving at that
	// address goes with it. Retiring has when there is one.
	from, old := Retiring(stored, desired)
	if old.IsValid() && retiredStillThere(from, old, kernel.Interface, obs, links) {
		plan.Changes = append(plan.Changes, changeView{
			Path:   "interfaces[" + from + "]",
			Kind:   kindUpdate,
			Impact: impactDisruptive,
			Diff:   fmt.Sprintf("- ip addr del %s dev %s\n", old, from),
		})
	}

	// Outside that block on purpose: two managers on one interface is worth
	// saying on a box where nothing else needs doing, which is exactly the box
	// where it is hardest to notice.
	var foreign []string
	for _, p := range kernel.Foreign {
		if from == kernel.Interface && p == old {
			// On its way off, and the line above already says so.
			continue
		}
		foreign = append(foreign, p.String())
	}
	if len(foreign) > 0 {
		// Reported, never removed by an apply. Two things address this
		// interface and neither knows about the other, which is a state an
		// operator can only fix if they can see it — and the alternative,
		// deleting what the other one put there unasked, is the failure the
		// uplink object exists to stop. Removing one is a separate, explicit
		// request (link's DELETE /interfaces/{name}/addresses).
		plan.Warnings = append(plan.Warnings, core.Problem{
			Path: UplinkPath + ".interface",
			Message: fmt.Sprintf("%s also has %s on it, which the uplink does not account for "+
				"and an apply will not remove. If something else on this box is addressing "+
				"the interface, leave one of the two in charge; if it is left over, remove it",
				kernel.Interface, strings.Join(foreign, ", ")),
		})
	}
}

// retiredStillThere reports whether the previous uplink's address is still on
// its interface, so a plan does not promise to remove something already gone.
//
// Read from obs when it is the interface being planned against and from link's
// view otherwise. With no view at all it says yes: the writer checks again
// before removing anything, and a plan line for an address that turns out to
// be gone costs less than a removal nobody was shown.
func retiredStillThere(from string, old netip.Prefix, planned string, obs Observed, links LinkView) bool {
	if from == planned {
		return slices.Contains(obs.Addrs, old)
	}
	if links == nil {
		return true
	}
	info, err := links.Interface(from)
	if err != nil {
		return false
	}
	return slices.Contains(info.Prefixes, old)
}

// takeoverNote says what a static uplink takes from the distribution
// (internal/host), at the moment it is being set rather than afterwards.
//
// Said generally, because the plan does not read the host: which DHCP client
// the distribution runs, if any, is the apply's to find. What the operator
// needs before confirming is the consequence — anything that client had put on
// the interface goes — and where names will be looked up from.
func takeoverNote(u *Uplink) string {
	if !u.HasIPv4() {
		return ""
	}
	note := fmt.Sprintf("olr takes IPv4 on %s from the distribution: a DHCP client it runs there "+
		"stops asking for an address, and anything it gave %s goes. IPv6 there stays as it is",
		u.Interface, u.Interface)
	if len(u.DNS) > 0 {
		addrs := make([]string, 0, len(u.DNS))
		for _, a := range u.DNS {
			addrs = append(addrs, a.String())
		}
		note += ". This router will look names up through " + strings.Join(addrs, ", ")
	}
	return note
}

// uplinkImpact classifies a change to the way out, in terms of what stops
// working rather than of which fields moved.
//
// Disruptive whenever the default route in the main table is about to be
// replaced by a different one, and that is judged against the *kernel* rather
// than against the stored config. The distinction matters on the first apply:
// setting up an uplink on a box whose distribution already provides a default
// route is a takeover and can drop the operator's session, even though the
// stored config went from nothing to something.
func uplinkImpact(before, after *Uplink, obs Observed) string {
	if after == nil || !after.HasIPv4() {
		return impactNone
	}
	if obs.Gateway.IsValid() && obs.Gateway != after.IPv4.Gateway {
		return impactDisruptive
	}
	if before != nil && before.HasIPv4() && before.IPv4.Address != after.IPv4.Address {
		// The address moving takes every session arriving at the old one with
		// it, whether or not the route changes.
		return impactDisruptive
	}
	return impactRestart
}

// describeUplink renders the uplink as diff lines.
func describeUplink(sign string, u *Uplink) string {
	if u == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%suplink on %s\n", sign, u.Interface)
	if u.IPv4 != nil {
		fmt.Fprintf(&b, "%s  ipv4 %s via %s\n", sign, u.IPv4.Address, u.IPv4.Gateway)
	} else {
		fmt.Fprintf(&b, "%s  no static ipv4\n", sign)
	}
	if len(u.DNS) > 0 {
		addrs := make([]string, 0, len(u.DNS))
		for _, a := range u.DNS {
			addrs = append(addrs, a.String())
		}
		fmt.Fprintf(&b, "%s  dns %s\n", sign, strings.Join(addrs, ", "))
	}
	return b.String()
}

// pick is a conditional expression, for the places where a three-line if would
// only separate a value from the condition that chooses it.
func pick[T any](cond bool, yes, no T) T {
	if cond {
		return yes
	}
	return no
}

func reflectorNote(r Record) string {
	if r.Source != SourceReflector || r.ReflectorURL == "" {
		return ""
	}
	return ", and ask " + r.ReflectorURL + " what that address is"
}

func itemPath(name string) string { return "records[" + name + "]" }

// diffRecord renders one record's change, redacted.
//
// Both credential fields go through Redacted before they reach here, so the
// diff an operator prints — and pastes into a bug report — carries the shape of
// the change and none of the secret.
func diffRecord(before, after Record) string {
	var out string
	for _, f := range recordFields(before, after) {
		switch {
		case f.before == f.after:
			continue
		case f.before == "":
			out += fmt.Sprintf("+ %s %s\n", f.name, f.after)
		case f.after == "":
			out += fmt.Sprintf("- %s %s\n", f.name, f.before)
		default:
			out += fmt.Sprintf("- %s %s\n+ %s %s\n", f.name, f.before, f.name, f.after)
		}
	}
	return out
}

type fieldDiff struct{ name, before, after string }

func recordFields(before, after Record) []fieldDiff {
	b, a := redactRecord(before), redactRecord(after)
	return []fieldDiff{
		{"name", b.Name, a.Name},
		{"provider", b.Provider.String(), a.Provider.String()},
		{"zone", b.Zone, a.Zone},
		{"key-id", b.KeyID, a.KeyID},
		{"token", b.Token, a.Token},
		{"callback-url", b.CallbackURL, a.CallbackURL},
		{"source", string(b.Source), string(a.Source)},
		{"interface", b.Interface, a.Interface},
		{"reflector", b.ReflectorURL, a.ReflectorURL},
		{"interval", durationField(b.Interval), durationField(a.Interval)},
		{"ttl", intField(b.TTL), intField(a.TTL)},
	}
}

func redactRecord(r Record) Record {
	c := Config{Records: []Record{r}}.Redacted()
	return c.Records[0]
}

func durationField(d Duration) string {
	if d == 0 {
		return ""
	}
	return d.String()
}

func intField(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprint(n)
}
