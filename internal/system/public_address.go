package system

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const (
	ipv4Endpoint = "https://api-ipv4.ip.sb/ip"
	ipv6Endpoint = "https://api-ipv6.ip.sb/ip"
)

// PublicAddresses are independently observed from the router's default route.
// An unavailable address is empty rather than failing the other family.
type PublicAddresses struct {
	IPv4 string `json:"ipv4"`
	IPv6 string `json:"ipv6"`
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

func readPublicAddress(ctx context.Context, client *http.Client, endpoint string, ipv4 bool) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "open-linux-router")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return ""
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(string(body)))
	if err != nil || addr.Is4() != ipv4 || !addr.IsGlobalUnicast() {
		return ""
	}
	return addr.String()
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
	v4 := make(chan string, 1)
	v6 := make(chan string, 1)
	go func() { v4 <- readPublicAddress(r.Context(), ipv4, ipv4Endpoint, true) }()
	go func() { v6 <- readPublicAddress(r.Context(), ipv6, ipv6Endpoint, false) }()
	core.WriteJSON(w, http.StatusOK, PublicAddresses{IPv4: <-v4, IPv6: <-v6})
}
