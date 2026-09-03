package dnsrelay

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// DNS message handling, and the invariant the whole relay is built around:
//
//	Parse a copy for observation. Forward the original bytes untouched.
//
// Never re-serialise a response. Re-serialising breaks DNSSEC signatures, drops
// EDNS options we did not model, and mangles record types we have never heard
// of. Relaying the original means a parse failure costs a statistics entry and
// nothing else — resolution still works. That makes everything below
// best-effort *by construction*: it never has to be right, only useful.
//
// The one place it bends is Synthesize, which has to build a response rather
// than relay one. That is the only code path here that must be exactly right,
// and it is small on purpose.
//
// golang.org/x/net/dns/dnsmessage rather than hand-rolled, for one reason worth
// stating: compression pointers. A malformed response with a pointer loop hangs
// a naive parser, and that parser would be sitting in the path of every name
// the network resolves.

// BlockTTL is how long a client may cache a blocked answer.
//
// Short, because it is the delay between an operator unblocking a name and the
// device believing them. A long TTL here turns "I fixed it" into "I fixed it,
// try again in an hour", which is indistinguishable from not having fixed it.
const BlockTTL = 60

// Question is what a client asked, as much of it as policy needs.
type Question struct {
	// Name is the QNAME, lowercased and without the trailing root dot.
	Name string

	// Type is the qtype, kept as its wire value so Synthesize can answer the
	// right record without a second parse.
	Type dnsmessage.Type

	// TypeName is the same thing an operator would recognise.
	TypeName string
}

// ParseQuestion reads the question section of a query.
//
// Only the first question. Multi-question queries are legal on the wire and
// essentially unused in practice — no resolver implements them coherently — so
// policy is decided on the first, and the message is relayed whole regardless.
func ParseQuestion(msg []byte) (Question, dnsmessage.Header, error) {
	var p dnsmessage.Parser
	header, err := p.Start(msg)
	if err != nil {
		return Question{}, dnsmessage.Header{}, fmt.Errorf("parsing header: %w", err)
	}
	q, err := p.Question()
	if err != nil {
		return Question{}, header, fmt.Errorf("parsing question: %w", err)
	}
	return Question{
		Name:     CanonicalName(q.Name.String()),
		Type:     q.Type,
		TypeName: TypeName(q.Type),
	}, header, nil
}

// Synthesize builds the response to a blocked query.
//
// It answers the original query's ID and question section, because a client
// matches a response to its query by both and will silently discard anything
// else — a blocked name that produced a discarded answer would look like a
// timeout, which is exactly the failure mode blocking is supposed to avoid.
func Synthesize(query []byte, response string) ([]byte, error) {
	var p dnsmessage.Parser
	header, err := p.Start(query)
	if err != nil {
		return nil, fmt.Errorf("parsing header: %w", err)
	}
	q, err := p.Question()
	if err != nil {
		return nil, fmt.Errorf("parsing question: %w", err)
	}

	out := dnsmessage.Header{
		ID:                 header.ID,
		Response:           true,
		OpCode:             header.OpCode,
		RecursionDesired:   header.RecursionDesired,
		RecursionAvailable: true,
	}
	if response != RespondZero {
		out.RCode = dnsmessage.RCodeNameError
	}

	b := dnsmessage.NewBuilder(nil, out)
	b.EnableCompression()
	if err := b.StartQuestions(); err != nil {
		return nil, err
	}
	if err := b.Question(q); err != nil {
		return nil, err
	}

	if response == RespondZero {
		if err := b.StartAnswers(); err != nil {
			return nil, err
		}
		rh := dnsmessage.ResourceHeader{Name: q.Name, Class: q.Class, TTL: BlockTTL}
		switch q.Type {
		case dnsmessage.TypeA:
			err = b.AResource(rh, dnsmessage.AResource{A: [4]byte{}})
		case dnsmessage.TypeAAAA:
			err = b.AAAAResource(rh, dnsmessage.AAAAResource{AAAA: [16]byte{}})
		default:
			// NODATA: the name exists, this type does not. Answering an address
			// for a qtype that is not an address would be a malformed reply,
			// and NXDOMAIN would contradict the A record we just promised.
			err = nil
		}
		if err != nil {
			return nil, err
		}
	}

	return b.Finish()
}

// Observation is what one response tells us, parsed from a copy.
type Observation struct {
	// Name is the QNAME. Everything below is attributed to it, never to the
	// record's owner: www.example.com commonly answers as a CNAME to
	// example.cdn.net with the A record owned by the latter, and billing the
	// device for a CDN hostname it never asked for is the naive mistake.
	Name     string
	TypeName string
	Rcode    string

	// Addrs are the A and AAAA records, in arrival order.
	Addrs []netip.Addr

	// Chain is the CNAME chain from the QNAME onwards. Its tail is the
	// organisation signal — the reason a device that asked for one name shows
	// up talking to a CDN.
	Chain []string

	// TTL is the smallest TTL among the address records, which is when the
	// answer stops being true.
	TTL time.Duration
}

// Observe parses a response for the query log and the name map.
//
// It never returns partial nonsense: a message it cannot read at all is an
// error, and an error here costs a statistics entry. The response itself was
// already on its way to the client before this ran.
func Observe(msg []byte) (Observation, error) {
	var p dnsmessage.Parser
	header, err := p.Start(msg)
	if err != nil {
		return Observation{}, fmt.Errorf("parsing header: %w", err)
	}
	q, err := p.Question()
	if err != nil {
		return Observation{}, fmt.Errorf("parsing question: %w", err)
	}
	if err := p.SkipAllQuestions(); err != nil {
		return Observation{}, fmt.Errorf("skipping questions: %w", err)
	}

	obs := Observation{
		Name:     CanonicalName(q.Name.String()),
		TypeName: TypeName(q.Type),
		Rcode:    RcodeName(header.RCode),
	}

	// target follows the CNAME chain, so an address record is only attributed
	// when it is actually the answer to what was asked rather than an unrelated
	// name a server chose to include.
	target := obs.Name
	minTTL := ^uint32(0)

	for {
		h, err := p.AnswerHeader()
		if err == dnsmessage.ErrSectionDone {
			break
		}
		if err != nil {
			return obs, fmt.Errorf("parsing answer: %w", err)
		}

		owner := CanonicalName(h.Name.String())
		switch h.Type {
		case dnsmessage.TypeCNAME:
			r, err := p.CNAMEResource()
			if err != nil {
				return obs, fmt.Errorf("parsing CNAME: %w", err)
			}
			next := CanonicalName(r.CNAME.String())
			if owner == target {
				obs.Chain = append(obs.Chain, next)
				target = next
			}
		case dnsmessage.TypeA:
			r, err := p.AResource()
			if err != nil {
				return obs, fmt.Errorf("parsing A: %w", err)
			}
			if owner == target {
				obs.Addrs = append(obs.Addrs, netip.AddrFrom4(r.A))
				minTTL = min(minTTL, h.TTL)
			}
		case dnsmessage.TypeAAAA:
			r, err := p.AAAAResource()
			if err != nil {
				return obs, fmt.Errorf("parsing AAAA: %w", err)
			}
			if owner == target {
				obs.Addrs = append(obs.Addrs, netip.AddrFrom16(r.AAAA))
				minTTL = min(minTTL, h.TTL)
			}
		default:
			// Everything else is relayed and not modelled. design.md §4.5's
			// guardrail applies to observation too: if we do not expose it, we
			// do not model it.
			if err := p.SkipAnswer(); err != nil {
				return obs, fmt.Errorf("skipping answer: %w", err)
			}
		}
	}

	// Authority and additional sections are deliberately not read. RRSIG and
	// NSEC are relayed, never validated — that is unbound's job and we would
	// get it subtly wrong for years (docs/dns.md §4).

	if len(obs.Addrs) > 0 && minTTL != ^uint32(0) {
		obs.TTL = time.Duration(minTTL) * time.Second
	}
	return obs, nil
}

// CanonicalName lowercases a wire-form name and drops the trailing root dot, so
// that "WWW.Example.COM." and "www.example.com" are one key rather than three.
func CanonicalName(s string) string {
	s = strings.ToLower(s)
	if s == "." {
		// The root, which is a legitimate QNAME for an NS query. Left as-is
		// rather than trimmed to the empty string, which would read as "no
		// name" everywhere downstream.
		return "."
	}
	return strings.TrimSuffix(s, ".")
}

// typeNames spells the qtypes an operator recognises.
//
// Our own table rather than dnsmessage.Type.String(), which returns "TypeA" —
// readable in a Go error and wrong in a column headed TYPE. It also lets us
// name HTTPS (65), which dnsmessage does not know and which is now a large
// share of what a browser asks for.
var typeNames = map[dnsmessage.Type]string{
	dnsmessage.TypeA:     "A",
	dnsmessage.TypeNS:    "NS",
	dnsmessage.TypeCNAME: "CNAME",
	dnsmessage.TypeSOA:   "SOA",
	dnsmessage.TypePTR:   "PTR",
	dnsmessage.TypeMX:    "MX",
	dnsmessage.TypeTXT:   "TXT",
	dnsmessage.TypeAAAA:  "AAAA",
	dnsmessage.TypeSRV:   "SRV",
	dnsmessage.TypeOPT:   "OPT",
	64:                   "SVCB",
	65:                   "HTTPS",
}

// TypeName spells a qtype, falling back to the TYPEnnn form the RFCs define for
// anything unnamed.
func TypeName(t dnsmessage.Type) string {
	if n, ok := typeNames[t]; ok {
		return n
	}
	return "TYPE" + strconv.Itoa(int(t))
}

var rcodeNames = map[dnsmessage.RCode]string{
	dnsmessage.RCodeSuccess:        "NOERROR",
	dnsmessage.RCodeFormatError:    "FORMERR",
	dnsmessage.RCodeServerFailure:  "SERVFAIL",
	dnsmessage.RCodeNameError:      "NXDOMAIN",
	dnsmessage.RCodeNotImplemented: "NOTIMP",
	dnsmessage.RCodeRefused:        "REFUSED",
}

// RcodeName spells a response code.
func RcodeName(r dnsmessage.RCode) string {
	if n, ok := rcodeNames[r]; ok {
		return n
	}
	return "RCODE" + strconv.Itoa(int(r))
}
