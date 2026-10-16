//go:build linux

package gateway

import (
	"net"

	"github.com/google/nftables/expr"
)

func destinationExprs(r DestinationRule) []expr.Any {
	addr := r.Destination.Addr().AsSlice()
	length := uint32(len(addr))
	offset := uint32(16)
	if r.Destination.Addr().Is6() {
		offset = 24
	}
	// sourceExprs supplies family, mark, DNAT and local-address guards.
	// Extra matches run before the classifier writes its mark.
	return sourceExprs(r.SourceRule,
		&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: append([]byte(r.Interface), 0)},
		&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: offset, Len: length},
		&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: length, Mask: net.CIDRMask(r.Destination.Bits(), len(addr)*8), Xor: make([]byte, length)},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: addr})
}
