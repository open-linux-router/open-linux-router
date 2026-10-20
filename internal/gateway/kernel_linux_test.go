//go:build linux

package gateway

import (
	"testing"

	"github.com/google/nftables"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestHookNameComparesValuesNotPointers(t *testing.T) {
	// google/nftables hands its known hooks out as package-level pointers, and
	// a chain read back from netlink carries a pointer of its own to the same
	// number. Comparing pointer identity called a real forward chain "hook 2",
	// which made a correct table read as drift forever.
	for _, tc := range []struct {
		hook *nftables.ChainHook
		want string
	}{
		{nil, "none"},
		{nftables.ChainHookForward, "forward"},
		{nftables.ChainHookRef(unix.NF_INET_FORWARD), "forward"},
		{nftables.ChainHookRef(unix.NF_INET_LOCAL_IN), "input"},
		{nftables.ChainHookRef(unix.NF_INET_LOCAL_OUT), "output"},
		// A hook we do not write still has to read as something other than the
		// line we would write, so the plan shows it as drift and applies it.
		{nftables.ChainHookRef(unix.NF_INET_PRE_ROUTING), "hook 0"},
	} {
		if got := hookName(tc.hook); got != tc.want {
			t.Errorf("hookName(%v) = %q, want %q", tc.hook, got, tc.want)
		}
	}
}

func TestAnInstalledAccountingChainReadsBackAsTheLineWeCompare(t *testing.T) {
	// The read-back and the desired lines are the same comparison, and this is
	// the property that lets restoring clear the warning: a chain the kernel
	// holds exactly as asked has to render as the line applyStat and BuildPlan
	// diff against.
	for _, want := range StatChains() {
		// A copy of the value rather than the package pointer, which is what
		// decoding a chain out of netlink produces.
		hook := nftables.ChainHook(*hookNumber(want.Hook))
		chain := &nftables.Chain{
			Name:    want.Name,
			Hooknum: &hook,
			Type:    nftables.ChainTypeFilter,
		}
		if got := observedChainLine(chain); got != want.Line() {
			t.Errorf("chain %s read back as %q, want %q", want.Name, got, want.Line())
		}
	}
}

func TestObservedIPv6DefaultMetricDoesNotDrift(t *testing.T) {
	for _, routeType := range []int{unix.RTN_UNREACHABLE, unix.RTN_UNICAST} {
		want := observedRouteLine(netlink.Route{Table: 8101, Type: routeType}, true)
		got := observedRouteLine(netlink.Route{Table: 8101, Type: routeType, Priority: 1024}, true)
		if got != want {
			t.Errorf("IPv6 type %d: default metric changed line from %q to %q", routeType, want, got)
		}
	}
}

func TestObservedExplicitRouteMetricStillDiffers(t *testing.T) {
	base := netlink.Route{Table: 8101, Type: unix.RTN_UNREACHABLE}
	for _, tc := range []struct {
		name     string
		priority int
		v6       bool
	}{
		{"IPv6 fallback", 100, true},
		{"IPv6 nondefault", 1025, true},
		{"IPv4 metric", 1024, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actual := base
			actual.Priority = tc.priority
			if got, want := observedRouteLine(actual, tc.v6), observedRouteLine(base, tc.v6); got == want {
				t.Errorf("metric %d was lost: %q", tc.priority, got)
			}
		})
	}
}
