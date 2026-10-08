//go:build linux

package gateway

import (
	"testing"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

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
