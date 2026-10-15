package dial

import (
	"encoding/json"
	"net/netip"
)

// Reserved independently of Gateway's 8100–8180 exit tables and priorities.
const BrokerTable = 7900
const BrokerPriority = 7900

// TunnelBroker stores the provider's complete assignment independently of the
// native uplink. Saving a disabled assignment does not configure the network.
type TunnelBroker struct {
	Enabled    bool         `json:"enabled"`
	TunnelID   string       `json:"tunnel_id,omitempty"`
	ServerIPv4 netip.Addr   `json:"server_ipv4"`
	ServerIPv6 netip.Prefix `json:"server_ipv6,omitempty,omitzero"`
	ClientIPv4 netip.Addr   `json:"client_ipv4,omitempty,omitzero"`
	ClientIPv6 netip.Prefix `json:"client_ipv6"`
	Routed64   netip.Prefix `json:"routed_64,omitempty,omitzero"`
	Routed48   netip.Prefix `json:"routed_48,omitempty,omitzero"`
	MTU        int          `json:"mtu,omitempty"`
}

// ActiveTunnel includes the legacy uplink form for existing installations.
func (c Config) ActiveTunnel() *UplinkIPv6 {
	if b := c.TunnelBroker; b != nil && b.Enabled && c.Uplink != nil {
		return &UplinkIPv6{Via: Via6in4, Server: b.ServerIPv4, Address: b.ClientIPv6, MTU: b.MTU}
	}
	if c.Uplink.HasTunnel() {
		v := *c.Uplink.IPv6
		return &v
	}
	return nil
}

func validateTunnelBroker(r *Result, c Config) {
	b := c.TunnelBroker
	if b == nil {
		return
	}
	// Reuse the tunnel endpoint rules, reporting fields in the saved assignment.
	tmp := Result{}
	validateUplinkIPv6(&tmp, &Uplink{IPv6: &UplinkIPv6{Via: Via6in4, Server: b.ServerIPv4, Address: b.ClientIPv6, MTU: b.MTU}})
	for _, p := range tmp.Errors {
		field := map[string]string{"uplink.ipv6.server": "server_ipv4", "uplink.ipv6.address": "client_ipv6", "uplink.ipv6.mtu": "mtu"}[p.Path]
		r.errorf("tunnelbroker."+field, "%s", p.Message)
	}
	if p := b.ClientIPv6; p.IsValid() && (p.Bits() != 64 || p.Addr().IsPrivate() || !p.Addr().IsGlobalUnicast()) {
		r.errorf("tunnelbroker.client_ipv6", "use HE's public Client IPv6 Address including /64")
	}
	if b.ClientIPv4.IsValid() && (!b.ClientIPv4.Is4() || !b.ClientIPv4.IsGlobalUnicast() || b.ClientIPv4.IsPrivate()) {
		r.errorf("tunnelbroker.client_ipv4", "use HE's public Client IPv4 Address")
	}
	if s := b.ServerIPv6; s.IsValid() && (!s.Addr().Is6() || !s.Addr().IsGlobalUnicast() || s.Bits() != 64 || !b.ClientIPv6.Contains(s.Addr()) || s.Addr() == b.ClientIPv6.Addr()) {
		r.errorf("tunnelbroker.server_ipv6", "use HE's Server IPv6 Address in the client's tunnel /64")
	}
	for _, item := range []struct {
		name   string
		prefix netip.Prefix
		bits   int
	}{{"routed_64", b.Routed64, 64}, {"routed_48", b.Routed48, 48}} {
		p := item.prefix
		if p.IsValid() && (!p.Addr().Is6() || !p.Addr().IsGlobalUnicast() || p.Addr().IsPrivate() || p.Bits() != item.bits || p != p.Masked() || p.Overlaps(b.ClientIPv6)) {
			r.errorf("tunnelbroker."+item.name, "use HE's separate Routed /%d network", item.bits)
		}
	}
	if b.Enabled {
		if c.Uplink == nil {
			r.errorf("tunnelbroker.enabled", "set up an uplink before enabling the tunnel")
		}
		if c.Uplink.HasTunnel() {
			r.errorf("tunnelbroker.enabled", "remove the legacy uplink tunnel before enabling this assignment")
		}
		r.warnf("tunnelbroker", "HE-addressed traffic uses the tunnel; native IPv6 keeps its existing route. LAN addresses and advertisements are configured separately. The IPv4 path must pass IP protocol 41.")
	}
}

func planTunnelBroker(plan *planView, stored, desired Config) {
	a, _ := json.Marshal(stored.TunnelBroker)
	b, _ := json.Marshal(desired.TunnelBroker)
	if string(a) == string(b) {
		return
	}
	impact := impactNone
	if (stored.TunnelBroker != nil && stored.TunnelBroker.Enabled) || (desired.TunnelBroker != nil && desired.TunnelBroker.Enabled) {
		impact = impactDisruptive
	}
	plan.Changes = append(plan.Changes, changeView{Path: "tunnelbroker", Kind: kindUpdate, Impact: impact, Diff: "- " + string(a) + "\n+ " + string(b) + "\n"})
}
