package ingress

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const iconPageLimit = 256 << 10
const iconManifestLimit = 64 << 10
const iconImageLimit = 1 << 20

type iconCandidate struct {
	url      string
	size     int
	priority int
}

// serviceIcon only visits the published HTTPS origin; neither redirects nor
// absolute manifest URLs can turn this into an arbitrary network fetch.
func serviceIcon(ctx context.Context, client *http.Client, origin string) ([]byte, string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	copyClient := *client
	copyClient.Timeout = 5 * time.Second
	copyClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" || !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
			return http.ErrUseLastResponse
		}
		return nil
	}
	base, err := url.Parse(origin)
	if err != nil {
		return nil, "", err
	}
	if base.Path == "" {
		base.Path = "/"
	}
	origin = base.String()
	page, finalURL, err := iconGet(ctx, &copyClient, origin, iconPageLimit)
	if err != nil {
		page = nil
	}
	pageBase := base
	if err == nil {
		pageBase, _ = url.Parse(finalURL)
	}
	candidates, manifest := pageIcons(page, pageBase)
	if manifest != "" {
		data, _, err := iconGet(ctx, &copyClient, manifest, iconManifestLimit)
		if err == nil {
			var m struct {
				Icons []struct{ Src, Sizes, Type string }
			}
			if json.Unmarshal(data, &m) == nil {
				for _, icon := range m.Icons {
					if u := iconURL(base, manifest, icon.Src); u != "" {
						candidates = append(candidates, iconCandidate{u, iconSize(icon.Sizes), 3})
					}
				}
			}
		}
	}
	if u := iconURL(base, base.String(), "/favicon.ico"); u != "" {
		candidates = append(candidates, iconCandidate{u, 0, 0})
	}
	slices.SortStableFunc(candidates, func(a, b iconCandidate) int {
		if a.size != b.size {
			return b.size - a.size
		}
		return b.priority - a.priority
	})
	seen := map[string]bool{}
	for _, candidate := range candidates[:min(len(candidates), 16)] {
		if seen[candidate.url] {
			continue
		}
		seen[candidate.url] = true
		data, _, err := iconGet(ctx, &copyClient, candidate.url, iconImageLimit)
		if err != nil {
			continue
		}
		kind := http.DetectContentType(data)
		switch kind {
		case "image/png", "image/jpeg", "image/webp", "image/gif", "image/vnd.microsoft.icon", "image/x-icon":
			return data, kind, nil
		}
	}
	return nil, "", errors.New("no usable icon")
}

func iconGet(ctx context.Context, client *http.Client, address string, limit int64) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", errors.New("icon resource unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || len(data) > int(limit) {
		return nil, "", errors.New("icon resource too large or unreadable")
	}
	finalURL := address
	if resp.Request != nil {
		finalURL = resp.Request.URL.String()
	}
	return data, finalURL, nil
}

func pageIcons(page []byte, base *url.URL) ([]iconCandidate, string) {
	doc, err := html.Parse(strings.NewReader(string(page)))
	if err != nil {
		return nil, ""
	}
	var icons []iconCandidate
	var manifest string
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "link" {
			attr := map[string]string{}
			for _, a := range n.Attr {
				attr[strings.ToLower(a.Key)] = a.Val
			}
			rel := strings.Fields(strings.ToLower(attr["rel"]))
			if slices.Contains(rel, "manifest") {
				manifest = iconURL(base, base.String(), attr["href"])
			}
			if slices.Contains(rel, "icon") || slices.Contains(rel, "apple-touch-icon") {
				if u := iconURL(base, base.String(), attr["href"]); u != "" {
					priority := 1
					if slices.Contains(rel, "apple-touch-icon") {
						priority = 2
					}
					icons = append(icons, iconCandidate{u, iconSize(attr["sizes"]), priority})
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(doc)
	return icons, manifest
}

func iconURL(origin *url.URL, parent, ref string) string {
	if ref == "" {
		return ""
	}
	p, err := url.Parse(parent)
	if err != nil {
		return ""
	}
	relative, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	resolved := p.ResolveReference(relative)
	if resolved.Scheme != origin.Scheme || !strings.EqualFold(resolved.Host, origin.Host) || resolved.User != nil {
		return ""
	}
	resolved.Fragment = ""
	return resolved.String()
}

func iconSize(sizes string) int {
	best := 0
	for _, size := range strings.Fields(sizes) {
		parts := strings.Split(strings.ToLower(size), "x")
		if len(parts) != 2 {
			continue
		}
		w, e1 := strconv.Atoi(parts[0])
		h, e2 := strconv.Atoi(parts[1])
		if e1 == nil && e2 == nil && w > 0 && h > 0 && w <= 4096 && h <= 4096 {
			best = max(best, min(w, h))
		}
	}
	return best
}
