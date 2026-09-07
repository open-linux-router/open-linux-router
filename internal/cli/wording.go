package cli

import (
	"fmt"
	"io"
	"strings"
)

// The four phrasings every module shares (docs/cli.md R8).
//
// These are here rather than in each module because four modules inventing
// four ways to say "there is no such thing" is exactly the drift this package
// already prevents for verbs — and unlike verb drift, wording drift is invisible
// until an operator reads two error messages side by side and cannot tell
// whether they mean the same thing.

// NothingToDo is what an apply prints when the plan is empty.
const NothingToDo = "Nothing to do; the configuration is already applied."

// candidateLimit is how many existing names an error will list.
//
// Above this the list stops being a hint and becomes the haystack again; the
// operator is better served by `show` than by a paragraph of names.
const candidateLimit = 8

// UnknownObject is the one phrasing for "you named something that is not there".
//
// It lists what does exist, because the reason an operator arrives here is
// almost always a typo rather than a genuine absence — and a bare "no such
// exit" answers only the half of the question they already knew.
//
// addCmd is the command that would create one, used only when nothing exists
// yet. Pass "" when there is no single obvious way to create the object.
func UnknownObject(kind, name, addCmd string, have []string) error {
	switch {
	case len(have) == 0 && addCmd != "":
		return fmt.Errorf("no %s named %q; none are configured yet, add one with `%s`",
			kind, name, addCmd)
	case len(have) == 0:
		return fmt.Errorf("no %s named %q; none are configured yet", kind, name)
	case len(have) <= candidateLimit:
		return fmt.Errorf("no %s named %q (have %s)", kind, name, strings.Join(have, ", "))
	default:
		return fmt.Errorf("no %s named %q", kind, name)
	}
}

// NoObjects writes the empty-list line for **stored configuration**.
//
// hint is one sentence saying either what to do next or what the empty state
// means, and may be empty. It is worth supplying wherever there is an obvious
// one: an empty list is the most common thing a new operator sees, and it is
// the cheapest place to teach the command that fills it.
func NoObjects(w io.Writer, plural, hint string) error {
	return writeEmpty(w, fmt.Sprintf("No %s configured.", plural), hint)
}

// NoneObserved is NoObjects for **observed** resources — leases, queries,
// resolved names (design.md §6.2).
//
// A separate phrasing because "configured" would be a lie about all of them,
// and the difference is the one an operator most needs here: an empty
// reservation list means nobody has written one, an empty lease list means
// nobody has asked for one. Same shape on screen, opposite thing to do about it.
func NoneObserved(w io.Writer, plural, hint string) error {
	return writeEmpty(w, fmt.Sprintf("No %s observed yet.", plural), hint)
}

func writeEmpty(w io.Writer, line, hint string) error {
	if hint != "" {
		line += " " + hint
	}
	_, err := fmt.Fprintln(w, line)
	return err
}
