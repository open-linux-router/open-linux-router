//go:build linux

package link

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func readInterfaceMetrics(iface *Interface) {
	readMetricsAt(iface, filepath.Join("/sys/class/net", iface.Name))
}

// Missing files, unsupported speed (-1), and a device removed during the read
// all leave gaps, without hiding the interface or reporting a made-up zero.
func readMetricsAt(iface *Interface, dir string) {
	read := func(name string) *uint64 {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil
		}
		value, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
		if err != nil {
			return nil
		}
		return &value
	}
	iface.SpeedMbps = read("speed")
	if iface.SpeedMbps != nil && *iface.SpeedMbps == 0 {
		iface.SpeedMbps = nil
	}
	iface.RXBytes = read("statistics/rx_bytes")
	iface.TXBytes = read("statistics/tx_bytes")
}
