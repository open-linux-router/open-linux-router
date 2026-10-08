//go:build !linux

package tools

import (
	"context"
	"errors"
)

type ForwardSample struct {
	Second int     `json:"second"`
	Mbps   float64 `json:"mbps"`
}
type ForwardRun struct {
	Name    string          `json:"name"`
	Mbps    float64         `json:"mbps"`
	PPS     float64         `json:"pps,omitempty"`
	Loss    float64         `json:"loss_percent,omitempty"`
	Samples []ForwardSample `json:"samples"`
}
type ForwardResult struct {
	Runs []ForwardRun `json:"runs"`
	Note string       `json:"note"`
}

func forwardBenchmark(context.Context) (ForwardResult, error) {
	return ForwardResult{}, errors.New("local forwarding test requires Linux")
}
