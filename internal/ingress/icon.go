package ingress

import (
	"context"
	"net/http"

	"github.com/open-linux-router/open-linux-router/internal/webicon"
)

func serviceIcon(ctx context.Context, client *http.Client, origin string) ([]byte, string, error) {
	return webicon.Discover(ctx, client, origin)
}
