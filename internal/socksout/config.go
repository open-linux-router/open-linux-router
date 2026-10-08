// Package socksout runs a SOCKS5-backed TUN as a separate data-plane process.
package socksout

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const (
	ModuleName    = "socksout"
	UnitName      = "olr-socks-out.service"
	DefaultConfig = "/etc/open-linux-router/rendered/socks-out/config.json"
	Interface     = "olrsocks0"
)

// Config is consumed only by the data-plane process. The file contains a
// credential, so its permissions must not grant access to other users.
type Config struct {
	Enabled bool   `json:"enabled"`
	Proxy   string `json:"proxy,omitempty"`
}

func Parse(data []byte) (Config, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("SOCKS5 outbound configuration: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return Config{}, fmt.Errorf("SOCKS5 outbound configuration: trailing JSON: %v", err)
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func Read(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("reading SOCKS5 outbound configuration: %w", err)
	}
	return Parse(data)
}

// Validate requires an IP literal: resolving a hostname before the router's
// DNS is up would make a boot-time dependency cycle. DNS can still be configured
// separately for devices using this exit.
func (c Config) Validate() error {
	if !c.Enabled && c.Proxy == "" {
		return nil
	}
	u, err := url.Parse(c.Proxy)
	if err != nil || u == nil || u.Scheme != "socks5" || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Opaque != "" {
		return fmt.Errorf("proxy must be a socks5:// URL with an IP address and port")
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		return fmt.Errorf("proxy must include an IP address and port: %w", err)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLoopback() || ip.Is4In6() {
		return fmt.Errorf("proxy host must be a routable IP address")
	}
	if port == "" || u.Port() == "" {
		return fmt.Errorf("proxy port is required")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || strings.IndexFunc(port, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return fmt.Errorf("proxy port must be numeric")
	}
	if u.User != nil {
		return fmt.Errorf("proxy credentials are not supported yet: the TUN engine logs its proxy URL including credentials")
	}
	return nil
}

// FromDocument keeps intent in the same document as the other modules.
func FromDocument(d core.Document) (Config, error) {
	raw, ok := d.Raw(ModuleName)
	if !ok {
		return Config{}, nil
	}
	return Parse(raw)
}
