package dnsrelay

import (
	"context"
	"net/netip"
	"sync/atomic"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func TestFilterAAAAIndependentOfTransportAndUpstream(t *testing.T) {
	var calls atomic.Int32
	u := newFakeUpstream(t, func(query []byte) []byte {
		calls.Add(1)
		answer, err := Answer(query, []netip.Addr{netip.MustParseAddr("192.0.2.10"), netip.MustParseAddr("2001:db8::10")}, 60)
		if err != nil {
			t.Error(err)
		}
		return answer
	})
	client := netip.MustParseAddr("192.0.2.20")
	for _, tcp := range []bool{false, true} {
		for _, filtered := range []bool{false, true} {
			r, err := New(Config{Upstream: u.addr(), FilterAAAA: filtered}, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, typ := range []dnsmessage.Type{dnsmessage.TypeA, dnsmessage.TypeAAAA} {
				before := calls.Load()
				query := buildQuery(t, 42, "example.com.", typ)
				res, err := r.resolve(context.Background(), client, query, tcp, arrival{})
				if err != nil {
					t.Fatal(err)
				}
				obs, err := Observe(res.response)
				if err != nil {
					t.Fatal(err)
				}
				if filtered && typ == dnsmessage.TypeAAAA {
					if calls.Load() != before || obs.Rcode != "NOERROR" || len(obs.Addrs) != 0 {
						t.Fatalf("filtered AAAA: %+v; upstream calls %d", obs, calls.Load()-before)
					}
				} else if calls.Load() != before+1 || len(obs.Addrs) != 1 {
					t.Fatalf("normal lookup: %+v", obs)
				}
			}
		}
	}
}
