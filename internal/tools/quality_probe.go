package tools

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sync"
	"syscall"
	"time"
)

func qualityMS(d time.Duration) *float64 {
	v := float64(d) / float64(time.Millisecond)
	return &v
}

func probeQuality(ctx context.Context, target qualityTarget, family string) QualitySample {
	return probeQualityTLS(ctx, target, family, nil)
}

// tlsConfig is only supplied by controlled TLS-server tests. Production uses
// ordinary certificate verification and ignores environment proxies.
func probeQualityTLS(ctx context.Context, target qualityTarget, family string, tlsConfig *tls.Config) QualitySample {
	sample := QualitySample{StartedAt: time.Now().UTC(), State: "failed", Stage: "dns"}
	var mu sync.Mutex
	finish := func(err error) QualitySample {
		mu.Lock()
		defer mu.Unlock()
		sample.CompletedAt = time.Now().UTC()
		if err != nil {
			sample.Error = err.Error()
			for _, local := range []error{syscall.EACCES, syscall.EPERM, syscall.EMFILE, syscall.ENFILE, syscall.ENOMEM, syscall.ENOBUFS} {
				if errors.Is(err, local) {
					sample.State = "error"
				}
			}
			if ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled) {
				sample.State = "cancelled"
			}
		}
		return sample
	}
	u, err := url.Parse(target.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		sample.State = "error"
		return finish(errors.New("invalid HTTPS test resource"))
	}
	network, ipNetwork := "tcp4", "ip4"
	if family == "ipv6" {
		network, ipNetwork = "tcp6", "ip6"
	}
	dnsStart := time.Now()
	addresses, err := net.DefaultResolver.LookupIP(ctx, ipNetwork, u.Hostname())
	if err != nil {
		return finish(err)
	}
	sample.DNS = qualityMS(time.Since(dnsStart))
	if len(addresses) == 0 {
		return finish(errors.New("no address for this IP family"))
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	address := net.JoinHostPort(addresses[0].String(), port)
	sample.Address = address
	sample.Stage = "connect"
	transport := &http.Transport{
		Proxy: nil, DisableKeepAlives: true, ForceAttemptHTTP2: false,
		TLSClientConfig: tlsConfig, MaxResponseHeaderBytes: 32 << 10,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			start := time.Now()
			conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
			if err == nil {
				mu.Lock()
				sample.TCP = qualityMS(time.Since(start))
				mu.Unlock()
			}
			return conn, err
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var requestStart time.Time
	trace := &httptrace.ClientTrace{
		TLSHandshakeStart: func() { mu.Lock(); sample.Stage = "tls"; mu.Unlock() },
		GotConn: func(httptrace.GotConnInfo) {
			mu.Lock()
			sample.Stage = "response"
			requestStart = time.Now()
			mu.Unlock()
		},
		GotFirstResponseByte: func() {
			mu.Lock()
			defer mu.Unlock()
			if !requestStart.IsZero() {
				sample.Response = qualityMS(time.Since(requestStart))
			}
		},
	}
	request, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodHead, target.URL, nil)
	if err != nil {
		sample.State = "error"
		return finish(err)
	}
	request.Header.Set("User-Agent", "open-linux-router/quality-v1")
	response, err := client.Do(request)
	if err != nil {
		return finish(err)
	}
	response.Body.Close()
	mu.Lock()
	sample.Status = response.StatusCode
	if response.StatusCode != target.Status {
		// Rate limits, challenges and changed resources cannot establish a
		// network capability failure. Keep timing evidence but not access points.
		sample.State, sample.Stage = "inconclusive", "expected-response"
		mu.Unlock()
		return finish(errors.New("test resource returned an unexpected HTTP status"))
	}
	sample.State, sample.Stage = "ok", "complete"
	mu.Unlock()
	return finish(nil)
}
