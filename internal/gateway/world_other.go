//go:build !linux

package gateway

import (
	"context"
	"errors"
)

func readWorldFlows(context.Context) ([]worldFlow, bool, error) {
	return nil, false, errors.New("connection accounting requires Linux")
}
