package dial

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func brokerConfig() Config {
	c := uplinkConfig()
	c.Uplink.IPv6 = &UplinkIPv6{Via: ViaPD, PrefixLength: 56}
	c.TunnelBroker = &TunnelBroker{
		ServerIPv4: netip.MustParseAddr("216.218.226.238"), ServerIPv6: netip.MustParsePrefix("2001:470:a:652::1/64"),
		ClientIPv4: netip.MustParseAddr("111.196.184.83"), ClientIPv6: netip.MustParsePrefix("2001:470:a:652::2/64"),
		Routed64: netip.MustParsePrefix("2001:470:b:652::/64"), Routed48: netip.MustParsePrefix("2001:470:abcd::/48"),
	}
	return c
}

func TestSavingDisabledBrokerDoesNotTouchNativeIPv6OrKernel(t *testing.T) {
	c := brokerConfig()
	old := c.Clone()
	old.TunnelBroker = nil
	writer := &RecordingWriter{}
	hostCalls, dependentCalls := 0, 0
	a := Applier{Store: testStore(t, ""), Writer: writer, Host: func(context.Context) ([]core.Step, error) { hostCalls++; return nil, nil }}
	if _, err := a.Save(old); err != nil {
		t.Fatal(err)
	}
	h := HTTP{Applier: a, Lock: core.NewLock(), Events: core.NewEvents(), Dependents: func(context.Context) []core.Step { dependentCalls++; return nil }}.Handler()
	data, _ := json.Marshal(c.TunnelBroker)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("PATCH", "/config", strings.NewReader(`{"tunnelbroker":`+string(data)+`}`)))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	// Saving the same assignment a second time is also inventory only.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("PATCH", "/config", strings.NewReader(`{"tunnelbroker":`+string(data)+`}`)))
	if w.Code != 200 {
		t.Fatalf("idempotent save: %d %s", w.Code, w.Body)
	}
	if len(writer.Applied) != 0 || hostCalls != 0 || dependentCalls != 0 {
		t.Fatalf("inventory save changed runtime: %d %d %d", len(writer.Applied), hostCalls, dependentCalls)
	}
	saved, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved, c) {
		t.Fatalf("assignment/native settings changed: %+v", saved)
	}
	plan := buildPlan(old, c, nil, Observed{})
	if plan.Impact != impactNone || len(plan.Changes) != 1 {
		t.Fatalf("inventory plan: %+v", plan)
	}
	if DesiredFor(c).Tunnel != nil || !DesiredFor(c).AcceptRA {
		t.Fatal("disabled broker interfered with native PD")
	}
}

func TestBrokerActivationUsesSourceRoutingAlongsideNativePD(t *testing.T) {
	c := brokerConfig()
	c.TunnelBroker.Enabled = true
	if r := Validate(c, nil); !r.OK() {
		t.Fatal(r.Errors)
	}
	d := DesiredFor(c)
	if d.Tunnel == nil || !d.Tunnel.SourceRouting || !d.AcceptRA || !c.Uplink.HasPD() {
		t.Fatalf("desired: %+v", d)
	}
	obs := agreeingUplink()
	obs.V6DefaultDev = c.Uplink.Interface
	p := PlanTunnel(*d.Tunnel, obs)
	if p == nil || p.ReplacesRoute != "" {
		t.Fatalf("replaces native route: %+v", p)
	}
	if strings.Contains(strings.Join(DescribeTunnelPlan(p), "\n"), "route replace default dev") {
		t.Fatal("main default route would be replaced")
	}
	obs.Tunnel = TunnelObserved{Present: true, Sit: true, Up: true, MTU: d.Tunnel.MTU, Local: d.Tunnel.Local, Remote: d.Tunnel.Remote, Addrs: []netip.Prefix{d.Tunnel.Address}}
	obs.BrokerRoute = true
	for _, p := range d.Tunnel.Sources {
		if p.IsValid() {
			obs.BrokerSources = append(obs.BrokerSources, p)
		}
	}
	if p := PlanTunnel(*d.Tunnel, obs); p != nil {
		t.Fatalf("matching source routing drifts: %+v", p)
	}
	obs.BrokerSources = obs.BrokerSources[:1]
	if p := PlanTunnel(*d.Tunnel, obs); p == nil || !p.Route {
		t.Fatal("missing routed-prefix rule not detected")
	}
	off := c.Clone()
	off.TunnelBroker.Enabled = false
	if !RemovingTunnel(c, off) || DesiredFor(off).Tunnel != nil || !off.Uplink.HasPD() {
		t.Fatal("disable must retain native PD and remove only HE")
	}
}

func TestBrokerInventoryRoundTripAndValidation(t *testing.T) {
	c := brokerConfig()
	data, err := MarshalConfig(c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, got) {
		t.Fatal("round trip lost assignment")
	}
	copy := c.Clone()
	copy.TunnelBroker.Enabled = true
	if c.TunnelBroker.Enabled {
		t.Fatal("clone aliases assignment")
	}
	cases := []struct {
		name string
		edit func(*TunnelBroker)
	}{
		{"tunnel subnet as routed", func(b *TunnelBroker) { b.Routed64 = b.ClientIPv6.Masked() }},
		{"wrong routed size", func(b *TunnelBroker) { b.Routed48 = b.Routed64 }},
		{"wrong peer subnet", func(b *TunnelBroker) { b.ServerIPv6 = netip.MustParsePrefix("2001:470:c::1/64") }},
		{"private endpoint", func(b *TunnelBroker) { b.ClientIPv4 = netip.MustParseAddr("192.168.1.2") }},
		{"wrong client mask", func(b *TunnelBroker) { b.ClientIPv6 = netip.MustParsePrefix("2001:470:a:652::2/48") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next := c.Clone()
			tc.edit(next.TunnelBroker)
			if Validate(next, nil).OK() {
				t.Fatal("invalid assignment accepted")
			}
		})
	}
	c.TunnelBroker.Enabled = true
	c.Uplink = nil
	if Validate(c, nil).OK() {
		t.Fatal("enabled tunnel without uplink accepted")
	}
}
