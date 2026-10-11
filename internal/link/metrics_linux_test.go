//go:build linux

package link

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInterfaceMetricsMissingUnsupportedAndZero(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "statistics"), 0755); err != nil {
		t.Fatal(err)
	}
	write := func(name, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0644); err != nil {
			t.Fatal(err)
		}
	}
	iface := Interface{Name: "lan0", Index: 7}
	readMetricsAt(&iface, dir)
	if iface.SpeedMbps != nil || iface.RXBytes != nil || iface.TXBytes != nil {
		t.Fatal("missing readings must remain absent")
	}
	write("speed", "-1\n")
	write("statistics/rx_bytes", "0\n")
	write("statistics/tx_bytes", "invalid\n")
	readMetricsAt(&iface, dir)
	if iface.SpeedMbps != nil || iface.RXBytes == nil || *iface.RXBytes != 0 || iface.TXBytes != nil {
		t.Fatalf("unsupported speed, observed zero, invalid counter: %+v", iface)
	}
	write("speed", "2500\n")
	write("statistics/tx_bytes", "123456\n")
	readMetricsAt(&iface, dir)
	if iface.SpeedMbps == nil || *iface.SpeedMbps != 2500 || iface.TXBytes == nil || *iface.TXBytes != 123456 {
		t.Fatalf("valid readings: %+v", iface)
	}
	v := viewInterface(Info{Name: "lan0", Present: true}, map[string]Interface{"lan0": iface}, Config{})
	encoded, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["rx_bytes"] != float64(0) || fields["tx_bytes"] != float64(123456) || fields["speed_mbps"] != float64(2500) || fields["index"] != float64(7) {
		t.Fatalf("API dropped observed zero or metrics: %s", encoded)
	}
	write("speed", "0\n")
	readMetricsAt(&iface, dir)
	if iface.SpeedMbps != nil {
		t.Fatal("zero speed is unavailable, not a 0 Mbps link")
	}
}
