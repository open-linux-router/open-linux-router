package dial

import (
	"fmt"
	"strings"
	"time"
)

// Duration is a time.Duration that round-trips through JSON as a string.
//
// Without the wrapper a five-minute check interval serialises as 300000000000,
// which is unreadable in a config file and hostile in an API response
// (design.md §10, config format).
//
// The fourth copy of this wrapper in the tree, after internal/dhcp,
// internal/dnsrelay and internal/gateway, and copied rather than shared for the
// reason internal/gateway/duration.go states: dhcp's accepts days and weeks
// because a two-day DHCP lease is ordinary, and stretching one type to cover
// both would mean publishing "2w" as a legal DDNS check interval. The
// duplication is six lines; a shared type that lies about its own range on
// three of four surfaces is worse.
type Duration time.Duration

// Duration converts back to the stdlib type.
func (d Duration) Duration() time.Duration { return time.Duration(d) }

// String renders the duration as time.Duration does — "5m", "1m30s".
func (d Duration) String() string { return time.Duration(d).String() }

// MarshalText makes Duration a JSON string.
func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// UnmarshalText parses that string back.
//
// The pair has to exist together: without this a config can be written but
// never read, and every surface here is a client of its own module's API.
func (d *Duration) UnmarshalText(text []byte) error {
	parsed, err := time.ParseDuration(strings.TrimSpace(string(text)))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", text, err)
	}
	*d = Duration(parsed)
	return nil
}
