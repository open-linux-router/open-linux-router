//go:build linux

package gateway

import (
	"testing"

	"github.com/google/nftables/expr"
)

func TestAccountingExprsAttributeTranslatedEndpoint(t *testing.T) {
	for _, spec := range StatSets() {
		for _, dnat := range []bool{false, true} {
			expressions := accountSetExprs(spec, dnat)
			var direction byte
			var offset, length uint32
			var status uint32
			for i, e := range expressions {
				switch x := e.(type) {
				case *expr.Ct:
					if x.Key == expr.CtKeyDIRECTION {
						direction = expressions[i+1].(*expr.Cmp).Data[0]
					}
				case *expr.Payload:
					offset, length = x.Offset, x.Len
				case *expr.Bitwise:
					if x.Len == 4 && string(x.Mask) == string(markBytes(ctStatusDstNAT)) {
						status = uint32(expressions[i+1].(*expr.Cmp).Data[0])
					}
				}
			}
			wantDirection := byte(ctDirOriginal)
			wantOffset := uint32(12)
			if spec.Down {
				wantDirection, wantOffset = ctDirReply, 16
			}
			if dnat {
				if spec.Down {
					wantDirection, wantOffset = ctDirOriginal, 16
				} else {
					wantDirection, wantOffset = ctDirReply, 12
				}
			}
			wantLength := uint32(4)
			if spec.V6 {
				wantLength = 16
				if wantOffset == 12 {
					wantOffset = 8
				} else {
					wantOffset = 24
				}
			}
			wantStatus := uint32(0)
			if dnat {
				wantStatus = ctStatusDstNAT
			}
			if direction != wantDirection || offset != wantOffset || length != wantLength || status != wantStatus {
				t.Errorf("%s dnat=%v: direction=%d offset=%d length=%d status=%#x, want %d %d %d %#x", spec.Name, dnat, direction, offset, length, status, wantDirection, wantOffset, wantLength, wantStatus)
			}
		}
	}
}
