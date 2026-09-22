package dial

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The module's REST surface (design.md §3.2 rule 2, §6.2).
//
// Deliberately thin: every decision here was made and tested in config.go,
// validate.go, view.go and apply.go. If a rule appears in this file that is not
// in one of those, it is in the wrong place — the CLI would not get it.

// HTTP serves the module.
type HTTP struct {
	// Applier owns the read and write paths onto the document.
	Applier Applier

	// Lock is core's one global apply lock (§3.6). Writes take it; reads never
	// do.
	Lock *core.Lock

	// Events is where an applied change is announced so the UI can re-read.
	Events *core.Events

	// Watch hands the stored config to the publisher after a write, and States
	// reads back what it knows. Both nil outside olrd — the conformance tests
	// call Routes on a zero value, and `status` then reports intent with no
	// observed half rather than refusing to answer.
	//
	// The publisher is reached through two function fields rather than held as
	// a dependency, exactly as internal/gateway reaches its prober: the
	// background loop belongs to the process, not to the module, and an Applier
	// that had to own one could not be constructed by a test.
	Watch  func(Config)
	States func() map[string]RecordState
}

// Routes is the module's surface, declared as data so it can be enumerated
// rather than only served. Core mounts these with the /api/dial prefix
// stripped.
func (h HTTP) Routes() []core.Route {
	return []core.Route{
		// Intent, whole section.
		{
			Method: "GET", Path: "/config", Tool: "show config",
			Summary: "Show the public names this router keeps pointing at itself, and where each one reads its address from. " +
				"Credentials are never returned.",
			Handler: h.getConfig,
		},
		{
			Method: "PUT", Path: "/config",
			Summary:  "Replace the whole set of dynamic DNS records.",
			Body:     core.BodyFull,
			Mutating: true,
			Handler:  h.putConfig,
		},
		{
			Method: "PATCH", Path: "/config",
			Summary:  "Change the dynamic DNS configuration, leaving unmentioned fields alone.",
			Body:     core.BodyRelaxed,
			Mutating: true,
			Handler:  h.patchConfig,
		},

		// Intent, the uplink on its own.
		//
		// Item routes beside /records/{name} and for the same reason: RFC 7386
		// replaces an array wholesale, and a PATCH meaning to edit the uplink
		// would take the record list with it. DELETE exists as its own verb
		// because "there is no uplink" is not something a merge patch can say
		// about a field it is also allowed to omit.
		{
			Method: "GET", Path: "/uplink", Tool: "show uplink",
			Summary: "Show how this router itself reaches the internet: the interface, its address " +
				"and the gateway olr was told to use, beside the address and default route the " +
				"kernel actually has.",
			Handler: h.getUplink,
		},
		{
			Method: "PUT", Path: "/uplink",
			Summary: "Set how this router itself reaches the internet. olr writes the address, brings " +
				"the interface up and owns the default route from then on.",
			Mutating: true,
			Handler:  h.putUplink,
		},
		{
			Method: "DELETE", Path: "/uplink",
			Summary: "Stop owning the way out. The address and the default route stay exactly as they " +
				"are; olr simply no longer maintains them or restores them after a reboot.",
			Mutating: true,
			Handler:  h.deleteUplink,
		},

		// Intent, one record at a time.
		//
		// These exist for the reason internal/ingress states about its services:
		// RFC 7386 replaces an array wholesale rather than merging into it, so a
		// PATCH meaning to edit one record would take the others with it. Without
		// them the edit happens in the client — load, splice, send back — which
		// is two requests where the lock only covers the second.
		{
			Method: "PUT", Path: "/records/{name}",
			Summary:  "Add one name to keep current, or replace it, leaving every other one alone.",
			Mutating: true,
			Handler:  h.putRecord,
		},
		{
			Method: "DELETE", Path: "/records/{name}",
			Summary: "Stop keeping one name current. The record is left at the provider pointing at " +
				"the address last published; this only stops olr updating it.",
			Mutating: true,
			Handler:  h.deleteRecord,
		},

		// Dry run. A POST because it takes a body, not because it changes
		// anything.
		{
			Method: "POST", Path: "/plan", Tool: "show plan",
			Summary: "Show what a dynamic DNS change would do without doing it. " +
				"An empty body plans the stored configuration.",
			Body:    core.BodyRelaxed,
			Handler: h.postPlan,
		},

		// Observed. Never stored, always stamped with as_of (§4.5).
		{
			Method: "GET", Path: "/status", Tool: "status",
			Summary: "Show, for each published name, when its address was last read, what it was, " +
				"and whether the last attempt to publish it succeeded.",
			Handler: h.getStatus,
		},
	}
}

// Handler returns the module's routes.
func (h HTTP) Handler() http.Handler { return core.RouteTable(h.Routes()) }

// --- intent -----------------------------------------------------------------

func (h HTTP) getConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Redacted on the way out, always. This is the response a UI renders into a
	// form and the CLI prints to a terminal, and no caller needs the real value
	// back — the only thing that reads it is the publisher, which loads the
	// config itself.
	core.WriteJSON(w, http.StatusOK, cfg.Redacted())
}

func (h HTTP) putConfig(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	next, err := UnmarshalConfig(data)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.mutate(w, r, func(cfg *Config) error {
		*cfg = preserveSecrets(next, *cfg)
		return nil
	})
}

func (h HTTP) patchConfig(w http.ResponseWriter, r *http.Request) {
	patch, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(bytes.TrimSpace(patch)) == 0 {
		core.WriteError(w, http.StatusBadRequest, "empty patch body")
		return
	}

	h.mutate(w, r, func(cfg *Config) error {
		currentJSON, err := MarshalConfig(*cfg)
		if err != nil {
			return err
		}
		merged, err := core.MergePatch(currentJSON, patch)
		if err != nil {
			return badRequest(err)
		}
		// Strict again on the merged result, so an unknown key in the patch is
		// caught rather than smuggled in by the merge.
		next, err := UnmarshalConfig(merged)
		if err != nil {
			return badRequest(err)
		}
		*cfg = preserveSecrets(next, *cfg)
		return nil
	})
}

func (h HTTP) putRecord(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var rec Record
	if err := core.DecodeJSON(w, r, &rec); err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The path wins over the body, and an omitted name means "the one in the
	// path" so the common case does not have to say it twice. Honouring the URL
	// is the reading that keeps PUT idempotent on the address it was sent to.
	rec.Name = name

	h.mutate(w, r, func(cfg *Config) error {
		if current, found := cfg.Find(name); found {
			rec = preserveRecordSecrets(rec, current)
		}
		cfg.SetRecord(rec)
		return nil
	})
}

// getUplink answers intent and fact in one reply.
//
// Both, because either alone is misleading: the stored gateway says what olr
// was told, and the route in the main table says what the box is doing, and the
// whole reason this object exists is that those two could silently be different
// things.
func (h HTTP) getUplink(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cfg.Uplink == nil {
		// 200 with the uplink absent, not 404. "olr does not own the way out"
		// is the answer for most boxes and a perfectly good state; a 404 would
		// make a UI render an error where it should render an empty section.
		core.WriteJSON(w, http.StatusOK, uplinkResponse{AsOf: time.Now()})
		return
	}
	obs := h.Applier.Observe(r.Context(), cfg.Uplink.Interface)
	core.WriteJSON(w, http.StatusOK, uplinkResponse{
		Uplink: viewUplink(cfg.Uplink, obs, uplinkProblems(cfg, h.Applier.Links)),
		AsOf:   time.Now(),
	})
}

func (h HTTP) putUplink(w http.ResponseWriter, r *http.Request) {
	var u Uplink
	if err := core.DecodeJSON(w, r, &u); err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.mutate(w, r, func(cfg *Config) error {
		cfg.SetUplink(u)
		return nil
	})
}

func (h HTTP) deleteUplink(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(cfg *Config) error {
		if !cfg.RemoveUplink() {
			// 404, not 422, matching deleteRecord: "olr does not own the way
			// out" is a statement about the address, and a UI retrying a delete
			// it already completed should be able to tell that from a refusal.
			return notFound(errors.New("olr does not own this box's uplink"))
		}
		return nil
	})
}

// uplinkProblems pulls the uplink's findings out of a whole-config validation,
// so a status row can carry its own complaints.
func uplinkProblems(cfg Config, links LinkView) []core.Problem {
	res := Validate(cfg, links)
	var out []core.Problem
	for _, p := range append(problems(res.Errors), problems(res.Warnings)...) {
		if findingOwner(cfg, p.Path) == UplinkPath {
			out = append(out, p)
		}
	}
	return out
}

func (h HTTP) deleteRecord(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	h.mutate(w, r, func(cfg *Config) error {
		if !cfg.RemoveRecord(name) {
			// 404, not 422. "There is no record by that name" is a statement
			// about the address, and a UI retrying a delete it already completed
			// should be able to tell that from a refusal.
			return notFound(fmt.Errorf("no record named %q", name))
		}
		return nil
	})
}

// preserveSecrets closes the round trip redaction creates.
//
// GET /config returns each credential as `********`. A UI that renders that into
// a form and sends the form back would otherwise *store the mask* — the
// publisher would authenticate with eight asterisks, every update would be
// refused from then on, and nothing about that is visible at the moment it
// happens. So the mask means "unchanged", which is the only thing it can
// honestly mean, and the real value is reachable by sending a different one.
//
// Matched by name, because that is the only identity a record has. A record
// being renamed and masked in one request therefore loses its credential, which
// is the correct reading: there is nothing to carry it over from.
func preserveSecrets(next, current Config) Config {
	for i := range next.Records {
		if have, found := current.Find(next.Records[i].Name); found {
			next.Records[i] = preserveRecordSecrets(next.Records[i], have)
		}
	}
	return next
}

func preserveRecordSecrets(next, current Record) Record {
	if next.Token == RedactedToken {
		next.Token = current.Token
	}
	if next.CallbackURL == RedactedToken {
		next.CallbackURL = current.CallbackURL
	}
	return next
}

// applyResponse carries the plan alongside the stored result.
//
// Steps are here now. Storing the document is still a single atomic write, but
// an uplink change continues into the kernel, and that part can land halfway —
// the address added and the route refused. §5.2 gives it no rollback, which
// only works if what did happen is reported. A record-only change leaves the
// field absent, which is honest: nothing reached the box.
type applyResponse struct {
	Plan   planView        `json:"plan"`
	Config Config          `json:"config"`
	Steps  []Step          `json:"steps,omitempty"`
	Error  *core.ErrorBody `json:"error,omitempty"`
}

// mutate is the one write path every mutating route goes through.
//
// Load, edit, validate, plan and apply, all inside the one global apply lock
// (§3.6). The lock has to cover every part or it covers nothing that matters:
// two clients adding a record at once would otherwise each splice their own copy
// of the list, and two programming the uplink would race on the route table.
//
// There is no disruptive gate here, matching `link` and unlike `ingress`. The
// server applies what it is given; it is the client that plans first and stops
// when the plan comes back disruptive, which is the instant-apply-unless-
// disruptive rule every module follows. What the server owes that client is an
// honest impact and a warning naming what is about to be lost, and
// withLockoutWarning is the sharpest of those — §5.5's guard is still not
// built, so the plan is the entire safety net.
func (h HTTP) mutate(w http.ResponseWriter, r *http.Request, edit func(*Config) error) {
	var (
		editErr  error
		invalid  Result
		failed   error
		result   ApplyResult
		stored   Config
		applyErr error
	)

	if lockErr := h.Lock.Do(r.Context(), func() error {
		current, err := h.Applier.Load()
		if err != nil {
			failed = err
			return nil
		}

		next := current.Clone()
		if err := edit(&next); err != nil {
			editErr = err
			return nil
		}
		next.Normalize()

		if res := Validate(next, h.Applier.Links); !res.OK() {
			invalid = res
			return nil
		}

		stored = next
		result, applyErr = h.Applier.Apply(r.Context(), next)
		if loaded, err := h.Applier.Load(); err == nil {
			stored = loaded
		}
		return nil
	}); lockErr != nil {
		core.WriteError(w, http.StatusServiceUnavailable,
			"timed out waiting for the apply lock: "+lockErr.Error())
		return
	}

	switch {
	case failed != nil:
		core.WriteError(w, http.StatusInternalServerError, failed.Error())
		return
	case editErr != nil:
		core.WriteError(w, statusFor(editErr), editErr.Error())
		return
	case !invalid.OK():
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid dial configuration", problems(invalid.Errors)...)
		return
	}

	plan := withLockoutWarning(result.Plan, r, stored)
	if applyErr != nil {
		core.WriteJSON(w, http.StatusInternalServerError, applyResponse{
			Plan:   plan,
			Config: stored.Redacted(),
			Steps:  result.Steps,
			Error:  &core.ErrorBody{Message: applyErr.Error()},
		})
		return
	}

	// Only when something actually changed, and in this order: the publisher is
	// told before the event is published, so a UI that re-reads on the event
	// sees a status the publisher has already been handed rather than one it is
	// about to be.
	if !plan.Empty {
		if h.Watch != nil {
			h.Watch(stored)
		}
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}

	core.WriteJSON(w, http.StatusOK, applyResponse{
		Plan: plan, Config: stored.Redacted(), Steps: result.Steps,
	})
}

// withLockoutWarning adds a warning when the change moves the address, or the
// route, the caller is talking to us over.
//
// This is §5.5's scenario, and §5.5's guard — the dead-man's switch that
// reverts a change nobody confirms — **is not built**. Until it is, this
// warning and the confirmation the UI stops at are the only things between an
// operator and a box they can no longer reach, so it names the address rather
// than saying something general about disruption.
//
// The uplink has a second way to lock somebody out that internal/link's version
// of this function does not have to consider: an operator reaching this box
// from *outside* the house arrives over the default route, and replacing it
// drops them even though no address changed. There is no reliable way to tell
// that case apart from a LAN client here — the request's local address is the
// box's own either way — so the route warning is attached whenever the route is
// being taken over, and it says what it depends on.
//
// It warns rather than refuses. Re-routing the box you are connected through is
// a legitimate thing to do deliberately; the plan is where olr says what it is
// about to cost, not where it overrules them.
func withLockoutWarning(plan planView, r *http.Request, cfg Config) planView {
	if cfg.Uplink == nil || !cfg.Uplink.HasIPv4() {
		return plan
	}
	for _, change := range plan.Changes {
		if change.Impact != impactDisruptive {
			continue
		}
		if local, ok := localAddr(r); ok && local == cfg.Uplink.IPv4.Address.Addr() {
			plan.Warnings = append(plan.Warnings, core.Problem{
				Path: UplinkPath,
				Message: fmt.Sprintf("you are connected to this router at %s, which is the uplink's "+
					"own address — applying this changes it and will drop your session. Have "+
					"console access ready", local),
			})
			return plan
		}
		plan.Warnings = append(plan.Warnings, core.Problem{
			Path: UplinkPath,
			Message: "this replaces the default route this box is using right now. If you are " +
				"reaching this router from outside your network, your session goes with it — " +
				"have a way in from the LAN, or console access, ready.",
		})
		return plan
	}
	return plan
}

// localAddr is the address on this box that the request arrived at.
//
// net/http puts the listener's local address in the connection context, which
// for TCP is the address the client actually reached us on. A unix socket has
// none — that is the `olr` CLI talking over /run, which cannot lock itself out
// of anything. Copied from internal/link/http.go, which had the problem first;
// it is six lines of net/http trivia rather than a decision, and sharing it
// would mean putting request plumbing in `core` for two callers.
func localAddr(r *http.Request) (netip.Addr, bool) {
	local, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	tcp, ok := local.(*net.TCPAddr)
	if !ok {
		return netip.Addr{}, false
	}
	addr, ok := netip.AddrFromSlice(tcp.IP)
	if !ok {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

// --- dry run ----------------------------------------------------------------

// postPlan answers "what would this do?" without doing it. An empty body plans
// the stored intent, which is the §5.4 drift check — empty on a box with no
// uplink, because the document is then the only thing this module owns, and on
// one with an uplink exactly the lines the kernel disagrees about.
func (h HTTP) postPlan(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	current, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	desired := current
	if len(bytes.TrimSpace(data)) > 0 {
		desired, err = UnmarshalConfig(data)
		if err != nil {
			core.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		// The same round trip preserveSecrets closes, and it matters here too:
		// planning with the mask would report a credential change that a PUT of
		// the same body would not make.
		desired = preserveSecrets(desired, current)
	}

	if res := Validate(desired, h.Applier.Links); !res.OK() {
		core.WriteError(w, http.StatusUnprocessableEntity,
			"invalid dial configuration", problems(res.Errors)...)
		return
	}

	obs := h.Applier.Observe(r.Context(), desired.uplinkInterface())
	plan := buildPlan(current, desired, h.Applier.Links, obs)
	core.WriteJSON(w, http.StatusOK, withLockoutWarning(plan, r, desired))
}

// --- observed ---------------------------------------------------------------

// getStatus is the three-questions surface (docs/ddns.md §6).
//
// It joins stored intent with what the publisher knows, per record. A record
// with no state has not been checked yet, which is a real transient right after
// a write and is reported as such rather than as a failure — the two look
// nothing alike to an operator and would otherwise be one blank column.
func (h HTTP) getStatus(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	states := map[string]RecordState{}
	if h.States != nil {
		states = h.States()
	}
	res := Validate(cfg, h.Applier.Links)
	byPath := map[string][]core.Problem{}
	for _, p := range append(problems(res.Errors), problems(res.Warnings)...) {
		byPath[findingOwner(cfg, p.Path)] = append(byPath[findingOwner(cfg, p.Path)], p)
	}

	resp := statusResponse{
		Uplink:  viewUplink(cfg.Uplink, h.Applier.Observe(r.Context(), cfg.uplinkInterface()), byPath[UplinkPath]),
		Records: make([]recordView, 0, len(cfg.Records)),
		AsOf:    time.Now(),
	}
	for _, rec := range cfg.Records {
		state, watched := states[rec.Name]
		view := recordView{
			Name:     rec.Name,
			Provider: rec.Provider.String(),
			Source:   rec.Source,
			From:     recordFrom(rec),
			Problems: byPath[rec.Name],
			Watched:  watched,
		}
		viewState(&view, state)
		resp.Records = append(resp.Records, view)
	}
	core.WriteJSON(w, http.StatusOK, resp)
}

// findingOwner maps a validation path back to the thing it is about, so a
// finding can be shown beside its own row: `records[3].interface` to the
// record's name, and anything under `uplink` to UplinkPath.
//
// A record's name can never collide with UplinkPath, because validateName
// refuses a single label — which is worth knowing rather than worth guarding,
// since the guard would be a second place the two vocabularies have to agree.
func findingOwner(c Config, path string) string {
	if path == UplinkPath || strings.HasPrefix(path, UplinkPath+".") {
		return UplinkPath
	}
	return recordNameFor(c, path)
}

// recordNameFor maps a validation path — `records[3].interface` — back to the
// record it is about.
func recordNameFor(c Config, path string) string {
	var index int
	if _, err := fmt.Sscanf(path, "records[%d]", &index); err != nil {
		return ""
	}
	if index < 0 || index >= len(c.Records) {
		return ""
	}
	return c.Records[index].Name
}

// --- helpers ----------------------------------------------------------------

// editError carries the status an edit failure should be reported with, so that
// "no such record" is a 404 and a malformed patch is a 400 without mutate having
// to know which edits can produce which.
type editError struct {
	status int
	err    error
}

func (e editError) Error() string { return e.err.Error() }
func (e editError) Unwrap() error { return e.err }

func notFound(err error) error   { return editError{status: http.StatusNotFound, err: err} }
func badRequest(err error) error { return editError{status: http.StatusBadRequest, err: err} }

func statusFor(err error) int {
	var e editError
	if errors.As(err, &e) {
		return e.status
	}
	return http.StatusUnprocessableEntity
}
