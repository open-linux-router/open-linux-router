package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

// verbs is the shared verb vocabulary (design.md §3.2 rule 4). Modules use
// these words and no others, so that knowing one module teaches you the rest.
var verbs = map[string]string{
	"show":    "Show current configuration",
	"set":     "Change configuration fields",
	"add":     "Add an item to a list-valued field",
	"rm":      "Remove an item",
	"status":  "Show runtime status",
	"logs":    "Show recent log output",
	"enable":  "Enable the module",
	"disable": "Disable the module",
}

// Verbs returns the vocabulary, sorted. Useful for docs and tests.
func Verbs() []string {
	out := make([]string, 0, len(verbs))
	for v := range verbs {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// Verb returns a subcommand for one of the shared verbs. Pass an empty short
// to accept the generic description.
//
// It panics if name is outside the vocabulary. A module inventing its own verb
// is the kind of drift that is invisible in review and obvious at startup, so
// we make it fail at startup.
func Verb(name, short string) *cobra.Command {
	generic, ok := verbs[name]
	if !ok {
		panic(fmt.Sprintf("cli: %q is not in the shared verb vocabulary %v", name, Verbs()))
	}
	if short == "" {
		short = generic
	}
	return &cobra.Command{
		Use:   name,
		Short: short,
		RunE:  NotImplemented,
	}
}

// NewModule builds a module's top-level command from its verbs.
func NewModule(name, short string, verbs ...*cobra.Command) *cobra.Command {
	c := &cobra.Command{
		Use:     name,
		Short:   short,
		GroupID: GroupModules,
	}
	c.AddCommand(verbs...)
	return c
}

// NotImplemented is the RunE for commands that exist in the tree but have no
// behaviour yet. Stubs report themselves rather than silently succeeding.
func NotImplemented(cmd *cobra.Command, _ []string) error {
	return fmt.Errorf("%s: not implemented yet", cmd.CommandPath())
}
