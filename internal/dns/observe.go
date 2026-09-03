package dns

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/dnsrelay"
)

// The module's window onto what the relay saw.
//
// design.md §4.5: the model and the query interface are ours, always —
// including for observed things. So the relay's wire types die here, at the
// module boundary, and nothing above this file sees a dnsrelay.Query. The
// conversion is dull on purpose; it is the same discipline that keeps "column 4
// of the lease file" out of internal/dhcp's callers.
//
// Nothing here is stored or cached. Every read goes to the relay, which is what
// lets `kill -9 olrd` change no answer the API gives (design.md §10).

// ObserveTimeout bounds a read from the relay.
//
// Short, and deliberately so: this is a status query behind an operator waiting
// on a page to load, and a relay that is wedged should produce "could not read"
// quickly rather than hanging the request. Resolution itself is unaffected
// either way — the relay answers queries on a different path from this one.
const ObserveTimeout = 2 * time.Second

// Query is one answered query, as this module models it.
type Query struct {
	At      time.Time
	Client  netip.Addr
	Name    string
	Type    string
	Rcode   string
	Blocked bool
	Policy  string
	Answers []netip.Addr
	Chain   []string
}

// Name is one domain→address pairing the relay observed.
type Name struct {
	Client   netip.Addr
	Name     string
	Addr     netip.Addr
	Chain    []string
	Expires  time.Time
	LastSeen time.Time
}

// Stats is the relay's account of itself, including the gaps.
type Stats struct {
	Since    time.Time
	Queries  uint64
	Blocked  uint64
	Refused  uint64
	Failed   uint64
	Dropped  uint64
	Unparsed uint64
	Held     int
	Capacity int
	Clients  []Client
}

// ObserveView is the read-only interface onto a running relay.
//
// Declared as an interface for the same reason internal/dhcp declares LinkView:
// so the module can be tested without one, and so a plan against a box whose
// relay is stopped is an ordinary case rather than a failure.
type ObserveView interface {
	// Queries returns the recent query log, newest first.
	Queries(ctx context.Context) ([]Query, Stats, error)

	// Names returns the domain→address map.
	Names(ctx context.Context) ([]Name, Stats, error)

	// Clients returns who has been resolving through us, which is what tells a
	// harmless access-control change from one that cuts somebody off.
	Clients(ctx context.Context) ([]Client, error)
}

// SocketObserver reads the relay over its unix socket.
type SocketObserver struct {
	// Path is the socket the relay serves on.
	Path string

	// Timeout bounds one read. Zero means ObserveTimeout.
	Timeout time.Duration
}

// NewObserver returns an observer for the given socket path.
func NewObserver(path string) SocketObserver { return SocketObserver{Path: path} }

func (o SocketObserver) timeout() time.Duration {
	if o.Timeout <= 0 {
		return ObserveTimeout
	}
	return o.Timeout
}

// client builds an HTTP client that dials the unix socket.
//
// The host in the URL is a placeholder — the transport ignores it entirely —
// but it has to be *something*, or net/http refuses to build the request.
func (o SocketObserver) client() *http.Client {
	return &http.Client{
		Timeout: o.timeout(),
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", o.Path)
			},
		},
	}
}

func (o SocketObserver) get(ctx context.Context, path string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://relay"+path, nil)
	if err != nil {
		return err
	}
	resp, err := o.client().Do(req)
	if err != nil {
		// Named plainly, because the common cause is benign: the relay is not
		// running. A caller turns this into "could not read" on its own line
		// rather than failing the whole status reply.
		return fmt.Errorf("reading %s from the relay at %s: %w", path, o.Path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the relay answered %s with %s", path, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return fmt.Errorf("parsing the relay's %s response: %w", path, err)
	}
	return nil
}

// Queries implements ObserveView.
func (o SocketObserver) Queries(ctx context.Context) ([]Query, Stats, error) {
	var body dnsrelay.QueriesResponse
	if err := o.get(ctx, "/queries", &body); err != nil {
		return nil, Stats{}, err
	}
	out := make([]Query, 0, len(body.Queries))
	for _, q := range body.Queries {
		out = append(out, Query{
			At: q.At, Client: q.Client, Name: q.Name, Type: q.Type, Rcode: q.Rcode,
			Blocked: q.Blocked, Policy: q.Policy, Answers: q.Answers, Chain: q.Chain,
		})
	}
	return out, viewStats(body.Stats), nil
}

// Names implements ObserveView.
func (o SocketObserver) Names(ctx context.Context) ([]Name, Stats, error) {
	var body dnsrelay.NamesResponse
	if err := o.get(ctx, "/names", &body); err != nil {
		return nil, Stats{}, err
	}
	out := make([]Name, 0, len(body.Names))
	for _, n := range body.Names {
		out = append(out, Name{
			Client: n.Client, Name: n.Name, Addr: n.Addr, Chain: n.Chain,
			Expires: n.Expires, LastSeen: n.LastSeen,
		})
	}
	return out, viewStats(body.Stats), nil
}

// Clients implements ObserveView.
func (o SocketObserver) Clients(ctx context.Context) ([]Client, error) {
	var body dnsrelay.Stats
	if err := o.get(ctx, "/stats", &body); err != nil {
		return nil, err
	}
	return viewClients(body.Clients), nil
}

func viewStats(s dnsrelay.Stats) Stats {
	return Stats{
		Since: s.Since, Queries: s.Queries, Blocked: s.Blocked, Refused: s.Refused,
		Failed: s.Failed, Dropped: s.Dropped, Unparsed: s.Unparsed,
		Held: s.Held, Capacity: s.Capacity, Clients: viewClients(s.Clients),
	}
}

func viewClients(in []dnsrelay.Client) []Client {
	if len(in) == 0 {
		return nil
	}
	out := make([]Client, 0, len(in))
	for _, c := range in {
		out = append(out, Client{Addr: c.Addr, Queries: int(c.Queries), LastSeen: c.LastSeen})
	}
	return out
}
