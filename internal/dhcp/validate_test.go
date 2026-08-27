package dhcp

import (
	"net/netip"
	"testing"
	"time"
)

// The baseline must be clean, or every "this is rejected" case below could be
// passing for the wrong reason.
func TestValidateAcceptsAGoodConfig(t *testing.T) {
	r := Validate(validConfig(t), testLinks())
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
		links   StaticLinks // nil for testLinks()
		path    string
		message string
	}{{
		name:    "missing interface",
		mutate:  func(c *Config) { c.Pools[0].Interface = "" },
		path:    "pools[0].interface",
		message: "required",
	}, {
		name:    "unknown interface",
		mutate:  func(c *Config) { c.Pools[0].Interface = "br-nope" },
		path:    "pools[0].interface",
		message: "no such interface",
	}, {
		// design.md §3.4: adopt-only. Serving DHCP on an interface nobody
		// handed us is the surprise that rule exists to prevent.
		name:    "interface not adopted",
		mutate:  func(c *Config) { c.Pools[0].Interface = "eth-foreign" },
		path:    "pools[0].interface",
		message: "not adopted",
	}, {
		name:    "start above end",
		mutate:  func(c *Config) { c.Pools[0].Start, c.Pools[0].End = c.Pools[0].End, c.Pools[0].Start },
		path:    "pools[0]",
		message: "is above end",
	}, {
		name:    "start missing",
		mutate:  func(c *Config) { c.Pools[0].Start = netip.Addr{} },
		path:    "pools[0].start",
		message: "required",
	}, {
		// The cross-module check from design.md §5.3.1 — the highest-value
		// validation in the design, catching a half-finished renumbering
		// before anything is written.
		name:    "range outside the interface subnet",
		mutate:  func(c *Config) { c.Pools[0].Start, c.Pools[0].End = addr(t, "10.0.0.5"), addr(t, "10.0.0.9") },
		path:    "pools[0].start",
		message: "outside every subnet on br-lan",
	}, {
		name:    "range spans two subnets",
		mutate:  func(c *Config) { c.Pools[0].End = addr(t, "192.168.2.50") },
		path:    "pools[0].end",
		message: "cannot span subnets",
	}, {
		name:    "range contains the router itself",
		mutate:  func(c *Config) { c.Pools[0].Start = addr(t, "192.168.1.1") },
		path:    "pools[0]",
		message: "br-lan's own address",
	}, {
		name:    "range contains the network address",
		mutate:  func(c *Config) { c.Pools[0].Start = addr(t, "192.168.1.0") },
		path:    "pools[0]",
		message: "network address",
	}, {
		name:    "range contains the broadcast address",
		mutate:  func(c *Config) { c.Pools[0].End = addr(t, "192.168.1.255") },
		path:    "pools[0]",
		message: "broadcast address",
	}, {
		name:    "IPv6 range",
		mutate:  func(c *Config) { c.Pools[0].Start, c.Pools[0].End = addr(t, "2001:db8::1"), addr(t, "2001:db8::9") },
		path:    "pools[0]",
		message: "configure IPv6 with the ra field",
	}, {
		name: "two pools on one interface",
		mutate: func(c *Config) {
			second := lanPool(t)
			second.Start, second.End = addr(t, "192.168.1.210"), addr(t, "192.168.1.220")
			c.Pools = append(c.Pools, second)
		},
		path:    "pools[1].interface",
		message: "one pool per interface",
	}, {
		// Two pools can only overlap if their interfaces share a subnet, which
		// is a link-level misconfiguration rather than a dhcp one. The rule
		// exists precisely for that case: both pools are individually valid, so
		// nothing else would catch the collision.
		name: "overlapping ranges on interfaces that share a subnet",
		mutate: func(c *Config) {
			other := Pool{Interface: "br-guest", Start: addr(t, "192.168.1.150"), End: addr(t, "192.168.1.160")}
			c.Pools = append(c.Pools, other)
		},
		links: StaticLinks{
			"br-lan":   {Adopted: true, Up: true, Prefixes: []netip.Prefix{netip.MustParsePrefix("192.168.1.1/24")}},
			"br-guest": {Adopted: true, Up: true, Prefixes: []netip.Prefix{netip.MustParsePrefix("192.168.1.2/24")}},
		},
		path:    "pools[1]",
		message: "overlaps",
	}, {
		name:    "lease below the dnsmasq floor",
		mutate:  func(c *Config) { c.Pools[0].LeaseTime = Duration(30 * time.Second) },
		path:    "pools[0].lease_time",
		message: "two minute minimum",
	}, {
		name:    "unknown RA mode",
		mutate:  func(c *Config) { c.Pools[0].RA = "sometimes" },
		path:    "pools[0].ra",
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
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := validConfig(t)
			tc.mutate(&c)
			links := tc.links
			if links == nil {
				links = testLinks()
			}
			r := Validate(c, links)
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
		message: "inside br-lan's dynamic range",
	}, {
		name: "pool on a down interface",
		mutate: func(c *Config) {
			c.Pools = []Pool{{Interface: "br-down", Start: addr(t, "172.16.0.100"), End: addr(t, "172.16.0.200")}}
		},
		path:    "pools[0].interface",
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
			r := Validate(c, testLinks())
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

	r := Validate(c, testLinks())
	if !r.OK() {
		t.Fatalf("rejected:\n    %s", problemStrings(r.Errors))
	}
	if len(r.Warnings) != 0 {
		t.Errorf("warned about the recommended layout:\n    %s", problemStrings(r.Warnings))
	}
}

func TestValidateErrIsNilWhenOK(t *testing.T) {
	if err := Validate(validConfig(t), testLinks()).Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

func TestBroadcastAddress(t *testing.T) {
	tests := []struct{ prefix, want string }{
		{"192.168.1.1/24", "192.168.1.255"},
		{"10.0.0.1/8", "10.255.255.255"},
		{"172.16.5.1/30", "172.16.5.3"},
		{"192.168.1.1/32", "192.168.1.1"},
		{"0.0.0.0/0", "255.255.255.255"},
	}
	for _, tc := range tests {
		got, ok := broadcast(netip.MustParsePrefix(tc.prefix))
		if !ok {
			t.Errorf("broadcast(%s) reported no IPv4 broadcast", tc.prefix)
			continue
		}
		if got.String() != tc.want {
			t.Errorf("broadcast(%s) = %s, want %s", tc.prefix, got, tc.want)
		}
	}
	if _, ok := broadcast(netip.MustParsePrefix("2001:db8::/64")); ok {
		t.Error("broadcast() claimed an IPv6 prefix has a broadcast address")
	}
}
