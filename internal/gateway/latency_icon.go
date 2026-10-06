package gateway

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/ingress"
)

var svgSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,79}$`)

func validateLatencyIcon(icon string) error {
	if icon == "" {
		return nil
	}
	if strings.HasPrefix(icon, "thesvg:") {
		if svgSlug.MatchString(strings.TrimPrefix(icon, "thesvg:")) {
			return nil
		}
		return errors.New("invalid theSVG icon ID")
	}
	for _, mime := range []string{"image/png", "image/jpeg", "image/webp"} {
		prefix := "data:" + mime + ";base64,"
		if strings.HasPrefix(icon, prefix) {
			if len(icon) > 130000 {
				return errors.New("icon exceeds 96 KiB")
			}
			data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(icon, prefix))
			if err != nil || len(data) == 0 || len(data) > 96<<10 || http.DetectContentType(data) != mime {
				return errors.New("invalid image data")
			}
			return nil
		}
	}
	return errors.New("use a PNG, JPEG, WebP or theSVG icon")
}

type latencyIcon struct {
	data    []byte
	kind    string
	checked time.Time
}

// Icon discovers a monitored site's favicon through the same selected exit.
// The result is cached independently of probe measurements.
func (m *CustomLatencyMonitor) Icon(ctx context.Context, name string) ([]byte, string, error) {
	m.mu.RLock()
	var site CustomLatencySite
	for _, candidate := range m.sites {
		if candidate.Name == name {
			site = candidate
			break
		}
	}
	cached, ok := m.icons[name]
	m.mu.RUnlock()
	if site.Name == "" {
		return nil, "", errors.New("site not found")
	}
	if strings.HasPrefix(site.Icon, "data:") {
		kind := strings.SplitN(strings.TrimPrefix(site.Icon, "data:"), ";", 2)[0]
		data, _ := base64.StdEncoding.DecodeString(strings.SplitN(site.Icon, ",", 2)[1])
		return data, kind, nil
	}
	if ok && time.Since(cached.checked) < 24*time.Hour && !strings.HasPrefix(site.Icon, "data:") {
		return cached.data, cached.kind, nil
	}
	if m.iconClient != nil {
		client, err := m.iconClient(&site)
		if err != nil {
			return nil, "", err
		}
		return m.discoverIcon(ctx, site, client)
	}
	if strings.HasPrefix(site.Icon, "thesvg:") {
		slug := strings.TrimPrefix(site.Icon, "thesvg:")
		ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://raw.githubusercontent.com/GLINCKER/thesvg/main/public/icons/"+slug+"/default.svg", nil)
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		response, err := client.Do(request)
		if err != nil {
			return nil, "", err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, "", fmt.Errorf("theSVG icon unavailable: HTTP %d", response.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, 64<<10+1))
		if err != nil || len(data) > 64<<10 || !safeTheSVG(data) {
			return nil, "", errors.New("invalid theSVG response")
		}
		m.mu.Lock()
		if m.icons == nil {
			m.icons = map[string]latencyIcon{}
		}
		if current := m.siteByName(name); current == site {
			m.icons[name] = latencyIcon{data: data, kind: "image/svg+xml", checked: time.Now()}
		}
		m.mu.Unlock()
		return data, "image/svg+xml", nil
	}
	transport := &http.Transport{DisableKeepAlives: true, TLSHandshakeTimeout: 3 * time.Second}
	defer transport.CloseIdleConnections()
	if site.Exit != "" {
		if m.Resolve == nil {
			return nil, "", errors.New("gateway exit unavailable")
		}
		mark, err := m.Resolve(site.Exit)
		if err != nil {
			return nil, "", err
		}
		transport.DialContext = markedLatencyDial(mark)
	}
	return m.discoverIcon(ctx, site, &http.Client{Transport: transport})
}

func (m *CustomLatencyMonitor) discoverIcon(ctx context.Context, site CustomLatencySite, client *http.Client) ([]byte, string, error) {
	target, _ := url.Parse(site.URL)
	target.RawQuery, target.Fragment = "", ""
	data, kind, err := ingress.DiscoverIcon(ctx, client, target.String())
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, current := range m.sites {
		if current == site {
			if err == nil {
				if m.icons == nil {
					m.icons = map[string]latencyIcon{}
				}
				m.icons[site.Name] = latencyIcon{data: data, kind: kind, checked: time.Now()}
			}
			break
		}
	}
	return data, kind, err
}

// siteByName is called with m.mu held.
func (m *CustomLatencyMonitor) siteByName(name string) CustomLatencySite {
	for _, site := range m.sites {
		if site.Name == name {
			return site
		}
	}
	return CustomLatencySite{}
}

func safeTheSVG(data []byte) bool {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	root := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return root
		}
		if err != nil {
			return false
		}
		switch element := token.(type) {
		case xml.StartElement:
			if !root {
				if element.Name.Local != "svg" {
					return false
				}
				root = true
			}
			if strings.EqualFold(element.Name.Local, "script") || strings.EqualFold(element.Name.Local, "foreignObject") {
				return false
			}
			for _, attr := range element.Attr {
				key := strings.ToLower(attr.Name.Local)
				value := strings.ToLower(strings.TrimSpace(attr.Value))
				if strings.HasPrefix(key, "on") || (key == "href" && !strings.HasPrefix(value, "#")) || strings.Contains(value, "url(") && !strings.Contains(value, "url(#") {
					return false
				}
			}
		case xml.Directive, xml.ProcInst:
			return false
		}
	}
}
