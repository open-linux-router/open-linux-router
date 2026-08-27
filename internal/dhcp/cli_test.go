package dhcp

import (
	"slices"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/cli"
)

// The module must speak only the shared verb vocabulary (design.md §3.2 rule 4).
// This is what stops each module from quietly growing its own dialect.
func TestModuleUsesOnlySharedVerbs(t *testing.T) {
	shared := cli.Verbs()
	for _, sub := range Command().Commands() {
		if !slices.Contains(shared, sub.Name()) {
			t.Errorf("dhcp verb %q is outside the shared vocabulary %v", sub.Name(), shared)
		}
	}
}

func TestModuleIsGroupedAsAModule(t *testing.T) {
	if got := Command().GroupID; got != cli.GroupModules {
		t.Errorf("GroupID = %q, want %q", got, cli.GroupModules)
	}
}
