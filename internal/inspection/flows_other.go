//go:build !linux

package inspection

import (
	"context"
	"errors"
)

// readFlows has no meaning off Linux: conntrack is a Linux interface, and the
// chart says so rather than drawing an empty week.
func readFlows(context.Context) ([]Flow, error) {
	return nil, errors.New("per-site activity requires Linux")
}
