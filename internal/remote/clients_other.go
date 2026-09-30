//go:build !linux

package remote

import (
	"context"
	"errors"
)

// NewConnTable returns the connection table for this platform. Off Linux there
// is none, and saying so beats reporting nobody connected.
func NewConnTable() ConnTable { return unsupportedConns{} }

type unsupportedConns struct{}

func (unsupportedConns) Flows(context.Context) ([]Flow, bool, error) {
	return nil, false, errors.New("the connection table can only be read on Linux")
}
