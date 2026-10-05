//go:build !linux

package gateway

import (
	"context"
	"net"
)

// Never silently test the default route when the requested exit cannot be marked.
func markedLatencyDial(uint32) func(context.Context, string, string) (net.Conn, error) {
	return func(context.Context, string, string) (net.Conn, error) { return nil, ErrUnsupported }
}
