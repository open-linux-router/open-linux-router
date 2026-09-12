package core

import "testing"

// No build tag on purpose. These two are what the refusal messages render
// through, and the first version of this change put String() behind //go:build
// linux — so every message read "It is held by ." anywhere else, including in
// the tests meant to prove the messages were good.

func TestUnitOf(t *testing.T) {
	for _, tc := range []struct{ name, cgroup, want string }{
		{"v2", "0::/system.slice/dnsmasq.service\n", "dnsmasq.service"},
		{"v2 nested slice", "0::/system.slice/system-foo.slice/unbound.service\n", "unbound.service"},
		{
			"v1 multi-line",
			"11:devices:/system.slice/dnsmasq.service\n1:name=systemd:/system.slice/dnsmasq.service\n",
			"dnsmasq.service",
		},
		{
			// The leaf is what an operator can act on; user@1000.service is the
			// manager that started it, not the thing holding the port.
			"under a user manager",
			"0::/user.slice/user-1000.slice/user@1000.service/app.slice/thing.service\n",
			"thing.service",
		},
		{"a scope, not a service", "0::/user.slice/user-1000.slice/session-3.scope\n", ""},
		{"no cgroup at all", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := unitOf(tc.cgroup); got != tc.want {
				t.Errorf("unitOf() = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestHolderString(t *testing.T) {
	for _, tc := range []struct {
		name   string
		holder Holder
		want   string
	}{
		{"everything", Holder{PID: 1, Name: "unbound", Unit: "unbound.service"}, "unbound (pid 1, unbound.service)"},
		{"no unit", Holder{PID: 1, Name: "unbound"}, "unbound (pid 1)"},
		{"no name", Holder{PID: 1}, "pid 1"},
		{"nothing", Holder{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.holder.String(); got != tc.want {
				t.Errorf("String() = %q; want %q", got, tc.want)
			}
		})
	}
}
