package daemon

import (
	"context"

	"github.com/open-linux-router/open-linux-router/internal/dns"
	"github.com/open-linux-router/open-linux-router/internal/inspection"
)

// inspectionNames adapts the resolver's observations to the shape the activity
// sampler needs.
//
// A nil observer is not an error: a build without a relay still records that
// bytes moved, they simply all land in "未命名", which is the truth.
func inspectionNames(observer dns.ObserveView) func(context.Context) ([]inspection.Name, error) {
	if observer == nil {
		return nil
	}
	return func(ctx context.Context) ([]inspection.Name, error) {
		names, _, err := observer.Names(ctx, dns.Unbounded)
		if err != nil {
			return nil, err
		}
		out := make([]inspection.Name, 0, len(names))
		for _, name := range names {
			out = append(out, inspection.Name{Client: name.Client, Addr: name.Addr, Name: name.Name})
		}
		return out, nil
	}
}
