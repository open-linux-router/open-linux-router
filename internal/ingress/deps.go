package ingress

import (
	"errors"
	"fmt"
	"net/netip"
)

// This module's read-only windows onto the two modules it depends on.
//
// Declared here by the consumer, and adapted in internal/daemon, which is the
// pattern dhcp uses for LinkView: `ingress` imports neither `dns` nor
// `devices`, states exactly the facts it needs, and never keeps a copy of
// either (design.md §4.1). Both edges are new to the §4.1 DAG and both point
// the same way every other edge does — at the module that owns the fact.

// DNSView is the window onto the `dns` module.
//
// There is exactly one fact here and it is load-bearing: the suffix this
// network answers for. Published names live under it, the wildcard certificate
// covers it, and the whole reason this module has no `domain` field of its own
// is that asking for it here cannot drift from what the resolver actually
// serves.
type DNSView interface {
	// LocalDomain returns the suffix local names live under — `dns`'s
	// LocalDomain, resolved through its own default.
	LocalDomain() string

	// Hosts returns the local names `dns` already answers for, relative to the
	// local domain.
	//
	// Read for one reason: to refuse a published service whose name is already
	// a device's. The two live in one namespace, and `dns` wins — it answers
	// the device's address directly and the request never reaches the proxy at
	// all. Nothing errors, nothing logs, the service is simply unreachable and
	// the Caddyfile looks perfectly correct while it happens. That is the
	// shape of bug worth spending an interface method to make impossible.
	Hosts() []string
}

// DeviceView is the window onto the `devices` module.
//
// Address resolution is read-through per request rather than cached, for the
// same reason dhcp reads link per request: a cached address is a copy, and a
// copy of something that changes is drift waiting for a lease to expire.
type DeviceView interface {
	// Device returns what is known about a device, or ErrNoSuchDevice.
	Device(name string) (DeviceInfo, error)
}

// DeviceInfo is the subset of a device's state that publishing depends on.
type DeviceInfo struct {
	// Name is the device's name in the `devices` module.
	Name string `json:"name,omitempty"`

	// Addr is where the device is right now.
	Addr netip.Addr `json:"address,omitempty"`

	// Fixed reports whether Addr is a fixed address owned by `dhcp`, as
	// opposed to whatever the device happens to hold from a dynamic pool.
	//
	// This is the difference between a published service that keeps working and
	// one that quietly proxies to a stranger's laptop after a lease turns over,
	// so validate.go refuses the dynamic case rather than warning about it.
	// docs/ingress.md §1.2 has the argument, and §5.6's rule that automatic
	// behaviour is declared rather than inferred is why the answer is a refusal
	// carrying a remedy instead of olr silently pinning the address itself.
	Fixed bool `json:"fixed"`
}

// ErrNoSuchDevice is returned by DeviceView.Device for an unknown name.
var ErrNoSuchDevice = errors.New("no such device")

// StaticDNS is a DNSView backed by literal values, for tests.
type StaticDNS struct {
	Domain    string
	HostNames []string
}

// LocalDomain implements DNSView.
func (s StaticDNS) LocalDomain() string { return s.Domain }

// Hosts implements DNSView.
func (s StaticDNS) Hosts() []string { return s.HostNames }

// StaticDevices is a DeviceView backed by a map, for tests.
//
// The validation rules are the largest thing in this module and none of them
// need a network to exercise, which is what §5.3.1 is for.
type StaticDevices map[string]DeviceInfo

// Device implements DeviceView.
func (s StaticDevices) Device(name string) (DeviceInfo, error) {
	info, ok := s[name]
	if !ok {
		return DeviceInfo{}, fmt.Errorf("%q: %w", name, ErrNoSuchDevice)
	}
	if info.Name == "" {
		info.Name = name
	}
	return info, nil
}
