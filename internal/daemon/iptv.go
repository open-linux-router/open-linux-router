package daemon

import (
	"context"
	"log/slog"
	"net/netip"
	"slices"

	"github.com/open-linux-router/open-linux-router/internal/iptv"
	"github.com/open-linux-router/open-linux-router/internal/link"
)

type iptvLinks struct{ facts link.Facts }

func (l iptvLinks) Interface(name string) (bool, bool, bool, error) {
	i, e := l.facts.Interface(name)
	return i.Adopted, i.Present, slices.ContainsFunc(i.Prefixes, func(p netip.Prefix) bool { return p.Addr().Is4() }), e
}
func (l iptvLinks) Interfaces() ([]string, error) {
	all, e := l.facts.Interfaces()
	if e != nil {
		return nil, e
	}
	out := make([]string, 0, len(all))
	for _, i := range all {
		if i.Present {
			out = append(out, i.Name)
		}
	}
	return out, nil
}
func (l iptvLinks) Networks() ([]iptv.Network, error) {
	all, e := l.facts.Networks()
	if e != nil {
		return nil, e
	}
	out := make([]iptv.Network, 0, len(all))
	for _, n := range all {
		out = append(out, iptv.Network{Name: n.Name, Members: n.Members, IPv4: n.Subnet.IsValid()})
	}
	return out, nil
}
func startIPTV(ctx context.Context, a iptv.Applier, logger *slog.Logger) {
	c, e := a.Load()
	if e != nil {
		logger.Error("IPTV config could not be read", "error", e)
		return
	}
	if !c.Enabled {
		return
	}
	p, e := a.Plan(ctx, c)
	if e != nil {
		logger.Error("IPTV drift could not be read", "error", e)
		return
	}
	if p.Empty {
		return
	}
	steps, e := a.Apply(ctx, c, false)
	if e != nil {
		logger.Error("IPTV could not be restored", "error", e, "steps", steps)
	}
}
