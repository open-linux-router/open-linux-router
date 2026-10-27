package tools

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

const stunMagic uint32 = 0x2112a442

// Every server is asked at once, so the list is ordered by what a server can
// tell us, not by whether it is likely to answer. Only a server that offers a
// second address for the same socket can classify mapping or filtering; Google
// and Cloudflare answer reliably but offer none, so they fall back to supplying
// the observed mapping alone.
//
// The first three were checked to reply from a different address for their
// alternate endpoint. stun.miwifi.com keeps the test useful when the others are
// blocked at the default route, which is common on mainland Chinese networks.
var stunServers = []string{
	"stun.miwifi.com:3478",
	"stun.1und1.de:3478",
	"stun.voipgate.com:3478",
	"stun1.l.google.com:19302",
	"stun.cloudflare.com:3478",
}

const (
	// stunQueryWait bounds one request. Classification costs several round
	// trips, and waiting is cheaper than reporting an unknown that would have
	// been answerable a second later.
	stunQueryWait = 3 * time.Second
	// CHANGE-REQUEST flags (RFC 5780 §7.2): ask for the reply from the server's
	// alternate port, or from its alternate address.
	changePort    = 0x02
	changeAddress = 0x04
)

// The values the mapping and filtering fields can hold. "not tested" means no
// observation was made; "no changed-source reply" means one was requested and
// nothing arrived, which the page reports without drawing a conclusion.
const (
	natNotTested       = "not tested"
	natNoChangedSource = "no changed-source reply"
)

type NATResult struct {
	Server    string `json:"server"`
	Mapped    string `json:"mapped"`
	Mapping   string `json:"mapping"`
	Filtering string `json:"filtering"`
	// Alternate reports whether the answering server offered a second address,
	// which is what classification needs. It separates "this server cannot
	// classify" from "the classification replies never arrived".
	Alternate bool `json:"alternate"`
}

type stunReply struct{ mapped, other string }

// stunProbe is what one parallel round learned from one server.
type stunProbe struct {
	server string
	target string
	mapped string
	other  string
}

var excludedAlternates = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

func runNAT(ctx context.Context) (NATResult, error) {
	return classifyProbes(ctx, collectProbes(ctx), classifyNAT)
}

// collectProbes queries every server at once. Asking them together is what
// makes a longer list cheap: one unanswered server no longer costs the whole
// test, and a server that cannot classify cannot win merely by answering first.
func collectProbes(ctx context.Context) []stunProbe {
	probes := probeServers(ctx, stunServers)
	if hasMapping(probes) || ctx.Err() != nil {
		return probes
	}
	// A single round can lose every reply to a burst of loss. The request is
	// small enough to repeat before calling the network unavailable.
	return probeServers(ctx, stunServers)
}

func probeServers(ctx context.Context, servers []string) []stunProbe {
	probes := make([]stunProbe, len(servers))
	var wg sync.WaitGroup
	for i, server := range servers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			probes[i] = probeServer(ctx, server)
		}()
	}
	wg.Wait()
	return probes
}

func probeServer(ctx context.Context, server string) stunProbe {
	probe := stunProbe{server: server}
	host, port, err := net.SplitHostPort(server)
	if err != nil {
		return probe
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
	if err != nil || len(addrs) == 0 {
		return probe
	}
	probe.target = net.JoinHostPort(addrs[0].String(), port)
	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		return probe
	}
	defer conn.Close()
	reply, err := stunQuery(ctx, conn, probe.target, 0)
	if err != nil {
		return probe
	}
	probe.mapped, probe.other = reply.mapped, reply.other
	return probe
}

func hasMapping(probes []stunProbe) bool {
	for _, probe := range probes {
		if probe.mapped != "" {
			return true
		}
	}
	return false
}

// classifyProbes turns the collected observations into one answer, preferring a
// server that can classify over one that can only report the mapping.
func classifyProbes(ctx context.Context, probes []stunProbe, classify func(context.Context, stunProbe) (string, string)) (NATResult, error) {
	var best NATResult
	found := false
	for _, probe := range probes {
		if probe.mapped == "" {
			continue
		}
		result := NATResult{
			Server:    probe.server,
			Mapped:    probe.mapped,
			Mapping:   natNotTested,
			Filtering: natNotTested,
			Alternate: alternateUsable(probe),
		}
		if result.Alternate {
			result.Mapping, result.Filtering = classify(ctx, probe)
			result.Mapping = orNotTested(result.Mapping)
			result.Filtering = orNotTested(result.Filtering)
		}
		if !found || betterResult(result, best) {
			best, found = result, true
		}
		if classified(best) == 2 || ctx.Err() != nil {
			break
		}
	}
	if !found {
		return NATResult{}, errors.New("no STUN server responded")
	}
	return best, nil
}

// betterResult keeps the answer with more classifications, and prefers a server
// that could classify when neither observation settled anything, so an
// unhelpful server does not hide the address one that could have helped saw.
func betterResult(candidate, best NATResult) bool {
	if classified(candidate) != classified(best) {
		return classified(candidate) > classified(best)
	}
	return candidate.Alternate && !best.Alternate
}

func classified(result NATResult) int {
	count := 0
	if result.Mapping != natNotTested {
		count++
	}
	if result.Filtering != natNotTested {
		count++
	}
	return count
}

func orNotTested(value string) string {
	if value == "" {
		return natNotTested
	}
	return value
}

// alternateUsable reports whether a server's second address can support the
// classification probes. Some public servers answer from the same address on a
// different port; probing that would show an unchanged mapping and label every
// NAT endpoint-independent, so it is not usable.
func alternateUsable(probe stunProbe) bool {
	otherHost, otherPort, err := net.SplitHostPort(probe.other)
	if err != nil || otherPort == "0" {
		return false
	}
	primaryHost, _, err := net.SplitHostPort(probe.target)
	if err != nil || otherHost == primaryHost {
		return false
	}
	return publicIPv4(otherHost)
}

// classifyNAT runs both classification probes at once. They use separate
// sockets: contacting the alternate address would prime the filter that the
// filtering test is meant to observe.
func classifyNAT(ctx context.Context, probe stunProbe) (mapping, filtering string) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); mapping = probeMapping(ctx, probe) }()
	go func() { defer wg.Done(); filtering = probeFiltering(ctx, probe) }()
	wg.Wait()
	return mapping, filtering
}

// probeMapping compares the mapping the NAT keeps while one socket contacts two
// destinations: the server's own port on another address, then another port on
// that address.
func probeMapping(ctx context.Context, probe stunProbe) string {
	_, port, err := net.SplitHostPort(probe.target)
	if err != nil {
		return natNotTested
	}
	otherHost, otherPort, err := net.SplitHostPort(probe.other)
	if err != nil {
		return natNotTested
	}
	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		return natNotTested
	}
	defer conn.Close()
	primary, err := stunQuery(ctx, conn, probe.target, 0)
	if err != nil {
		return natNotTested
	}
	alternate, err := stunQuery(ctx, conn, net.JoinHostPort(otherHost, port), 0)
	if err != nil {
		return natNotTested
	}
	if alternate.mapped == primary.mapped {
		return "endpoint-independent"
	}
	last, err := stunQuery(ctx, conn, net.JoinHostPort(otherHost, otherPort), 0)
	if err != nil {
		return natNotTested
	}
	if last.mapped == alternate.mapped {
		return "address-dependent"
	}
	return "address-and-port-dependent"
}

// probeFiltering asks the server to answer from an address and port the socket
// has not contacted, which only gets through if the NAT does not filter by
// source.
func probeFiltering(ctx context.Context, probe stunProbe) string {
	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		return natNotTested
	}
	defer conn.Close()
	if _, err = stunQuery(ctx, conn, probe.target, 0); err != nil {
		return natNotTested
	}
	if _, err = stunQuery(ctx, conn, probe.target, changeAddress|changePort); err == nil {
		return "endpoint-independent"
	}
	if ctx.Err() != nil {
		return natNotTested
	}
	if _, err = stunQuery(ctx, conn, probe.target, changePort); err == nil {
		return "address-dependent"
	}
	if ctx.Err() != nil {
		return natNotTested
	}
	return natNoChangedSource
}

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
	deadline := time.Now().Add(stunQueryWait)
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
