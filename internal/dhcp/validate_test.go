package dhcp

import (
	"net/netip"
	"testing"
	"time"
)

// The baseline must be clean, or every "this is rejected" case below could be
// passing for the wrong reason.
func TestValidateAcceptsAGoodConfig(t *testing.T) {
	r := Validate(validConfig(t), testGroups())
	if !r.OK() {
		t.Fatalf("baseline config rejected:\n    %s", problemStrings(r.Errors))
	}
	if len(r.Warnings) != 0 {
		t.Errorf("baseline config warned:\n    %s", problemStrings(r.Warnings))
	}
}

// One case per rule, each mutating exactly one thing away from the baseline, so
// a failure names the rule that broke.
func TestValidateRejects(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		groups  StaticGroups // nil for testGroups()
		path    string
		message string
	}{{
		name:    "missing network",
		mutate:  func(c *Config) { c.Pools[0].Group = "" },
		path:    "pools[0].group",
		message: "required",
	}, {
		name:    "unknown network",
		mutate:  func(c *Config) { c.Pools[0].Group = "nope" },
		path:    "pools[0].group",
		message: "no such network",
	}, {
		// The adopt-only check that used to live here is gone, and its absence
		// is the point: being in a network means having been adopted, and
		// `link` enforces that where the network is stored. A rule checked in
		// two modules is a rule that can be enforced in one of them and not the
		// other.
		name: "start above end",
		mutate: func(c *Config) {
			c.Pools[0].IPv4.Start, c.Pools[0].IPv4.End = c.Pools[0].IPv4.End, c.Pools[0].IPv4.Start
		},
		path:    "pools[0].ipv4",
		message: "is above end",
	}, {
		name:    "start missing",
		mutate:  func(c *Config) { c.Pools[0].IPv4.Start = netip.Addr{} },
		path:    "pools[0].ipv4.start",
		message: "required when an end is given",
	}, {
		// The rule that started all of this. It used to read "outside every
		// subnet on br-lan" — true, unactionable, and checked against an
		// address configured outside olr. It is now a contradiction between two
		// things olr stores, and the message names the network.
		name:    "range outside the network's subnet",
		mutate:  func(c *Config) { c.Pools[0].IPv4.Start, c.Pools[0].IPv4.End = addr(t, "10.0.0.5"), addr(t, "10.0.0.9") },
		path:    "pools[0].ipv4.start",
		message: `outside 192.168.1.0/24, the subnet of network "lan"`,
	}, {
		name:    "end outside the network's subnet",
		mutate:  func(c *Config) { c.Pools[0].IPv4.End = addr(t, "192.168.2.50") },
		path:    "pools[0].ipv4.end",
		message: `outside 192.168.1.0/24`,
	}, {
		name:    "range contains the router itself",
		mutate:  func(c *Config) { c.Pools[0].IPv4.Start = addr(t, "192.168.1.1") },
		path:    "pools[0].ipv4",
		message: "this router's own address",
	}, {
		name:    "range contains the network address",
		mutate:  func(c *Config) { c.Pools[0].IPv4.Start = addr(t, "192.168.1.0") },
		path:    "pools[0].ipv4",
		message: "network address",
	}, {
		name:    "range contains the broadcast address",
		mutate:  func(c *Config) { c.Pools[0].IPv4.End = addr(t, "192.168.1.255") },
		path:    "pools[0].ipv4",
		message: "broadcast address",
	}, {
		// An IPv6 literal in the v4 range is almost always somebody looking for
		// the ipv6 block, so the message points at it.
		name: "IPv6 addresses in the IPv4 range",
		mutate: func(c *Config) {
			c.Pools[0].IPv4.Start, c.Pools[0].IPv4.End = addr(t, "2001:db8::1"), addr(t, "2001:db8::9")
		},
		path:    "pools[0].ipv4",
		message: "configure IPv6 under ipv6.mode",
	}, {
		name: "two pools on one network",
		mutate: func(c *Config) {
			second := lanPool(t)
			second.IPv4.Start, second.IPv4.End = addr(t, "192.168.1.210"), addr(t, "192.168.1.220")
			c.Pools = append(c.Pools, second)
		},
		path:    "pools[1].group",
		message: "one pool per network",
	}, {
		// Both pools are individually valid, so nothing else would catch the
		// collision. `link` refuses overlapping subnets, but a fixture can
		// still state them and a hand-edited document can still contain them.
		name: "overlapping ranges on networks that share a subnet",
		mutate: func(c *Config) {
			c.Pools = append(c.Pools, Pool{
				Group: "guest",
				IPv4:  &PoolIPv4{Start: addr(t, "192.168.1.150"), End: addr(t, "192.168.1.160")},
			})
		},
		groups: StaticGroups{
			"lan": {
				Members: []string{"br-lan"}, Up: true,
				Subnet: netip.MustParsePrefix("192.168.1.0/24"),
				Router: netip.MustParseAddr("192.168.1.1"),
			},
			"guest": {
				Members: []string{"br-guest"}, Up: true,
				Subnet: netip.MustParsePrefix("192.168.1.0/24"),
				Router: netip.MustParseAddr("192.168.1.2"),
			},
		},
		path:    "pools[1]",
		message: "overlaps",
	}, {
		// Not expressible before the split, and now the one shape that really
		// is broken: a pool that serves neither family does nothing at all.
		name:    "serves neither family",
		mutate:  func(c *Config) { c.Pools[0].IPv4 = nil },
		path:    "pools[0]",
		message: "serves neither IPv4 nor IPv6",
	}, {
		// A v4 range on a network with no subnet has nothing to sit in, and the
		// message says where to fix it rather than only that it is wrong.
		name:    "IPv4 pool on a network with no subnet",
		mutate:  func(c *Config) { c.Pools[0].Group = "v6only" },
		path:    "pools[0].ipv4",
		message: "olr net set v6only --subnet",
	}, {
		name:    "lease below the dnsmasq floor",
		mutate:  func(c *Config) { c.Pools[0].LeaseTime = Duration(30 * time.Second) },
		path:    "pools[0].lease_time",
		message: "two minute minimum",
	}, {
		name:    "unknown IPv6 mode",
		mutate:  func(c *Config) { c.Pools[0].IPv6 = &PoolIPv6{Mode: "sometimes"} },
		path:    "pools[0].ipv6.mode",
		message: "unknown mode",
	}, {
		name: "gateway outside the subnet",
		mutate: func(c *Config) {
			gw := addr(t, "10.0.0.1")
			c.Pools[0].Gateway = &gw
		},
		path:    "pools[0].gateway",
		message: "could not reach it",
	}, {
		name: "option that has a dedicated field",
		mutate: func(c *Config) {
			c.Pools[0].Options = []Option{{Option: "option:router", Value: "192.168.1.9"}}
		},
		path:    "pools[0].options[0].option",
		message: "set that instead",
	}, {
		name: "option by number that has a dedicated field",
		mutate: func(c *Config) {
			c.Pools[0].Options = []Option{{Option: "6", Value: "1.1.1.1"}}
		},
		path:    "pools[0].options[0].option",
		message: "set that instead",
	}, {
		name: "option with no value",
		mutate: func(c *Config) {
			c.Pools[0].Options = []Option{{Option: "252", Value: ""}}
		},
		path:    "pools[0].options[0].value",
		message: "required",
	}, {
		name: "bad reservation MAC",
		mutate: func(c *Config) {
			c.Reservations = []Reservation{{MAC: "nope", IP: addr(t, "192.168.1.50")}}
		},
		path:    "reservations[0].mac",
		message: "invalid MAC",
	}, {
		name: "duplicate reservation MAC",
		mutate: func(c *Config) {
			c.Reservations = []Reservation{
				{MAC: "aa:bb:cc:dd:ee:ff", IP: addr(t, "192.168.1.50")},
				{MAC: "AA:BB:CC:DD:EE:FF", IP: addr(t, "192.168.1.51")},
			}
		},
		path:    "reservations[1].mac",
		message: "already reserved",
	}, {
		name: "duplicate reservation IP",
		mutate: func(c *Config) {
			c.Reservations = []Reservation{
				{MAC: "aa:bb:cc:dd:ee:ff", IP: addr(t, "192.168.1.50")},
				{MAC: "aa:bb:cc:dd:ee:11", IP: addr(t, "192.168.1.50")},
			}
		},
		path:    "reservations[1].ip",
		message: "already reserved",
	}, {
		// dnsmasq's own rule: a dhcp-host address must share a subnet with some
		// dhcp-range or it is never offered.
		name: "reservation in no pool's subnet",
		mutate: func(c *Config) {
			c.Reservations = []Reservation{{MAC: "aa:bb:cc:dd:ee:ff", IP: addr(t, "10.55.0.5")}}
		},
		path:    "reservations[0].ip",
		message: "not in the subnet of any configured pool",
	}, {
		name: "reservation hostname with an illegal character",
		mutate: func(c *Config) {
			c.Reservations = []Reservation{{MAC: "aa:bb:cc:dd:ee:ff", IP: addr(t, "192.168.1.50"), Hostname: "my_nas"}}
		},
		path:    "reservations[0].hostname",
		message: "only letters, digits and hyphens",
	}, {
		// The escape hatch is additive. Letting it set a directive we render
		// would let the daemon's file contradict the config that produced it,
		// defeating the single-source rule the hatch exists to preserve.
		name:    "escape hatch overrides an owned directive",
		mutate:  func(c *Config) { c.ExtraConf = "dhcp-range=192.168.1.5,192.168.1.9" },
		path:    "extra_dnsmasq_conf line 1",
		message: "is set by the dhcp module",
	}, {
		name:    "escape hatch re-enables DNS",
		mutate:  func(c *Config) { c.ExtraConf = "# a comment\nport=53" },
		path:    "extra_dnsmasq_conf line 2",
		message: "belongs to the dns module",
	}, {
		// Rendered unconditionally now, so setting it here would emit the
		// directive twice.
		name:    "escape hatch sets dhcp-authoritative",
		mutate:  func(c *Config) { c.ExtraConf = "dhcp-authoritative" },
		path:    "extra_dnsmasq_conf line 1",
		message: "authoritative by construction",
	}, {
		// The lease ceiling is derived from the pools; a hand-set one would
		// silently cap a pool that olr reports as having free addresses, which
		// is the exact bug rendering it was meant to fix.
		name:    "escape hatch sets dhcp-lease-max",
		mutate:  func(c *Config) { c.ExtraConf = "dhcp-lease-max=50" },
		path:    "extra_dnsmasq_conf line 1",
		message: "sized from the configured pools",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := validConfig(t)
			tc.mutate(&c)
			groups := tc.groups
			if groups == nil {
				groups = testGroups()
			}
			r := Validate(c, groups)
			if r.OK() {
				t.Fatalf("config was accepted, expected %s to be rejected", tc.path)
			}
			if !hasProblem(r.Errors, tc.path, tc.message) {
				t.Errorf("no error at %q containing %q; got:\n    %s", tc.path, tc.message, problemStrings(r.Errors))
			}
		})
	}
}

// Warnings exist so that hazards which the operator may legitimately want are
// named rather than forbidden — refusing them would be us overruling someone on
// their own network.
func TestValidateWarns(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		path    string
		message string
	}{{
		name: "reservation inside the dynamic range",
		mutate: func(c *Config) {
			c.Reservations = []Reservation{{MAC: "aa:bb:cc:dd:ee:ff", IP: addr(t, "192.168.1.150")}}
		},
		path:    "reservations[0].ip",
		message: "inside lan's dynamic range",
	}, {
		name: "pool on a down network",
		mutate: func(c *Config) {
			c.Pools = []Pool{{
				Group: "down",
				IPv4:  &PoolIPv4{Start: addr(t, "172.16.0.100"), End: addr(t, "172.16.0.200")},
			}}
		},
		path:    "pools[0].group",
		message: "is down",
	}, {
		name:    "enabled with no pools",
		mutate:  func(c *Config) { c.Pools = nil },
		path:    "pools",
		message: "nothing will be served",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := validConfig(t)
			tc.mutate(&c)
			r := Validate(c, testGroups())
			if !r.OK() {
				t.Fatalf("expected a warning, got errors:\n    %s", problemStrings(r.Errors))
			}
			if !hasProblem(r.Warnings, tc.path, tc.message) {
				t.Errorf("no warning at %q containing %q; got:\n    %s", tc.path, tc.message, problemStrings(r.Warnings))
			}
		})
	}
}

// dnsmasq permits a reservation outside the dynamic range, and it is the safer
// habit, so it must not warn.
func TestReservationOutsideRangeIsClean(t *testing.T) {
	c := validConfig(t)
	c.Reservations = []Reservation{{MAC: "aa:bb:cc:dd:ee:ff", IP: addr(t, "192.168.1.50"), Hostname: "nas"}}

	r := Validate(c, testGroups())
	if !r.OK() {
		t.Fatalf("rejected:\n    %s", problemStrings(r.Errors))
	}
	if len(r.Warnings) != 0 {
		t.Errorf("warned about the recommended layout:\n    %s", problemStrings(r.Warnings))
	}
}

func TestValidateErrIsNilWhenOK(t *testing.T) {
	if err := Validate(validConfig(t), testGroups()).Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

// The broadcast arithmetic moved to internal/core, where `link` uses it too;
// see internal/core/subnet_test.go. Keeping a copy here would be a second
// implementation of the same eight lines, which is exactly what the move was
// for.
