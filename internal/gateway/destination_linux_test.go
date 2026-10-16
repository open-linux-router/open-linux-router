//go:build linux

package gateway

import (
	"bytes"
	"net/netip"
	"testing"

	"github.com/google/nftables/expr"
)

func TestDestinationExpressionsCheckAddressBeforeWritingMark(t *testing.T) {
	for _, tc := range []struct {
		source, destination string
		offset, length      uint32
	}{
		{"192.168.1.0/24", "1.2.3.4/32", 16, 4},
		{"fd00::/64", "2001:db8::/32", 24, 16},
	} {
		r := DestinationRule{SourceRule: SourceRule{Prefix: netip.MustParsePrefix(tc.source), Interface: "br-lan", Mark: 0x10000}, Destination: netip.MustParsePrefix(tc.destination)}
		es := destinationExprs(r)
		foundDest, foundInterface, foundCT, foundLocal := false, false, false, false
		for i, e := range es {
			switch v := e.(type) {
			case *expr.Payload:
				if v.Offset == tc.offset && v.Len == tc.length {
					cmp, ok := es[i+2].(*expr.Cmp)
					if !ok || !bytes.Equal(cmp.Data, r.Destination.Addr().AsSlice()) {
						t.Fatal("wrong destination match")
					}
					foundDest = true
				}
			case *expr.Ct:
				foundCT = true
			case *expr.Fib:
				foundLocal = true
			case *expr.Meta:
				if v.Key == expr.MetaKeyIIFNAME {
					foundInterface = true
				}
				if v.Key == expr.MetaKeyMARK && v.SourceRegister && !foundDest {
					t.Fatal("mark set before destination matches")
				}
			}
		}
		if !foundDest || !foundInterface || !foundCT || !foundLocal {
			t.Fatalf("missing routing guard: %+v", es)
		}
	}
}
