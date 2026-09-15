package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

// Clearing a blocker, rather than only reporting one.
//
// core.Blocker has always carried the command that resolves it, and
// distro.go's comment named the endpoint of that: "when olr grows a button that
// yields the port for the operator, this is the text it must display before
// acting". This file is that button's other half. olr knows what is missing, it
// knows the command, and it runs as root — handing the operator a paste buffer
// was the last mile of a job already done.
//
// What it deliberately does *not* become is an apply-time side effect. design.md
// §3.4's "no machine-wide service disabling" stands, with one exception now
// written into it: a shadow instance of a backend olr itself runs may be stopped
// and disabled *on request*. A click is the operator asking; doing it inside an
// apply would be olr deciding, and installing a package or disabling a unit are
// the two things here a mis-click cannot undo.

// Step is one unit of work in a fix, and how it went.
//
// The same shape internal/dhcp and internal/dns already report from an apply,
// and for the same §5.3.2 reason: there is no rollback, so a fix that got
// halfway says which halves landed rather than returning a bare error and
// leaving the operator to guess what the box looks like now.
type Step struct {
	Description string `json:"description"`
	Done        bool   `json:"done"`
	Error       string `json:"error,omitempty"`
}

// Action is a blocker olr can clear itself.
//
// The performing half is unexported on purpose. Everything a client can see is
// declarative — an opaque handle, a label and the list of commands — so no
// caller anywhere can name a package to install or a unit to disable and have
// olr do it. The only things olr will ever act on are the ones the two blocker
// builders in this package put here, which is also where the knowledge of what
// is safe to touch lives.
type Action struct {
	// ID is the stable handle a client names to ask for this one fix:
	// "install:unbound", "standdown:unbound.service".
	ID string `json:"id"`

	// Label is the button, and it says what will happen rather than "fix".
	Label string `json:"label"`

	// Runs is every step, up front, in the operator's words. It is display
	// text and it must match what do actually does — where the work goes over
	// D-Bus rather than through a shell, this is still the sentence the
	// operator would have typed, because that is what tells them what their box
	// will look like afterwards.
	Runs []string `json:"runs"`

	// do performs it. Never serialised; see the type comment.
	do func(context.Context) []Step
}

// ErrNoSuchFix is returned for an id no blocker offers, so a handler can turn
// it into a 404 rather than a 500.
var ErrNoSuchFix = errors.New("no such fix")

// fixing serialises fixes across the whole box.
//
// Not the global apply lock (§3.6), and that is a decision rather than an
// oversight. §3.6 holds that lock only for work that is bounded — "render,
// reload, return" — and an apt install is the opposite: on a box running
// unattended-upgrades it waits on the dpkg lock, for as long as that takes.
// Freezing every other module behind it is exactly what the bounded-apply rule
// exists to prevent. Nothing here writes configuration or a rendered file, so
// there is no intent to serialise against; this mutex only stops two package
// managers being started at once, which would otherwise collide on dpkg's own
// lock and fail the second one for a reason that reads like a bug.
var fixing sync.Mutex

// FixBlockers performs the actions the named blockers carry.
//
// An empty ids clears every blocker that has an action, in the order they were
// reported — which is the order they have to be worked through, dependencies
// before port conflicts.
//
// Steps are returned for what ran, and a step that failed does not stop the
// *next blocker* being attempted: two blockers are two independent problems and
// an operator who asked for both should not silently get one. Within a single
// action the opposite holds — its steps are a sequence, and standing a unit
// down after the install that was meant to bring it into existence failed would
// be acting on a box we no longer have a description of.
func FixBlockers(ctx context.Context, blockers []Blocker, ids []string) ([]Step, error) {
	actions, err := selectActions(blockers, ids)
	if err != nil {
		return nil, err
	}

	fixing.Lock()
	defer fixing.Unlock()

	var steps []Step
	for _, a := range actions {
		steps = append(steps, a.do(ctx)...)
	}
	return steps, nil
}

// Actionable reports the blockers olr can clear, which is what a caller needs
// to decide whether asking is worth a round trip at all.
func Actionable(blockers []Blocker) []Blocker {
	var out []Blocker
	for _, b := range blockers {
		if b.Action != nil {
			out = append(out, b)
		}
	}
	return out
}

// selectActions resolves ids against what is actually blocked right now.
//
// The blockers are re-derived by the caller immediately before this, rather
// than remembered from the status request the client read — so an id for a
// conflict somebody else has already cleared is refused instead of acted on.
func selectActions(blockers []Blocker, ids []string) ([]*Action, error) {
	if len(ids) == 0 {
		var out []*Action
		for _, b := range blockers {
			if b.Action != nil {
				out = append(out, b.Action)
			}
		}
		return out, nil
	}

	byID := make(map[string]*Action, len(blockers))
	for _, b := range blockers {
		if b.Action != nil {
			byID[b.Action.ID] = b.Action
		}
	}

	out := make([]*Action, 0, len(ids))
	for _, id := range ids {
		a, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("%w: %q is not something olr can clear on this box right now", ErrNoSuchFix, id)
		}
		out = append(out, a)
	}
	return out, nil
}

// task is one step of an action, before it has been run.
type task struct {
	description string
	run         func(context.Context) error
}

// runTasks performs tasks in order and stops at the first failure.
//
// Stopping rather than pressing on: every sequence here is a sequence for a
// reason — the package before the unit it creates, the drop-in before the
// restart that reads it — so a step after a failed one is either pointless or
// acting on a box that is not the one we planned against. What ran is still
// reported, which is the §5.3.2 half that matters.
func runTasks(ctx context.Context, tasks ...task) []Step {
	out := make([]Step, 0, len(tasks))
	for _, t := range tasks {
		step := Step{Description: t.description}
		if err := t.run(ctx); err != nil {
			step.Error = err.Error()
			return append(out, step)
		}
		step.Done = true
		out = append(out, step)
	}
	return out
}

// WriteStepsText prints what a fix attempted and how far it got.
//
// The same rendering internal/dhcp and internal/dns use for their own apply
// steps, here so that the two `fix` commands cannot drift into two formats for
// one kind of answer.
func WriteStepsText(w io.Writer, steps []Step) {
	if len(steps) == 0 {
		return
	}
	fmt.Fprintln(w, "steps attempted:")
	for _, s := range steps {
		mark := "ok  "
		if !s.Done {
			mark = "FAIL"
		}
		fmt.Fprintf(w, "  [%s] %s\n", mark, s.Description)
		if s.Error != "" {
			fmt.Fprintf(w, "%s\n", IndentLines(s.Error, "         "))
		}
	}
	if StepsFailed(steps) {
		fmt.Fprintln(w, "nothing was rolled back; deal with the cause and re-run to finish.")
	}
}

// StepsFailed reports whether any step did not complete, which is the one thing
// every surface has to branch on.
func StepsFailed(steps []Step) bool {
	for _, s := range steps {
		if !s.Done {
			return true
		}
	}
	return false
}
