package core

import (
	"mime"
	"net/http"
	"strings"
)

// BrowserGuard rejects cross-site fetches and non-JSON API bodies on TCP.
// Host and Origin are not restricted, so custom domains and HTTPS reverse
// proxies work without separate daemon configuration.
func BrowserGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isAPI := r.URL.Path == APIPrefix || strings.HasPrefix(r.URL.Path, APIPrefix+"/")
		if isAPI {
			if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
				WriteError(w, 403, "cross-site API requests are not allowed")
				return
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
