package core

import (
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
)

// BrowserGuard runs only on TCP. Checking Host against this machine's actual
// addresses prevents DNS rebinding even when the attacker controls Origin.
// Reverse proxies and custom DNS names must be explicitly allowed.
func BrowserGuard(next http.Handler, allowedHosts []string) http.Handler {
	hostname, _ := os.Hostname()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if parsed, _, err := net.SplitHostPort(host); err == nil {
			host = parsed
		}
		host = strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
		allowed := host != "" && (host == strings.ToLower(hostname) || host == "localhost")
		for _, name := range allowedHosts {
			if host == strings.ToLower(strings.TrimSuffix(name, ".")) {
				allowed = true
			}
		}
		if addr, err := netip.ParseAddr(host); err == nil {
			if local, ok := r.Context().Value(http.LocalAddrContextKey).(*net.TCPAddr); ok {
				localIP, _ := netip.AddrFromSlice(local.IP)
				allowed = addr.Unmap() == localIP.Unmap()
			}
		}
		if !allowed {
			WriteError(w, 403, "request Host is not a configured router address")
			return
		}
		isAPI := r.URL.Path == APIPrefix || strings.HasPrefix(r.URL.Path, APIPrefix+"/")
		if isAPI {
			if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
				WriteError(w, 403, "cross-site API requests are not allowed")
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				scheme := "http"
				if r.TLS != nil {
					scheme = "https"
				}
				u, err := url.Parse(origin)
				if err != nil || u.Scheme != scheme || !strings.EqualFold(u.Host, r.Host) || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
					WriteError(w, 403, "API requests must come from this router's own origin")
					return
				}
			}
			if r.Method != "GET" && r.Method != "HEAD" && (r.ContentLength != 0 || len(r.TransferEncoding) != 0) {
				media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if err != nil || media != "application/json" {
					WriteError(w, 415, "API request bodies require application/json")
					return
				}
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}
