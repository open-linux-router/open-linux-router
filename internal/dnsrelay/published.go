package dnsrelay

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
)

// Names this box answers for itself, with its own address.
//
// They are the names `ingress` publishes. Each one is served by the proxy on
// this box, so the right answer is an address of this box — and *which* one
// depends on who asked. A router with a LAN on 192.168.1.0/24 and another on
// 172.16.1.0/24 has an address on each, and a client should be sent to the one
// on its own network: the other may be firewalled from it, and even where it is
// not, it is a detour through the routing table for a packet that never needed
// to leave the segment.
//
// That rules out unbound's local-data, which is how every other local name is
// answered: local-data holds one fixed answer per name, and every query unbound
// sees now arrives from the relay on loopback, so even its views cannot tell
// the networks apart (policy.go says the same about blocking). The relay is the
// one thing that still knows where a query arrived. It answers with the
// addresses of the interface the query came in on — the same idea as the
// hijack rule's `redirect`, which also means "this box, as seen from here" and
// so never has to name an address that could go stale.
//
// It also removes the failure that made this necessary. Before, a published
// name was answered by nothing on this box: the query fell through to the
// internet, and if the operator had published a public record pointing at the
// router, unbound's rebinding protection correctly stripped it. The name did
// not resolve on the one network it was published for.

// Published is the published-names file, re-read on SIGHUP like the policy
// directory, so publishing a name never interrupts anybody's resolution.
type Published struct {
	// Names are canonical — lowercased, no trailing dot. The renderer
	// guarantees that, so the lookup does not have to normalise on the hot
	// path.
	Names []string `json:"names"`
}

// PublishedTTL is how long a client may cache a published name's address.
//
// Short, for the reason BlockTTL is: the answer is this box's address on the
// client's network, and a network that is renumbered should not leave clients
// pointed at the old one for an hour.
const PublishedTTL = 60

// MarshalPublished renders the published-names file.
func MarshalPublished(p Published) ([]byte, error) { return json.MarshalIndent(p, "", "  ") }

// UnmarshalPublished parses it strictly, for the reason UnmarshalConfig gives.
func UnmarshalPublished(data []byte) (Published, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var p Published
	if err := dec.Decode(&p); err != nil {
		return Published{}, fmt.Errorf("parsing published names: %w", err)
	}
	return p, nil
}

// LoadPublished reads the published-names file into a lookup set.
//
// A missing file is not an error: it is the ordinary state of a box that
// publishes nothing, and the renderer deletes the file rather than writing an
// empty one.
func LoadPublished(path string) (map[string]bool, error) {
	set := map[string]bool{}
	if path == "" {
		return set, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return set, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	p, err := UnmarshalPublished(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, name := range p.Names {
		set[name] = true
	}
	return set, nil
}

// arrival is where a query came in: the address it was sent to, and the
// interface it arrived on when the platform said. Either may be zero.
type arrival struct {
	addr    netip.Addr
	ifIndex int
}

// arrivalFrom reads a UDP query's destination out of its control message.
func arrivalFrom(r replyTo) arrival {
	addr, _ := netip.AddrFromSlice(r.src)
	return arrival{addr: addr.Unmap(), ifIndex: r.ifIndex}
}

// systemLocalAddrs returns the addresses of the interface a query arrived on.
//
// The interface by index when the control message named one, and otherwise
// the interface holding the destination address — which is the TCP case, where
// the accepted connection knows its local address and nothing else. If neither
// can be found, the destination address alone is still an address of this box
// that the client demonstrably reached.
func systemLocalAddrs(at arrival) []netip.Addr {
	var ifi *net.Interface
	if at.ifIndex > 0 {
		ifi, _ = net.InterfaceByIndex(at.ifIndex)
	}
	if ifi == nil && at.addr.IsValid() {
		ifi = interfaceHolding(at.addr)
	}

	var out []netip.Addr
	if ifi != nil {
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			prefix, err := netip.ParsePrefix(a.String())
			if err != nil {
				continue
			}
			if addr := prefix.Addr().Unmap(); answerable(addr) {
				out = append(out, addr)
			}
		}
	}
	if len(out) == 0 && answerable(at.addr) {
		out = append(out, at.addr)
	}
	return out
}

func interfaceHolding(addr netip.Addr) *net.Interface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for i := range ifaces {
		addrs, err := ifaces[i].Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if prefix, err := netip.ParsePrefix(a.String()); err == nil && prefix.Addr().Unmap() == addr {
				return &ifaces[i]
			}
		}
	}
	return nil
}

// answerable reports whether an address may be handed to a client.
//
// Link-local is excluded because it is meaningless without a zone, which an
// AAAA record cannot carry; loopback because it would send the client to
// itself.
func answerable(a netip.Addr) bool {
	return a.IsValid() && !a.IsUnspecified() && !a.IsLoopback() &&
		!a.IsLinkLocalUnicast() && !a.IsMulticast()
}
