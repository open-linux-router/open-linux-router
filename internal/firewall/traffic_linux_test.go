//go:build linux

package firewall

import (
	"net/netip"
	"testing"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
)

func TestSourcePartitionExpressions(t *testing.T) {
	conn := &nftables.Conn{}
	table := &nftables.Table{Name: TableName, Family: nftables.TableFamilyINet}
	for _, source := range []TrafficSource{SourceOutside, SourceInside, SourceLocal} {
		expressions, err := sourceExprs(conn, table, []string{"lan0", "wg0", "lo"}, source)
		if err != nil {
			t.Fatal(err)
		}
		meta, ok := expressions[0].(*expr.Meta)
		if !ok || meta.Key != expr.MetaKeyIIFNAME {
			t.Fatalf("source %s is not matched by incoming interface", source)
		}
		guard := expressions[1].(*expr.Cmp)
		if string(guard.Data[:2]) != "lo" || len(guard.Data) != 16 {
			t.Fatalf("bad loopback guard: %+v", guard)
		}
		if source == SourceLocal {
			if guard.Op != expr.CmpOpEq || len(expressions) != 2 {
				t.Fatal("local source must match loopback alone")
			}
		} else {
			lookup := expressions[2].(*expr.Lookup)
			if guard.Op != expr.CmpOpNeq || lookup.Invert != (source == SourceOutside) {
				t.Fatal("inside/outside must be complementary and exclude loopback")
			}
		}
	}
}

func TestEveryRuleCountsAfterConditionsBeforeVerdict(t *testing.T) {
	conn := &nftables.Conn{}
	table := &nftables.Table{Name: TableName, Family: nftables.TableFamilyINet}
	d := RenderIPTV(Config{Enabled: true}, []string{"lan0", "wg0"}, []Opening{web, wg,
		{For: "tunnel", Protocol: SixInFour, From: netip.MustParseAddr("203.0.113.1")}}, IPTV{Upstream: "iptv0", Downstream: []string{"lan0"}})
	for _, r := range append(d.Input, d.Forward...) {
		expressions, err := ruleExprs(conn, table, d, r)
		if err != nil {
			t.Fatalf("%s: %v", r.Line, err)
		}
		var counters int
		for i, e := range expressions {
			if _, ok := e.(*expr.Counter); ok {
				counters++
				if i != len(expressions)-2 {
					t.Fatalf("%s counted before its conditions", r.Line)
				}
			}
		}
		verdict, ok := expressions[len(expressions)-1].(*expr.Verdict)
		if !ok || counters != 1 {
			t.Fatalf("%s must count once then return a verdict", r.Line)
		}
		want := expr.VerdictAccept
		if r.Kind == RuleDrop {
			want = expr.VerdictDrop
			if _, ok := expressions[len(expressions)-3].(*expr.Objref); !ok {
				t.Fatal("drop lost its legacy aggregate counter")
			}
		}
		if verdict.Kind != want {
			t.Fatalf("%s changed its verdict", r.Line)
		}
	}
}
