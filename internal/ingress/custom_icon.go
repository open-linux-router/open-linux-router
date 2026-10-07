package ingress

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var svgSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,79}$`)

// ValidateCustomIcon accepts a bounded raster image or a theSVG catalog ID.
func ValidateCustomIcon(icon string) error {
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

// CustomIcon resolves an already validated choice, without following redirects.
func CustomIcon(ctx context.Context, icon string) ([]byte, string, error) {
	if icon == "" {
		return nil, "", errors.New("no custom icon")
	}
	if err := ValidateCustomIcon(icon); err != nil {
		return nil, "", err
	}
	if strings.HasPrefix(icon, "data:") {
		kind := strings.SplitN(strings.TrimPrefix(icon, "data:"), ";", 2)[0]
		data, _ := base64.StdEncoding.DecodeString(strings.SplitN(icon, ",", 2)[1])
		return data, kind, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://raw.githubusercontent.com/GLINCKER/thesvg/main/public/icons/"+strings.TrimPrefix(icon, "thesvg:")+"/default.svg", nil)
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
	if err != nil || len(data) > 64<<10 || !SafeTheSVG(data) {
		return nil, "", errors.New("invalid theSVG response")
	}
	return data, "image/svg+xml", nil
}

func SafeTheSVG(data []byte) bool {
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
