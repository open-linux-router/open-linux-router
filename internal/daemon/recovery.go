package daemon

import (
	"context"
	"errors"
)

// Continue independent repairs after an error so a missing optional backend
// cannot prevent management addresses or the firewall from being recovered.
func reconcileConfiguration(ctx context.Context, apply ...func(context.Context) error) error {
	var failures []error
	for _, fn := range apply {
		failures = append(failures, fn(ctx))
	}
	return errors.Join(failures...)
}
