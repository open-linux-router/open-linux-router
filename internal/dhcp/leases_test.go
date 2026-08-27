package dhcp

import (
	"testing"
	"time"
)

// A realistic lease file: IPv4 and IPv6 entries, absent fields written as "*",
// an infinite lease, and the server DUID line dnsmasq writes when DHCPv6 is in
// use.
const sampleLeases = `1767225600 aa:bb:cc:dd:ee:ff 192.168.1.100 laptop 01:aa:bb:cc:dd:ee:ff
1767225600 11:22:33:44:55:66 192.168.1.101 * *
0 aa:bb:cc:dd:ee:00 192.168.1.102 forever *
duid 00:01:00:01:2a:3b:4c:5d:00:11:22:33:44:55
1767225600 148012345 2001:db8::100 desktop 00:01:00:01:2a:3b:4c:5d:00:11:22:33:44:66
`

func TestParseLeases(t *testing.T) {
	leases, problems := ParseLeases([]byte(sampleLeases))
	if len(problems) != 0 {
		t.Fatalf("unexpected problems:\n    %s", problemStrings(problems))
	}
	if len(leases) != 4 {
		t.Fatalf("got %d leases, want 4 (the duid line is not a lease)", len(leases))
	}

	first := leases[0]
	if first.MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("MAC = %q", first.MAC)
	}
	if first.IP.String() != "192.168.1.100" {
		t.Errorf("IP = %s", first.IP)
	}
	if first.Hostname != "laptop" {
		t.Errorf("Hostname = %q", first.Hostname)
	}
	if first.ClientID != "01:aa:bb:cc:dd:ee:ff" {
		t.Errorf("ClientID = %q", first.ClientID)
	}
	if want := time.Unix(1767225600, 0).UTC(); !first.Expires.Equal(want) {
		t.Errorf("Expires = %v, want %v", first.Expires, want)
	}

	// "*" means the client supplied nothing, not a hostname of "*".
	if leases[1].Hostname != "" || leases[1].ClientID != "" {
		t.Errorf("`*` fields were not treated as absent: %+v", leases[1])
	}

	// An expiry of 0 is dnsmasq's infinite lease.
	if !leases[2].Expires.IsZero() {
		t.Errorf("expiry 0 should mean never, got %v", leases[2].Expires)
	}
	if !leases[2].Active(time.Now().AddDate(10, 0, 0)) {
		t.Error("an infinite lease should still be active in ten years")
	}

	// The second field is an IAID for IPv6, where a hardware address is
	// frequently not available at all.
	v6 := leases[3]
	if v6.IAID != "148012345" {
		t.Errorf("IAID = %q", v6.IAID)
	}
	if v6.MAC != "" {
		t.Errorf("IPv6 lease should have no MAC, got %q", v6.MAC)
	}
	if v6.IP.String() != "2001:db8::100" {
		t.Errorf("IP = %s", v6.IP)
	}
}

// A lease file is written by a running daemon and can be read mid-write. One
// bad line must not cost us the rest of the file — but it must be reported,
// because a silently short count is worse than a visibly imperfect one.
func TestParseLeasesReportsBadLinesWithoutLosingGoodOnes(t *testing.T) {
	input := `1767225600 aa:bb:cc:dd:ee:ff 192.168.1.100 laptop *
truncated line
1767225600 zz:zz:zz:zz:zz:zz 192.168.1.101 bad-mac *
banana aa:bb:cc:dd:ee:11 192.168.1.102 bad-expiry *
1767225600 aa:bb:cc:dd:ee:22 999.999.999.999 bad-ip *
1767225600 aa:bb:cc:dd:ee:33 192.168.1.103 good *
`
	leases, problems := ParseLeases([]byte(input))

	if len(leases) != 2 {
		t.Errorf("got %d good leases, want 2", len(leases))
	}
	if len(problems) != 4 {
		t.Errorf("got %d problems, want 4:\n    %s", len(problems), problemStrings(problems))
	}
	for _, want := range []struct{ path, msg string }{
		{"line 2", "expected at least 3 fields"},
		{"line 3", "not a MAC address"},
		{"line 4", "not a timestamp"},
		{"line 5", "not an IP address"},
	} {
		if !hasProblem(problems, want.path, want.msg) {
			t.Errorf("no problem at %s containing %q:\n    %s", want.path, want.msg, problemStrings(problems))
		}
	}
}

func TestParseLeasesHandlesEmptyInput(t *testing.T) {
	for _, in := range []string{"", "\n", "   \n\n  \n"} {
		leases, problems := ParseLeases([]byte(in))
		if len(leases) != 0 || len(problems) != 0 {
			t.Errorf("ParseLeases(%q) = %d leases, %d problems", in, len(leases), len(problems))
		}
	}
}

func TestLoadMissingLeaseFileIsNotAnError(t *testing.T) {
	leases, problems, err := LoadLeases(t.TempDir() + "/absent")
	if err != nil {
		t.Fatalf("LoadLeases on a missing file: %v", err)
	}
	if len(leases) != 0 || len(problems) != 0 {
		t.Errorf("expected nothing, got %d leases and %d problems", len(leases), len(problems))
	}
}

func TestRangeSize(t *testing.T) {
	tests := []struct {
		start, end string
		want       int
	}{
		{"192.168.1.100", "192.168.1.200", 101},
		{"192.168.1.1", "192.168.1.1", 1},
		{"10.0.0.0", "10.0.1.0", 257},
		// Backwards ranges are a validation error, not a negative count.
		{"192.168.1.200", "192.168.1.100", 0},
	}
	for _, tc := range tests {
		if got := RangeSize(addr(t, tc.start), addr(t, tc.end)); got != tc.want {
			t.Errorf("RangeSize(%s, %s) = %d, want %d", tc.start, tc.end, got, tc.want)
		}
	}
}

func TestUsageOf(t *testing.T) {
	now := time.Unix(1767225000, 0).UTC()
	pool := lanPool(t) // 192.168.1.100 - .200, 101 addresses

	leases := []Lease{
		{IP: addr(t, "192.168.1.100"), Expires: now.Add(time.Hour)},  // active
		{IP: addr(t, "192.168.1.101"), Expires: now.Add(time.Hour)},  // active
		{IP: addr(t, "192.168.1.102"), Expires: now.Add(-time.Hour)}, // expired
		{IP: addr(t, "192.168.1.50")},                                // reservation, outside the range
		{IP: addr(t, "10.10.0.5"), Expires: now.Add(time.Hour)},      // another pool
	}

	u := UsageOf(pool, leases, now)
	if u.Interface != "br-lan" {
		t.Errorf("Interface = %q", u.Interface)
	}
	if u.Size != 101 {
		t.Errorf("Size = %d, want 101", u.Size)
	}
	if u.Active != 2 {
		t.Errorf("Active = %d, want 2", u.Active)
	}
	if u.Expired != 1 {
		t.Errorf("Expired = %d, want 1", u.Expired)
	}
	if u.Free() != 99 {
		t.Errorf("Free() = %d, want 99", u.Free())
	}
	if u.Percent() != 1 {
		t.Errorf("Percent() = %d, want 1", u.Percent())
	}
}

func TestUsagePercentHandlesEmptyPool(t *testing.T) {
	if got := (Usage{}).Percent(); got != 0 {
		t.Errorf("Percent() on a zero-size pool = %d, want 0", got)
	}
}
