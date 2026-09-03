package dnsrelay

import (
	"net/netip"
	"time"
)

// What the relay saw, as it is served over the observation socket.
//
// This is the *read* direction, and it is the one place this backend is not
// driven by a file. design.md §3.5's corollary — drive our own backend the way
// we drive dnsmasq, not over a private RPC channel — is about configuration,
// and it names the two costs to avoid: the backend stops being independently
// runnable, and the escape hatch stops working for it. A read-only socket costs
// neither. The relay still starts from its rendered files with nothing else
// alive on the box, and `curl --unix-socket` is a better debugging story than
// dnsmasq's lease file, not a worse one.
//
// It is a socket rather than a state file because the alternative is writing a
// line per query to disk, and a great many olr boxes will boot from an SD card.
// Where the query log should eventually live is genuinely open (docs/dns.md
// §7.5); keeping it in memory is the answer that pre-decides nothing.

// Query is one answered query, as recorded.
//
// The response echoes the question section, so {client, response bytes} is
// fully self-describing: there is no correlation table, no pending-query map
// and no timeout sweeper anywhere in this package. The gap that buys is queries
// which never get an answer — they produce no observation at all — which is why
// Stats counts failures separately and why this is a log of *answered* queries.
type Query struct {
	At     time.Time  `json:"at"`
	Client netip.Addr `json:"client"`

	// Name is the QNAME, lowercased, without the trailing root dot.
	Name string `json:"name"`

	// Type is the qtype spelled the way an operator would recognise it — "A",
	// "AAAA", "HTTPS" — falling back to "TYPE65" for anything unnamed.
	Type string `json:"type"`

	// Rcode is the response code, "NOERROR" or "NXDOMAIN" and so on.
	Rcode string `json:"rcode"`

	// Blocked reports that olr answered this itself rather than relaying it,
	// and Policy names the rule that decided so. Together they are the answer
	// to "why can this device not reach that site", which is the question the
	// query log exists to make answerable.
	Blocked bool   `json:"blocked,omitempty"`
	Policy  string `json:"policy,omitempty"`

	// Answers are the A and AAAA records, in the order they arrived.
	Answers []netip.Addr `json:"answers,omitempty"`

	// Chain is the CNAME chain from the QNAME to whatever finally owned the
	// address records. Its tail is the organisation signal — the reason a
	// device that asked for one name shows up talking to a CDN.
	Chain []string `json:"chain,omitempty"`
}

// Name is one entry in the domain→IP map.
//
// Keyed by (client, address), not by address alone: two devices reaching one
// CDN address by different names is the ordinary case, and global keying would
// attribute one device's traffic to the other's name.
type Name struct {
	Client netip.Addr `json:"client"`

	// Name is the QNAME the client actually asked for, never the record's
	// owner. www.example.com commonly answers as a CNAME to example.cdn.net
	// with the A record owned by the latter; billing the device for a CDN
	// hostname it never typed is the naive mistake here.
	Name string `json:"name"`

	Addr netip.Addr `json:"address"`

	// Chain is the CNAME chain that led here, kept for the same reason
	// Query.Chain is.
	Chain []string `json:"chain,omitempty"`

	// Expires is the record's TTL plus a grace period. Conntrack flows
	// routinely outlive the TTL that created them, and an address reassigned to
	// another tenant misattributes silently.
	Expires time.Time `json:"expires"`

	// LastSeen is when this pairing was last confirmed by an answer.
	LastSeen time.Time `json:"last_seen"`
}

// Client is one address the relay has answered, with how much and how recently.
type Client struct {
	Addr     netip.Addr `json:"address"`
	Queries  uint64     `json:"queries"`
	LastSeen time.Time  `json:"last_seen"`
}

// Stats is the relay's own account of itself.
//
// Every counter here that represents a *gap* is present deliberately. "Always
// show what you cannot account for" is the rule the whole observability story
// rests on, and a tee that silently dropped observations under load would break
// it in the least visible way possible.
type Stats struct {
	// Since is when the relay started. The query log does not survive a
	// restart, and saying so beats implying a history it does not have.
	Since time.Time `json:"since"`

	// Queries is every query accepted, answered or not.
	Queries uint64 `json:"queries"`

	// Blocked is those a policy answered itself.
	Blocked uint64 `json:"blocked"`

	// Refused is queries dropped by access control — somebody outside
	// allow_from asked. A steady stream here is worth an operator's attention.
	Refused uint64 `json:"refused"`

	// Failed is queries the upstream did not answer in time.
	Failed uint64 `json:"failed"`

	// Dropped is observations discarded because the tee was full. The
	// statistics lag; DNS never does. This number is the size of the lie the
	// rest of this struct would otherwise tell.
	Dropped uint64 `json:"dropped"`

	// Unparsed is responses the observer could not read. They were still
	// relayed byte for byte — a parse failure costs a statistics entry and
	// nothing else — so this is a gap in the log, not in anybody's resolution.
	Unparsed uint64 `json:"unparsed"`

	// Held and Capacity describe the ring buffer.
	Held     int `json:"held"`
	Capacity int `json:"capacity"`

	// Clients is who has been asking.
	Clients []Client `json:"clients,omitempty"`
}

// QueriesResponse is the body of GET /queries.
type QueriesResponse struct {
	Queries []Query `json:"queries"`
	Stats   Stats   `json:"stats"`
}

// NamesResponse is the body of GET /names.
type NamesResponse struct {
	Names []Name `json:"names"`
	Stats Stats  `json:"stats"`
}
