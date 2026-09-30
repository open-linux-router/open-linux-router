// Package dhcpv6 is the client half of DHCPv6 prefix delegation (RFC 8415):
// just enough of the protocol to ask an ISP for a prefix and keep it.
//
// Stdlib only, and small on purpose. What a router needs from DHCPv6 on its
// uplink is one identity association for prefix delegation (IA_PD) and the
// lease that comes back — not addresses (the uplink gets its own by SLAAC),
// not the relay or server halves, not Reconfigure. A general library would be
// a dependency for the parts of the protocol this does not speak; the parts it
// does speak fit in two files and are tested byte for byte below them.
package dhcpv6

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"
)

// MessageType is a DHCPv6 message type (RFC 8415 §7.3).
type MessageType byte

const (
	Solicit   MessageType = 1
	Advertise MessageType = 2
	Request   MessageType = 3
	Renew     MessageType = 5
	Rebind    MessageType = 6
	Reply     MessageType = 7
	Release   MessageType = 8
)

func (t MessageType) String() string {
	switch t {
	case Solicit:
		return "Solicit"
	case Advertise:
		return "Advertise"
	case Request:
		return "Request"
	case Renew:
		return "Renew"
	case Rebind:
		return "Rebind"
	case Reply:
		return "Reply"
	case Release:
		return "Release"
	}
	return fmt.Sprintf("type %d", byte(t))
}

// Option codes this client reads or writes (RFC 8415 §21, RFC 3646).
const (
	optClientID    = 1
	optServerID    = 2
	optORO         = 6
	optPreference  = 7
	optElapsedTime = 8
	optStatusCode  = 13
	optRapidCommit = 14
	optDNSServers  = 23
	optIAPD        = 25
	optIAPrefix    = 26
)

// Status codes worth naming (RFC 8415 §21.13).
const (
	StatusSuccess       = 0
	StatusNoPrefixAvail = 6
)

// Infinity is the lifetime value meaning "forever" (RFC 8415 §7.7).
const Infinity = 0xffffffff

// Ports (RFC 8415 §7.2).
const (
	ClientPort = 546
	ServerPort = 547
)

// AllServers is All_DHCP_Relay_Agents_and_Servers, where every client message
// goes (RFC 8415 §7.1).
var AllServers = netip.MustParseAddr("ff02::1:2")

// Message is one DHCPv6 message as this client sees it.
type Message struct {
	Type MessageType
	XID  [3]byte

	ClientID []byte
	ServerID []byte

	// Elapsed is the time since the exchange began, sent in hundredths of a
	// second and capped at 0xffff as the RFC says.
	Elapsed time.Duration

	// Preference is the server's, from an Advertise; 255 means "take this one
	// now" (RFC 8415 §18.2.9).
	Preference int

	RapidCommit bool

	// RequestDNS asks for the DNS servers option in the ORO.
	RequestDNS bool
	DNS        []netip.Addr

	// Status is the message-level status code, StatusSuccess when absent.
	Status        int
	StatusMessage string

	IAPD *IAPD
}

// IAPD is an identity association for prefix delegation (RFC 8415 §21.21).
type IAPD struct {
	IAID   uint32
	T1, T2 time.Duration

	Prefixes []IAPrefix

	// Status is the IA's own status code, where NoPrefixAvail lives.
	Status        int
	StatusMessage string
}

// IAPrefix is one delegated prefix and its lifetimes (RFC 8415 §21.22).
//
// A zero Prefix with a non-zero length is how a Solicit hints at the size it
// wants — "::/56" — which is what an ISP that hands out several sizes reads.
type IAPrefix struct {
	Prefix    netip.Prefix
	Preferred time.Duration
	Valid     time.Duration
}

// DUIDLL is a DUID-LL (RFC 8415 §11.4): type 3, hardware type 1 (Ethernet),
// and the MAC.
//
// The link-layer form rather than DUID-LLT because it is a function of the
// interface alone. An ISP keys the delegation on (DUID, IAID); a DUID that
// survives a reinstall, a restart and a lost state file is what gets the same
// prefix back each time, and that is the one property of this value that
// matters to anybody behind the router.
func DUIDLL(mac net.HardwareAddr) []byte {
	out := []byte{0, 3, 0, 1}
	return append(out, mac...)
}

// Encode serialises the message.
func (m Message) Encode() []byte {
	b := []byte{byte(m.Type), m.XID[0], m.XID[1], m.XID[2]}
	if len(m.ClientID) > 0 {
		b = appendOpt(b, optClientID, m.ClientID)
	}
	if len(m.ServerID) > 0 {
		b = appendOpt(b, optServerID, m.ServerID)
	}
	if m.Type != Reply && m.Type != Advertise {
		cs := m.Elapsed / (10 * time.Millisecond)
		if cs > 0xffff {
			cs = 0xffff
		}
		b = appendOpt(b, optElapsedTime, binary.BigEndian.AppendUint16(nil, uint16(cs)))
	}
	if m.RequestDNS {
		b = appendOpt(b, optORO, binary.BigEndian.AppendUint16(nil, optDNSServers))
	}
	if m.RapidCommit {
		b = appendOpt(b, optRapidCommit, nil)
	}
	if m.IAPD != nil {
		b = appendOpt(b, optIAPD, m.IAPD.encode())
	}
	return b
}

func (ia IAPD) encode() []byte {
	b := binary.BigEndian.AppendUint32(nil, ia.IAID)
	b = binary.BigEndian.AppendUint32(b, seconds(ia.T1))
	b = binary.BigEndian.AppendUint32(b, seconds(ia.T2))
	for _, p := range ia.Prefixes {
		v := binary.BigEndian.AppendUint32(nil, seconds(p.Preferred))
		v = binary.BigEndian.AppendUint32(v, seconds(p.Valid))
		v = append(v, byte(p.Prefix.Bits()))
		addr := p.Prefix.Addr().As16()
		v = append(v, addr[:]...)
		b = appendOpt(b, optIAPrefix, v)
	}
	return b
}

// ErrShort is returned for a message or option cut off before its end.
var ErrShort = errors.New("dhcpv6: truncated")

// Decode parses a message. Options it does not know are skipped; an option it
// knows and cannot parse is an error, since a lease read from half of one is
// worse than no lease.
func Decode(b []byte) (Message, error) {
	if len(b) < 4 {
		return Message{}, ErrShort
	}
	m := Message{Type: MessageType(b[0]), XID: [3]byte{b[1], b[2], b[3]}}
	err := walkOpts(b[4:], func(code uint16, v []byte) error {
		switch code {
		case optClientID:
			m.ClientID = append([]byte(nil), v...)
		case optServerID:
			m.ServerID = append([]byte(nil), v...)
		case optPreference:
			if len(v) != 1 {
				return ErrShort
			}
			m.Preference = int(v[0])
		case optRapidCommit:
			m.RapidCommit = true
		case optStatusCode:
			m.Status, m.StatusMessage = decodeStatus(v)
		case optDNSServers:
			for len(v) >= 16 {
				m.DNS = append(m.DNS, netip.AddrFrom16([16]byte(v[:16])))
				v = v[16:]
			}
		case optIAPD:
			ia, err := decodeIAPD(v)
			if err != nil {
				return err
			}
			// One IA_PD is all this client asks for; a second one is not ours.
			if m.IAPD == nil {
				m.IAPD = &ia
			}
		}
		return nil
	})
	return m, err
}

func decodeIAPD(v []byte) (IAPD, error) {
	if len(v) < 12 {
		return IAPD{}, ErrShort
	}
	ia := IAPD{
		IAID: binary.BigEndian.Uint32(v[0:4]),
		T1:   duration(binary.BigEndian.Uint32(v[4:8])),
		T2:   duration(binary.BigEndian.Uint32(v[8:12])),
	}
	err := walkOpts(v[12:], func(code uint16, o []byte) error {
		switch code {
		case optStatusCode:
			ia.Status, ia.StatusMessage = decodeStatus(o)
		case optIAPrefix:
			if len(o) < 25 {
				return ErrShort
			}
			bits := int(o[8])
			if bits > 128 {
				return fmt.Errorf("dhcpv6: prefix length %d", bits)
			}
			ia.Prefixes = append(ia.Prefixes, IAPrefix{
				Preferred: duration(binary.BigEndian.Uint32(o[0:4])),
				Valid:     duration(binary.BigEndian.Uint32(o[4:8])),
				Prefix:    netip.PrefixFrom(netip.AddrFrom16([16]byte(o[9:25])), bits).Masked(),
			})
		}
		return nil
	})
	return ia, err
}

func decodeStatus(v []byte) (int, string) {
	if len(v) < 2 {
		return StatusSuccess, ""
	}
	return int(binary.BigEndian.Uint16(v[:2])), string(v[2:])
}

func walkOpts(b []byte, fn func(code uint16, v []byte) error) error {
	for len(b) > 0 {
		if len(b) < 4 {
			return ErrShort
		}
		code := binary.BigEndian.Uint16(b[0:2])
		n := int(binary.BigEndian.Uint16(b[2:4]))
		if len(b) < 4+n {
			return ErrShort
		}
		if err := fn(code, b[4:4+n]); err != nil {
			return err
		}
		b = b[4+n:]
	}
	return nil
}

func appendOpt(b []byte, code uint16, v []byte) []byte {
	b = binary.BigEndian.AppendUint16(b, code)
	b = binary.BigEndian.AppendUint16(b, uint16(len(v)))
	return append(b, v...)
}

// seconds encodes a lifetime, with anything at or past Infinity as Infinity.
func seconds(d time.Duration) uint32 {
	s := d / time.Second
	if s >= Infinity || d < 0 {
		return Infinity
	}
	return uint32(s)
}

// duration decodes a lifetime. Infinity decodes to a duration long enough
// that no timer built from it fires, rather than to a sentinel every caller
// would have to remember.
func duration(s uint32) time.Duration {
	if s == Infinity {
		return 100 * 365 * 24 * time.Hour
	}
	return time.Duration(s) * time.Second
}
