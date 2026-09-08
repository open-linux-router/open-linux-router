package link

import (
	"net"
	"net/netip"
	"testing"
)

// toPrefix is where a wrong answer would be silent and expensive: a /24
// misread as a /120 puts every pool outside its own subnet, and the error the
// operator sees is about their range rather than about this.
func TestToPrefixHandlesTheKernelsRepresentations(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   *net.IPNet
		want string
	}{
		{
			name: "four-byte IPv4, as the kernel usually gives it",
			in:   &net.IPNet{IP: net.IP{192, 168, 1, 2}, Mask: net.CIDRMask(24, 32)},
			want: "192.168.1.2/24",
		},
		{
			// The case that motivates the function. A 16-byte v4-mapped address
			// unmaps to Is4 while its mask still reports 128 bits.
			name: "sixteen-byte IPv4, v4-mapped",
			in: &net.IPNet{
				IP:   net.ParseIP("192.168.1.2"),
				Mask: net.CIDRMask(96+24, 128),
			},
			want: "192.168.1.2/24",
		},
		{
			name: "IPv6",
			in:   &net.IPNet{IP: net.ParseIP("fd00:20::1"), Mask: net.CIDRMask(64, 128)},
			want: "fd00:20::1/64",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := toPrefix(tc.in)
			if !ok {
				t.Fatalf("toPrefix(%v) refused", tc.in)
			}
			if got.String() != tc.want {
				t.Errorf("toPrefix = %s, want %s", got, tc.want)
			}
		})
	}
}

// A non-contiguous mask has no prefix length that describes it, so there is
// nothing honest to publish.
func TestToPrefixRefusesANonContiguousMask(t *testing.T) {
	in := &net.IPNet{IP: net.IP{192, 168, 1, 2}, Mask: net.IPMask{255, 0, 255, 0}}
	if _, ok := toPrefix(in); ok {
		t.Error("toPrefix accepted a non-contiguous mask")
	}
}

// Link-local addresses sit on essentially every interface and are never the
// subnet an operator means. Including them would put an identical fe80::/64 on
// every row of the list, and hand routing a source prefix matching everything.
func TestPrefixesOfExcludesLinkLocal(t *testing.T) {
	addrs := []net.Addr{
		&net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)},
		&net.IPNet{IP: net.IP{169, 254, 3, 4}, Mask: net.CIDRMask(16, 32)},
		&net.IPNet{IP: net.IP{192, 168, 1, 2}, Mask: net.CIDRMask(24, 32)},
	}

	got := prefixesOf(addrs)
	if len(got) != 1 || got[0].String() != "192.168.1.2/24" {
		t.Errorf("prefixesOf = %v, want only 192.168.1.2/24", got)
	}
}

// IPv4 first: it is what an operator is looking for on a home network, and a
// stable order keeps the UI's rows from churning.
func TestPrefixesOfPutsIPv4First(t *testing.T) {
	addrs := []net.Addr{
		&net.IPNet{IP: net.ParseIP("fd00::1"), Mask: net.CIDRMask(64, 128)},
		&net.IPNet{IP: net.IP{192, 168, 1, 2}, Mask: net.CIDRMask(24, 32)},
	}

	got := prefixesOf(addrs)
	if len(got) != 2 || !got[0].Addr().Is4() {
		t.Errorf("prefixesOf = %v, want the IPv4 prefix first", got)
	}
}

// Loopback sorts last rather than being hidden: a list that silently omits an
// interface is one you cannot trust to be complete.
func TestSortInterfacesPutsLoopbackLast(t *testing.T) {
	in := []Interface{
		{Name: "lo", Loopback: true},
		{Name: "wan0"},
		{Name: "lan0"},
	}
	sortInterfaces(in)

	got := []string{in[0].Name, in[1].Name, in[2].Name}
	want := []string{"lan0", "wan0", "lo"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("order = %v, want %v", got, want)
			break
		}
	}
}

// Kernel is exercised for shape rather than content: what interfaces the
// machine running the test has is not something to assert on, but that reading
// them works and that every one has a name is.
func TestKernelReadsThisMachine(t *testing.T) {
	got, err := Kernel()
	if err != nil {
		t.Fatalf("Kernel: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("Kernel returned no interfaces; every machine has at least loopback")
	}
	for _, iface := range got {
		if iface.Name == "" {
			t.Errorf("interface at index %d has no name", iface.Index)
		}
		for _, p := range iface.Prefixes {
			if p == (netip.Prefix{}) {
				t.Errorf("%s has an invalid prefix", iface.Name)
			}
		}
	}
}
