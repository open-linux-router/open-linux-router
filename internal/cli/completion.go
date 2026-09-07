package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

// Shell completion for the things an operator actually types (docs/cli.md R7).
//
// The names of exits, pools, policies and reservations are chosen by the
// operator, so they are precisely what cannot be guessed and precisely what a
// completer is for. A tree that completes only its own subcommands — which is
// what registering cobra's `completion` command without any ValidArgsFunction
// produces — completes the half nobody needed help with.
//
// Suggestions are a read of stored config and so go through olrd like every
// other read (design.md §6.1). With the daemon stopped they degrade to no
// suggestions, which is the honest failure: `olr --help` is truthful without
// olrd, but *which exits exist* is a question only olrd can answer.

// CompletionFunc is cobra's ValidArgsFunction, named so signatures stay short.
type CompletionFunc func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)

// Fetcher returns the candidate names for one positional.
type Fetcher func(*cobra.Command) ([]string, error)

// CompleteArgs completes positionals left to right, one fetcher each.
//
// A nil fetcher leaves that position uncompleted, which is how `via <network>
// <exit>` completes its second argument from stored exits while its first is
// an interface name we do not yet own a list of.
func CompleteArgs(fetchers ...Fetcher) CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		i := len(args)
		if i >= len(fetchers) || fetchers[i] == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		names, err := fetchers[i](cmd)
		if err != nil {
			// olrd is down, or refused. A completer is not the place to
			// report that — the shell would paste the error onto the
			// operator's command line.
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return filterPrefix(names, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
}

// CompleteEach completes every positional from the same source, for the
// variadic commands — `rm block <name>...` should still be suggesting names on
// the fourth one.
func CompleteEach(fetch Fetcher) CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		names, err := fetch(cmd)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return filterPrefix(remaining(names, args), toComplete), cobra.ShellCompDirectiveNoFileComp
	}
}

// remaining drops what the operator has already typed on this line, so a
// repeated argument is not offered twice.
func remaining(names, used []string) []string {
	if len(used) == 0 {
		return names
	}
	seen := make(map[string]bool, len(used))
	for _, u := range used {
		seen[u] = true
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if !seen[n] {
			out = append(out, n)
		}
	}
	return out
}

// CompleteFlag completes a flag's value from stored names.
//
// Separate from CompleteArgs because a flag's completer is handed the
// positional arguments seen so far, which for CompleteArgs is the index it
// completes by — using it here would silently complete the wrong thing, or
// nothing, depending on where in the line the flag appeared.
func CompleteFlag(fetch Fetcher) CompletionFunc {
	return func(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		names, err := fetch(cmd)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return filterPrefix(names, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
}

// CompleteEnum completes a fixed vocabulary, for flags whose values are a
// closed set the operator has to remember otherwise.
func CompleteEnum(values ...string) CompletionFunc {
	return func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return filterPrefix(values, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
}

// EnumFlag registers a flag's value completion, ignoring the error cobra
// returns for a flag that does not exist — which is a programming mistake the
// conformance test catches, not something to handle at runtime.
func EnumFlag(cmd *cobra.Command, flag string, values ...string) {
	_ = cmd.RegisterFlagCompletionFunc(flag, CompleteEnum(values...))
}

func filterPrefix(names []string, prefix string) []string {
	if prefix == "" {
		return names
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if strings.HasPrefix(n, prefix) {
			out = append(out, n)
		}
	}
	return out
}
