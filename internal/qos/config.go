package qos

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"slices"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const ModuleName = "qos"

type Priority string

const (
	High   Priority = "high"
	Normal Priority = "normal"
	Low    Priority = "low"
)

type Device struct {
	MAC          string   `json:"mac"`
	Priority     Priority `json:"priority,omitempty"`
	DownloadMbps float64  `json:"download_mbps,omitempty"`
	UploadMbps   float64  `json:"upload_mbps,omitempty"`
}

type Config struct {
	Enabled      bool     `json:"enabled"`
	DownloadMbps float64  `json:"download_mbps,omitempty"`
	UploadMbps   float64  `json:"upload_mbps,omitempty"`
	Devices      []Device `json:"devices,omitempty"`
}

func (c *Config) Normalize() {
	for i := range c.Devices {
		if mac, err := core.NormalizeMAC(c.Devices[i].MAC); err == nil {
			c.Devices[i].MAC = mac
		}
		if c.Devices[i].Priority == "" {
			c.Devices[i].Priority = Normal
		}
	}
	slices.SortFunc(c.Devices, func(a, b Device) int {
		if a.MAC < b.MAC {
			return -1
		}
		if a.MAC > b.MAC {
			return 1
		}
		return 0
	})
}
func validRate(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 100000 }
func (c Config) Validate() error {
	if !validRate(c.DownloadMbps) || !validRate(c.UploadMbps) {
		return fmt.Errorf("link rates must be between 0 and 100000 Mbps")
	}
	if c.Enabled {
		return fmt.Errorf("device QoS enforcement is not available yet; keep it disabled")
	}
	seen := map[string]bool{}
	for _, d := range c.Devices {
		mac, err := core.NormalizeMAC(d.MAC)
		if err != nil {
			return fmt.Errorf("device MAC %q: %w", d.MAC, err)
		}
		if seen[mac] {
			return fmt.Errorf("device %s appears twice", mac)
		}
		seen[mac] = true
		if d.Priority != "" && d.Priority != High && d.Priority != Normal && d.Priority != Low {
			return fmt.Errorf("device %s: unknown priority %q", mac, d.Priority)
		}
		if !validRate(d.DownloadMbps) || !validRate(d.UploadMbps) {
			return fmt.Errorf("device %s: limits must be between 0 and 100000 Mbps", mac)
		}
	}
	return nil
}
func Parse(b []byte) (Config, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return c, err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return c, fmt.Errorf("extra JSON after configuration: %v", err)
	}
	c.Normalize()
	return c, c.Validate()
}
func FromDocument(doc core.Document) (Config, error) {
	b, ok := doc.Raw(ModuleName)
	if !ok {
		return Config{}, nil
	}
	return Parse(b)
}
func (c Config) Device(mac string) Device {
	if normalized, err := core.NormalizeMAC(mac); err == nil {
		mac = normalized
	}
	for _, d := range c.Devices {
		if d.MAC == mac {
			return d
		}
	}
	return Device{MAC: mac, Priority: Normal}
}
func (c *Config) SetDevice(d Device) {
	if mac, err := core.NormalizeMAC(d.MAC); err == nil {
		d.MAC = mac
	}
	c.RemoveDevice(d.MAC)
	if (d.Priority != "" && d.Priority != Normal) || d.DownloadMbps > 0 || d.UploadMbps > 0 {
		c.Devices = append(c.Devices, d)
	}
	c.Normalize()
}
func (c *Config) RemoveDevice(mac string) {
	if normalized, err := core.NormalizeMAC(mac); err == nil {
		mac = normalized
	}
	c.Devices = slices.DeleteFunc(c.Devices, func(d Device) bool { return d.MAC == mac })
}
