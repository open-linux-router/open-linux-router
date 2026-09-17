package remote

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
)

// Turning the proxy's intent into the two things that leave this package: the
// file `ssserver` reads, and the `ss://` line a client imports.
//
// The contrast with the object beside it is sharp enough to be worth naming.
// WireGuard's configuration is handed to `wg` on a pipe and never written down,
// because a peer's key exists for one instant. This one *is* a file on disk,
// mode 0600, because a daemon has to re-read it on every start — and the
// password in it is deliberately re-showable, since a client cannot be
// configured without it and there is exactly one for all of them.

// UnitName is the systemd unit for the proxy.
//
// `olr-shadowsocks.service`, not `shadowsocks-rust.service` or anything a
// distribution might ship: unit name and config path both differ from any
// packaged one, so a box already running a Shadowsocks server has nothing to
// resolve. This is the same rule internal/ingress applies to Caddy, and it is
// the difference between working and not on a box that has done this before.
const UnitName = "olr-shadowsocks.service"

// Paths locates everything the proxy reads or writes.
//
// A struct rather than constants so tests can render into a temporary directory
// and still get a config whose internal references are self-consistent.
type Paths struct {
	// Conf is the JSON file `ssserver -c` is given. It holds the password, so
	// it is written 0600 and withheld from every surface that displays a file.
	Conf string
}

// DefaultPaths is the on-disk layout for a real install.
func DefaultPaths() Paths { return RootedPaths("") }

// RootedPaths is the same layout relocated under root, for development against
// a scratch directory without root or systemd.
func RootedPaths(root string) Paths {
	return Paths{
		Conf: filepath.Join(root, "/etc/open-linux-router/rendered/remote/shadowsocks.json"),
	}
}

// File is one rendered file.
type File struct {
	Path string
	Mode fs.FileMode
	Data []byte

	// Secret suppresses the contents on every surface that displays a file: the
	// plan diff, the drift report, the logs. The file is still written and
	// still compared byte for byte — only its *display* is withheld.
	Secret bool
}

// Rendered is the complete set of files a config produces, sorted by path so
// that drift detection compares like with like.
type Rendered struct {
	Files []File
}

// Get returns a file by path.
func (r Rendered) Get(path string) (File, bool) {
	i := slices.IndexFunc(r.Files, func(f File) bool { return f.Path == path })
	if i < 0 {
		return File{}, false
	}
	return r.Files[i], true
}

// Paths lists the rendered paths, in order.
func (r Rendered) Paths() []string {
	out := make([]string, len(r.Files))
	for i, f := range r.Files {
		out[i] = f.Path
	}
	return out
}

// RenderShadowsocks turns intent into the files the proxy reads.
//
// It assumes the config has been validated; an unvalidated one produces
// nonsense rather than an error, which is why every caller validates first
// (design.md §5.3.1).
func RenderShadowsocks(c Config, paths Paths) (Rendered, error) {
	conf, err := shadowsocksConf(c.Shadowsocks)
	if err != nil {
		return Rendered{}, err
	}
	return Rendered{Files: []File{{
		Path: paths.Conf,
		// 0600 and not a byte looser. Anyone who can read this file can use the
		// proxy, and systemd reads it as PID 1 before the service starts, so
		// nothing is lost by keeping it unreadable to everything else.
		Mode:   0o600,
		Data:   conf,
		Secret: true,
	}}}, nil
}

// serverConf is the shape shadowsocks-rust's single-server configuration takes.
//
// A struct rather than a map so the field names are checked once, here, rather
// than being string literals scattered through a builder. The escape hatch is
// merged in afterwards (see shadowsocksConf), which is why this is marshalled
// to a map on the way out rather than emitted directly.
type serverConf struct {
	// Server is the listen address.
	//
	// 0.0.0.0 and not `::`. Dual-stack would be better and is one config key
	// away, but that key fails to bind on a box with IPv6 disabled — and a
	// proxy that refuses to start is a worse default than one that serves only
	// IPv4. docs/remote-access.md §11 keeps it open; the tunnel beside it is
	// IPv4-only for its own reasons, so the two are at least consistent.
	Server string `json:"server"`

	ServerPort uint16 `json:"server_port"`
	Password   string `json:"password"`
	Method     string `json:"method"`

	// Mode is `tcp_and_udp` unless the operator narrowed it. Upstream's default
	// is `tcp_only`, which presents as "the web works and some apps do not".
	Mode string `json:"mode"`
}

func shadowsocksConf(s Shadowsocks) ([]byte, error) {
	mode := "tcp_and_udp"
	if !s.UDPEnabled() {
		mode = "tcp_only"
	}

	base := serverConf{
		Server:     "0.0.0.0",
		ServerPort: s.PortOrDefault(),
		Password:   s.Password,
		Method:     string(s.Cipher.OrDefault()),
		Mode:       mode,
	}

	// Marshalled to a map so the escape hatch can be merged key by key. JSON
	// has no comments and no include, so an additive hatch has to be a merge —
	// which also means a key olr renders can be overridden, unlike the
	// Caddyfile hatch next door. The validator refuses the ones that would
	// contradict the config that produced them.
	raw, err := json.Marshal(base)
	if err != nil {
		return nil, err
	}
	var merged map[string]any
	if err := json.Unmarshal(raw, &merged); err != nil {
		return nil, err
	}

	if extra := strings.TrimSpace(s.ExtraConf); extra != "" {
		var overrides map[string]any
		if err := json.Unmarshal([]byte(extra), &overrides); err != nil {
			return nil, fmt.Errorf("raw_shadowsocks_conf is not a JSON object: %w", err)
		}
		for k, v := range overrides {
			merged[k] = v
		}
	}

	// Indented, and with a trailing newline, because this file is read by
	// people as well as by the daemon — and because a stable rendering is what
	// lets drift be a byte comparison.
	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// ClientURL renders the `ss://` line a client imports.
//
// SIP002's form: the method and password are base64 in the userinfo, the
// endpoint is the host and port a client dials, and the fragment is a label the
// client app shows in its list. This is what `ssurl` emits and what every
// client accepts, including the ones too old to read the newer plain form.
//
// Unlike WireGuard's client configuration this can be produced again whenever
// it is asked for, because there is one password for every client and olr
// stores it. That asymmetry is not an inconsistency: a WireGuard peer has a key
// of its own and revoking one revokes one device, while here every client holds
// the same secret and "revoking" is changing it for everybody.
func ClientURL(c Config, label string) (string, error) {
	s := c.Shadowsocks
	if s.Password == "" {
		return "", fmt.Errorf("no password has been generated yet")
	}
	dial := c.DialAddress(s.DialPort())
	if dial == "" {
		return "", fmt.Errorf("no endpoint is set, so there is no address for a client to dial")
	}

	userinfo := base64.RawURLEncoding.EncodeToString(
		[]byte(string(s.Cipher.OrDefault()) + ":" + s.Password))

	out := "ss://" + userinfo + "@" + dial
	if label = strings.TrimSpace(label); label != "" {
		out += "#" + url.PathEscape(label)
	}
	return out, nil
}
