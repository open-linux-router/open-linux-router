package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The CLI's connection to olrd.
//
// design.md §6.1 makes this the shape of the whole tool: everything except
// `olr daemon …` is a client of olrd's API, on equal footing with the WebUI and
// the MCP server. That is not an aesthetic preference. A module command that
// reached the system directly would be a *second* writer — with its own idea of
// the apply lock, its own copy of the validation rules, and no way to publish
// the change event the UI listens for. The lock argument alone settles it: §3.6
// mandates one global apply lock, and a lock in a process the other writer
// cannot see is not one.
//
// Only the unix socket is spoken here. §6.2 makes that the local admin path,
// authenticated by the socket's mode; TCP and its bearer token exist for
// remote clients and can be added when `olr` needs to drive a box it is not on.

// requestTimeout bounds a call. Well above the apply settle window (dhcp's
// post-apply verification watches the backend for over a second) and well below
// "hung forever", which is the state this exists to turn into a message.
const requestTimeout = 60 * time.Second

// Client talks to olrd over its control socket.
type Client struct {
	http   *http.Client
	socket string
}

// NewClient returns a client for the socket at path.
func NewClient(socket string) *Client {
	return &Client{
		socket: socket,
		http: &http.Client{
			Timeout: requestTimeout,
			Transport: &http.Transport{
				// The host in the URL is a placeholder — every connection goes
				// to the same socket regardless of what it says.
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socket)
				},
			},
		},
	}
}

// ClientFor builds a client from the command's --socket flag.
func ClientFor(cmd *cobra.Command) *Client { return NewClient(Socket(cmd)) }

// APIError is a response olrd refused.
//
// It carries the addressed problems as well as the message, so that a field the
// operator got wrong is reported by path — the same way the WebUI shows it, and
// from the same bytes, rather than from a second implementation of the rules.
type APIError struct {
	Status   int
	Message  string
	Problems []core.Problem
}

func (e *APIError) Error() string {
	if len(e.Problems) == 0 {
		return e.Message
	}
	var b strings.Builder
	b.WriteString(e.Message)
	for _, p := range e.Problems {
		b.WriteString("\n  ")
		if p.Path != "" {
			b.WriteString(p.Path)
			b.WriteString(": ")
		}
		b.WriteString(p.Message)
	}
	return b.String()
}

// Get reads a resource into out.
func (c *Client) Get(ctx context.Context, path string, out any) error {
	return c.Do(ctx, http.MethodGet, path, nil, out)
}

// Put replaces a resource.
func (c *Client) Put(ctx context.Context, path string, body, out any) error {
	return c.Do(ctx, http.MethodPut, path, body, out)
}

// Post sends a body that is not a replacement — a dry run, mostly.
func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	return c.Do(ctx, http.MethodPost, path, body, out)
}

// Do sends one request.
//
// On a non-2xx it returns an *APIError — and still decodes the body into out
// when it can. That is deliberate and not merely lenient: design.md §5.3.2 has
// no rollback, so a failed apply answers with the steps that did land, and
// throwing that away would leave the operator to guess what state the box is
// in. The caller gets both the failure and the partial result.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding the request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, "http://olr"+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return c.dialError(err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, core.MaxBodyBytes))
	if err != nil {
		return fmt.Errorf("reading the response: %w", err)
	}

	if out != nil && len(bytes.TrimSpace(data)) > 0 {
		// A decode failure is only worth reporting when the request otherwise
		// succeeded; on an error response the envelope below is the better
		// answer and this body was never going to fit out anyway.
		if err := json.Unmarshal(data, out); err != nil && resp.StatusCode < 300 {
			return fmt.Errorf("decoding the response: %w", err)
		}
	}

	if resp.StatusCode >= 300 {
		return apiError(resp.StatusCode, data)
	}
	return nil
}

// apiError unwraps olrd's error envelope, falling back to the status line for a
// response that is not one of ours.
func apiError(status int, data []byte) error {
	var envelope struct {
		Error core.ErrorBody `json:"error"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil && envelope.Error.Message != "" {
		return &APIError{
			Status:   status,
			Message:  envelope.Error.Message,
			Problems: envelope.Error.Problems,
		}
	}
	return &APIError{Status: status, Message: strings.TrimSpace(string(data))}
}

// dialError turns a failed connection into the sentence the operator needs.
//
// Without this the message is a raw syscall error naming a path, which says
// nothing about the daemon or how to start it — and "olrd is not running" is
// far and away the most common reason to land here.
func (c *Client) dialError(err error) error {
	var opErr *net.OpError
	if errors.As(err, &opErr) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf(
			"cannot reach olrd on %s: %w\n"+
				"Check it is running with `olr daemon status`, and start it with `olr daemon start`",
			c.socket, err)
	}
	return err
}
