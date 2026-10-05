package webicon

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestURLRejectsOtherOrigins(t *testing.T) {
	base, _ := url.Parse("https://app.home.example.com/")
	for _, ref := range []string{"https://elsewhere/icon", "//elsewhere/icon", "http://app.home.example.com/icon", "data:image/png,abc"} {
		if got := URL(base, base.String(), ref); got != "" {
			t.Errorf("%s resolved to %s", ref, got)
		}
	}
}

func TestGetRejectsOversizedResponse(t *testing.T) {
	client := &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 11))), Header: make(http.Header)}, nil
	})}
	if _, _, err := Get(context.Background(), client, "https://app.home.example.com/icon", 10); err == nil {
		t.Fatal("oversized icon accepted")
	}
}
