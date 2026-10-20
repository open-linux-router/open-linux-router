//go:build !linux

package daemon

import "encoding/json"

func captureConnectivity() (json.RawMessage, error) { return nil, nil }
func restoreConnectivity(json.RawMessage) error     { return nil }
