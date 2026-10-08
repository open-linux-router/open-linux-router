//go:build !linux

package tools

import "errors"

func startIperf(string, int) (func(), <-chan struct{}, error) {
	return nil, nil, errors.New("LAN test server requires Linux")
}

func probeIperf(string) (string, error) { return "", nil }
