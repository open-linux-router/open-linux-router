package tools

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"time"
)

const stunMagic uint32 = 0x2112a442

var stunServers = []string{
	"stun.1und1.de:3478", // Supports RFC 5780 when its alternate address is reachable.
	"stun1.l.google.com:19302",
	"stun.antisip.com:3478",
	"stun.cloudflare.com:3478",
}

type NATResult struct {
	Server    string `json:"server"`
	Mapped    string `json:"mapped"`
	Mapping   string `json:"mapping"`
	Filtering string `json:"filtering"`
}

type stunReply struct{ mapped, other string }

var excludedAlternates = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

func runNAT(ctx context.Context) (NATResult, error) {
	var last error
	for _, server := range stunServers {
		result, err := probeNAT(ctx, server)
		if err == nil {
			return result, nil
		}
		last = fmt.Errorf("%s: %w", server, err)
	}
	return NATResult{}, fmt.Errorf("no STUN server responded; last error: %w", last)
}

func probeNAT(ctx context.Context, server string) (NATResult, error) {
	addr, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", serverHost(server))
	if err != nil {
		return NATResult{}, err
	}
	if len(addr) == 0 {
		return NATResult{}, errors.New("no IPv4 address")
	}
	_, port, err := net.SplitHostPort(server)
	if err != nil {
		return NATResult{}, err
	}
	target := net.JoinHostPort(addr[0].String(), port)
	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		return NATResult{}, err
	}
	defer conn.Close()
	first, err := stunQuery(ctx, conn, target, 0)
	if err != nil {
		return NATResult{}, err
	}
	result := NATResult{Server: server, Mapped: first.mapped, Mapping: "not tested", Filtering: "not tested"}
	if first.other == "" {
		return result, nil
	}
	otherHost, otherPort, err := net.SplitHostPort(first.other)
	if err != nil || !publicIPv4(otherHost) || otherPort == "0" {
		return result, nil
	}
	samePort := net.JoinHostPort(otherHost, port)
	alternate, err := stunQuery(ctx, conn, samePort, 0)
	if err == nil {
		if alternate.mapped == first.mapped {
			result.Mapping = "endpoint-independent"
		} else {
			last, err := stunQuery(ctx, conn, net.JoinHostPort(otherHost, otherPort), 0)
			if err == nil {
				if last.mapped == alternate.mapped {
					result.Mapping = "address-dependent"
				} else {
					result.Mapping = "address-and-port-dependent"
				}
			}
		}
	}
	// Use a fresh socket: contacting the alternate server would prime the NAT filter.
	filterConn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		return result, nil
	}
	defer filterConn.Close()
	if _, err = stunQuery(ctx, filterConn, target, 0); err != nil {
		return result, nil
	}
	if _, err = stunQuery(ctx, filterConn, target, 6); err == nil {
		result.Filtering = "endpoint-independent"
	} else if ctx.Err() == nil {
		if _, err = stunQuery(ctx, filterConn, target, 2); err == nil {
			result.Filtering = "address-dependent"
		} else {
			result.Filtering = "no changed-source reply"
		}
	}
	return result, nil
}

func serverHost(server string) string { host, _, _ := net.SplitHostPort(server); return host }

func stunQuery(ctx context.Context, conn net.PacketConn, target string, change uint32) (stunReply, error) {
	dest, err := net.ResolveUDPAddr("udp4", target)
	if err != nil {
		return stunReply{}, err
	}
	var tx [12]byte
	if _, err = rand.Read(tx[:]); err != nil {
		return stunReply{}, err
	}
	request := make([]byte, 20)
	binary.BigEndian.PutUint16(request, 1)
	binary.BigEndian.PutUint32(request[4:], stunMagic)
	copy(request[8:], tx[:])
	if change != 0 {
		binary.BigEndian.PutUint16(request[2:], 8)
		request = append(request, 0, 3, 0, 4, 0, 0, 0, byte(change))
	}
	deadline := time.Now().Add(1200 * time.Millisecond)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err = conn.SetDeadline(deadline); err != nil {
		return stunReply{}, err
	}
	if _, err = conn.WriteTo(request, dest); err != nil {
		return stunReply{}, err
	}
	buf := make([]byte, 1500)
	for {
		if err = ctx.Err(); err != nil {
			return stunReply{}, err
		}
		n, _, readErr := conn.ReadFrom(buf)
		if readErr != nil {
			return stunReply{}, readErr
		}
		reply, valid := parseSTUN(buf[:n], tx)
		if valid {
			return reply, nil
		}
	}
}

func parseSTUN(data []byte, tx [12]byte) (stunReply, bool) {
	if len(data) < 20 || binary.BigEndian.Uint16(data) != 0x101 || binary.BigEndian.Uint32(data[4:]) != stunMagic || string(data[8:20]) != string(tx[:]) {
		return stunReply{}, false
	}
	end := 20 + int(binary.BigEndian.Uint16(data[2:]))
	if end > len(data) {
		return stunReply{}, false
	}
	var result stunReply
	for pos := 20; pos+4 <= end; {
		kind, length := binary.BigEndian.Uint16(data[pos:]), int(binary.BigEndian.Uint16(data[pos+2:]))
		pos += 4
		if pos+length > end {
			return stunReply{}, false
		}
		value := data[pos : pos+length]
		if (kind == 0x20 || kind == 0x802c) && len(value) >= 8 && value[1] == 1 {
			port := binary.BigEndian.Uint16(value[2:])
			ip := append([]byte(nil), value[4:8]...)
			if kind == 0x20 {
				port ^= uint16(stunMagic >> 16)
				for i := range ip {
					ip[i] ^= byte((stunMagic >> (24 - 8*i)) & 0xff)
				}
			}
			address := net.JoinHostPort(net.IP(ip).String(), strconv.Itoa(int(port)))
			if kind == 0x20 {
				result.mapped = address
			} else {
				result.other = address
			}
		}
		pos += (length + 3) &^ 3
	}
	return result, result.mapped != ""
}

func publicIPv4(host string) bool {
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.Is4() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range excludedAlternates {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}
