package ingress

import (
	"encoding/base64"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/brandlogo"
)

var svgSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,79}$`)

// ValidateCustomIcon accepts a bounded raster image or a theSVG catalog ID.
//
// A slug is validated by shape, not by asking the catalogue whether it holds
// one: a configuration written against an older olrd may name a mark this
// build does not carry, and that must not turn into a box whose configuration
// refuses to load. The UI draws the category's own picture when a mark is
// missing, exactly as it does when none was chosen.
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

// CustomIcon resolves an already validated choice from the binary's own
// catalogue.
//
// It used to fetch the mark from raw.githubusercontent.com. That made an icon
// on a router page depend on GitHub being reachable from the router, and it
// put an unbounded, unauthenticated network fetch behind a configuration
// field; the artwork now ships in olrd (see internal/brandlogo).
func CustomIcon(icon string) ([]byte, string, error) {
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
	data, err := brandlogo.SVG(strings.TrimPrefix(icon, "thesvg:"))
	if err != nil {
		return nil, "", err
	}
	return data, "image/svg+xml", nil
}
