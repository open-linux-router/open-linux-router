package system

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const (
	ipv4Endpoint = "https://api-ipv4.ip.sb/geoip"
	ipv6Endpoint = "https://api-ipv6.ip.sb/geoip"
)

// PublicAddress is what ip.sb sees from the router, plus optional location
// and network ownership information. Empty IP means this family is unavailable.
type PublicAddress struct {
	IP           string `json:"ip"`
	Country      string `json:"country,omitempty"`
	Region       string `json:"region,omitempty"`
	City         string `json:"city,omitempty"`
	ISP          string `json:"isp,omitempty"`
	Organization string `json:"organization,omitempty"`
	ASN          int    `json:"asn,omitempty"`
}

type PublicAddresses struct {
	IPv4 PublicAddress `json:"ipv4"`
	IPv6 PublicAddress `json:"ipv6"`
}

func publicAddressClient(network string) *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, addr)
			},
		},
	}
}

func readPublicAddress(ctx context.Context, client *http.Client, endpoint string, ipv4 bool) PublicAddress {
	var out PublicAddress
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return out
	}
	req.Header.Set("User-Agent", "open-linux-router")
	resp, err := client.Do(req)
	if err != nil {
		return out
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&out); err != nil {
		return PublicAddress{}
	}
	addr, err := netip.ParseAddr(out.IP)
	if err != nil || addr.Is4() != ipv4 || !addr.IsGlobalUnicast() {
		return PublicAddress{}
	}
	out.IP = addr.String()
	return out
}

func (h HTTP) getPublicAddresses(w http.ResponseWriter, r *http.Request) {
	ipv4 := h.PublicIPv4Client
	if ipv4 == nil {
		ipv4 = publicAddressClient("tcp4")
	}
	ipv6 := h.PublicIPv6Client
	if ipv6 == nil {
		ipv6 = publicAddressClient("tcp6")
	}
	v4 := make(chan PublicAddress, 1)
	v6 := make(chan PublicAddress, 1)
	go func() { v4 <- readPublicAddress(r.Context(), ipv4, ipv4Endpoint, true) }()
	go func() { v6 <- readPublicAddress(r.Context(), ipv6, ipv6Endpoint, false) }()
	core.WriteJSON(w, http.StatusOK, PublicAddresses{IPv4: <-v4, IPv6: <-v6})
}
