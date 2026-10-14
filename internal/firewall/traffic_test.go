package firewall

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func TestTrafficSourcesPreserveRuleCoverage(t *testing.T) {
	d := RenderIPTV(Config{Enabled: true}, []string{"lan0", "wg0"}, []Opening{web}, IPTV{Upstream: "iptv0", Downstream: []string{"lan0"}})
	for _, rules := range [][]Rule{d.Input, d.Forward} {
		groups := map[string][]TrafficSource{}
		for _, r := range rules {
			category, description := semanticCategory(r)
			if category == "Unknown" || description == "" {
				t.Fatalf("unclassified rule: %+v", r)
			}
			key := category
			if r.Kind == RuleIPTVIGMP {
				key += r.Downstream
			}
			groups[key] = append(groups[key], r.Source)
		}
		for category, sources := range groups {
			want := []TrafficSource{SourceOutside, SourceInside, SourceLocal}
			switch category {
			case "Inside-initiated access", "IPTV · IGMPlan0":
				want = []TrafficSource{SourceInside}
			case "Router-local traffic":
				want = []TrafficSource{SourceLocal}
			case "IPTV · IGMPiptv0", "IPTV · IPv4 multicast":
				want = []TrafficSource{SourceOutside}
			}
			if !slices.Equal(sources, want) {
				t.Fatalf("%s: sources %v, want %v", category, sources, want)
			}
		}
	}
}

func TestTrafficStatusRequiresCompleteObservedCounters(t *testing.T) {
	k := &StaticKernel{}
	a := Applier{
		Store:  core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName),
		Kernel: k, Boundary: StaticBoundary{Interfaces: []string{"lan0"}},
	}
	c := Config{Enabled: true}
	if err := a.save(c); err != nil {
		t.Fatal(err)
	}
	d := Render(c, []string{"lan0"}, nil)
	if err := k.Apply(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	input := d.Input[0].Line
	forward := d.Forward[0].Line
	k.Traffic[input] = TrafficCount{Packets: 11, Bytes: 1200}
	k.Traffic[forward] = TrafficCount{Packets: 23, Bytes: 4600}
	st, err := a.GetStatus(context.Background())
	if err != nil || !st.TrafficKnown || st.Drifted {
		t.Fatalf("status: %+v, error: %v", st, err)
	}
	var packets, bytes uint64
	for _, row := range st.Traffic {
		packets += row.Packets
		bytes += row.Bytes
		if row.ID == input && (row.Source != SourceOutside || row.Destination != "router" || row.Verdict != "allowed") {
			t.Fatalf("input attribution: %+v", row)
		}
		if row.ID == forward && row.Destination != "forwarded" {
			t.Fatalf("forward attribution: %+v", row)
		}
	}
	if packets != 34 || bytes != 5800 {
		t.Fatalf("double-counted or missing traffic: %d packets, %d bytes", packets, bytes)
	}
	delete(k.Traffic, input)
	st, _ = a.GetStatus(context.Background())
	if st.TrafficKnown {
		t.Fatal("missing counter reported as known zero")
	}
	k.State = k.State[:len(k.State)-1]
	st, _ = a.GetStatus(context.Background())
	if !st.Drifted || st.TrafficKnown || len(st.Traffic) != 0 {
		t.Fatalf("drifted counters must not be attributed: %+v", st)
	}
	if err := a.save(Config{}); err != nil {
		t.Fatal(err)
	}
	st, _ = a.GetStatus(context.Background())
	if st.TrafficKnown || len(st.Traffic) != 0 {
		t.Fatal("disabled firewall claims measured traffic")
	}
}
