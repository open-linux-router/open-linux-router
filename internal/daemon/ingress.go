package daemon

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/devices"
	"github.com/open-linux-router/open-linux-router/internal/dns"
	"github.com/open-linux-router/open-linux-router/internal/ingress"
)

// Adapters joining the ingress module to the two it reads.
//
// Here rather than inside any of the three, for the reason the dhcp↔devices
// adapters are here: `ingress` declares the interfaces it needs (DNSView,
// DeviceView) and imports neither `dns` nor `devices`, so design.md §4.1's
// arrows keep pointing one way and the introduction happens at the one place
// that already knows the whole module list.

// ingressDNS is ingress's window onto the dns module.
//
// Both facts are read per request through dns's own Load. The domain especially:
// `ingress` has no domain of its own precisely so that the certificate it
// obtains and the zone the resolver serves cannot disagree, and that guarantee
// only holds if this reads the live value rather than a copy taken at startup.
type ingressDNS struct {
	applier dns.Applier
}

func (d ingressDNS) LocalDomain() string {
	cfg, err := d.applier.Load()
	if err != nil {
		// An unreadable dns config reads as "no domain", which the ingress
		// validator turns into a clear refusal naming `olr dns set`. Returning
		// a default here instead would be worse: it would let a certificate be
		// requested for a suffix the resolver is not actually serving.
		return ""
	}
	return cfg.LocalDomainOrDefault()
}

func (d ingressDNS) Hosts() []string {
	cfg, err := d.applier.Load()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(cfg.Hosts))
	for _, h := range cfg.Hosts {
		out = append(out, h.Name)
	}
	return out
}

// ingressDevices is ingress's window onto the devices module.
//
// Names, not MACs. `ingress` publishes a service by naming a device the way the
// operator does, and the join from that name to an address is `devices`' own —
// which is the whole reason this is an adapter and not a field in ingress's
// config.
type ingressDevices struct {
	applier devices.Applier
}

func (d ingressDevices) Device(name string) (ingress.DeviceInfo, error) {
	resolved, _, err := d.applier.List(context.Background())
	if err != nil {
		return ingress.DeviceInfo{}, err
	}

	want := strings.ToLower(strings.TrimSpace(name))
	for _, r := range resolved {
		if !strings.EqualFold(r.DisplayName(), want) {
			continue
		}
		return deviceInfo(r), nil
	}
	return ingress.DeviceInfo{}, fmt.Errorf("%q: %w", name, ingress.ErrNoSuchDevice)
}

// deviceInfo picks the one address a published service should point at.
//
// The fixed address wins outright, and nothing else is even considered when one
// exists. That is not a preference — it is the whole of docs/ingress.md §1.2:
// a published service needs an address that will still be this device's
// tomorrow, and only a reservation promises that. A device with no reservation
// still reports whatever it was last seen at, because the validator's refusal
// reads far better when it can say what the device *is* rather than only that
// it has no fixed address.
func deviceInfo(r devices.Resolved) ingress.DeviceInfo {
	info := ingress.DeviceInfo{Name: r.DisplayName()}

	if r.FixedIP != "" {
		if addr, err := netip.ParseAddr(r.FixedIP); err == nil {
			info.Addr, info.Fixed = addr, true
			return info
		}
	}

	if r.Presence == nil {
		return info
	}
	// The first address that parses, preferring IPv4: a published upstream is
	// dialled by the proxy, and a link-local IPv6 address — which is what a
	// neighbour table most often offers — is not something to hand a dialler as
	// a stable target.
	var v6 netip.Addr
	for _, ip := range r.Presence.IPs {
		addr, err := netip.ParseAddr(ip)
		if err != nil {
			continue
		}
		if addr.Is4() {
			info.Addr = addr
			return info
		}
		if !v6.IsValid() && !addr.IsLinkLocalUnicast() {
			v6 = addr
		}
	}
	info.Addr = v6
	return info
}
