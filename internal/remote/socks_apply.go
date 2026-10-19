package remote

import "github.com/open-linux-router/open-linux-router/internal/core"

type ProxyObject string

const (
	// ObjectSocks is the plain SOCKS5 proxy.
	ObjectSocks ProxyObject = "socks5"
)

// proxyKind is the per-object half of an applier's behaviour.
type proxyKind struct {
	Object string

	// Unit is the systemd unit this object's daemon runs under.
	Unit string

	// Conf is where the rendered file goes.
	Conf func(Paths) string

	// Enabled reads this object's on/off out of a whole config.
	Enabled func(Config) bool

	// Locate finds the backend binary, and Missing explains its absence.
	Locate  func() (string, error)
	Missing func() error

	// Build is the object's plan builder.
	Build func(desired, previous Config, paths Paths, obs ProxyObserved) (ProxyPlan, Rendered, error)

	// Validate is the object's rules, run before a plan is built.
	Validate func(Config) Result

	// Fill generates the credential this object cannot work without, reporting
	// whether it changed anything. It is called only when the object is
	// enabled, and it is derived state rather than inferred behaviour: nothing
	// an operator could type would be better than random bytes.
	Fill func(*Config) (bool, error)

	// Redacted picks this object's own section out of an already-redacted
	// config, for the apply envelope. A client asking about one proxy is never
	// told about the other.
	Redacted func(Config) any
}

func (o ProxyObject) kind() proxyKind {
	if o == ObjectSocks || o == "" {
		return proxyKind{
			Object:   "SOCKS5",
			Unit:     SocksUnitName,
			Conf:     func(p Paths) string { return p.SocksConf },
			Enabled:  func(c Config) bool { return c.Socks.Enabled },
			Locate:   FindSocks,
			Missing:  ErrSocksMissing,
			Build:    BuildSocksPlan,
			Validate: ValidateSocks,
			Redacted: func(c Config) any { s := c.Socks; return &s },
			Fill: func(c *Config) (bool, error) {
				next, did, err := c.Socks.WithGeneratedPassword()
				if err != nil {
					return false, err
				}
				c.Socks = next
				return did, nil
			},
		}
	}
	panic("unsupported proxy object")
}

// SocksBlockers reports what stands between this box and a working SOCKS5
// proxy.
//
// Deliberately not a core.Dependency, for the reason socks_binary.go gives at
// length: 3proxy is not in Debian but publishes its own apt repository, so the
// fix is two steps and core/dependency.go can only render one — and would
// attach a button to it that fails until the repository is added.
func SocksBlockers() []core.Blocker {
	if _, err := FindSocks(); err == nil {
		return nil
	}
	return []core.Blocker{{
		Kind:    "missing_dependency",
		Summary: "No SOCKS5 server is installed on this box.",
		Detail: "olr does not implement SOCKS5 — 3proxy does, and olr renders its " +
			"configuration and supervises it.",
		Fix: socksInstallHint(),
	}}
}
