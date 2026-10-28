package tools

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/geoip"
)

// dnsExitBudget bounds the whole check: every leg and both sources, plus the
// location lookup for whatever addresses came back.
const dnsExitBudget = 12 * time.Second

// DNSLeg is one resolver this router can name: its own, or one an exit
// selected. Server is the address the query is sent to, so an operator can see
// that this asked the resolver they configured rather than one inferred.
type DNSLeg struct {
	Name   string `json:"name"`
	Server string `json:"server"`
	Kind   string `json:"kind"`
}

// DNSAnswer is one source's report of the address its resolver used.
type DNSAnswer struct {
	Source string       `json:"source"`
	Name   string       `json:"name"`
	IP     string       `json:"ip,omitempty"`
	Place  *geoip.Place `json:"place,omitempty"`
	Error  string       `json:"-"`
}

// DNSLegResult is what one resolver said, or why it said nothing.
type DNSLegResult struct {
	Name    string      `json:"name"`
	Server  string      `json:"server"`
	Kind    string      `json:"kind"`
	Latency float64     `json:"latency_ms"`
	Answers []DNSAnswer `json:"answers"`
	Error   string      `json:"error,omitempty"`
}

type DNSExitReport struct {
	Legs []DNSLegResult `json:"legs"`
}

// whoami is a name whose authoritative server answers with the address of the
// resolver that asked. Two are used so one operator's view is not the only one.
type whoami struct {
	source string
	name   dnsmessage.Name
	kind   dnsmessage.Type
}

var whoamis = []whoami{
	{source: "Google", name: dnsmessage.MustNewName("o-o.myaddr.l.google.com."), kind: dnsmessage.TypeTXT},
	{source: "Akamai", name: dnsmessage.MustNewName("whoami.akamai.net."), kind: dnsmessage.TypeA},
}

var whoamiID atomic.Uint32

func (h *HTTP) dnsExit(w http.ResponseWriter, r *http.Request) {
	if h.DNSLegs == nil {
		core.WriteError(w, http.StatusServiceUnavailable, "no resolver legs are configured")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dnsExitBudget)
	defer cancel()
	report, err := measureDNSEgress(ctx, h.DNSLegs, h.Locate)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		core.WriteError(w, http.StatusServiceUnavailable, "DNS exit check failed: "+err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, report)
}

// measureDNSEgress asks every leg in parallel, then locates the addresses once.
//
// A leg that fails keeps its row: a resolver that did not answer is a fact
// about the network, and dropping it would make the page look complete.
func measureDNSEgress(ctx context.Context, legs func() ([]DNSLeg, error), locate func(context.Context, []netip.Addr) (map[string]geoip.Place, error)) (DNSExitReport, error) {
	list, err := legs()
	if err != nil {
		return DNSExitReport{}, err
	}
	report := DNSExitReport{Legs: make([]DNSLegResult, len(list))}
	var wg sync.WaitGroup
	for i, leg := range list {
		wg.Add(1)
		go func(i int, leg DNSLeg) {
			defer wg.Done()
			report.Legs[i] = probeDNSLeg(ctx, leg)
		}(i, leg)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return DNSExitReport{}, err
	}

	var addrs []netip.Addr
	seen := map[string]bool{}
	for _, leg := range report.Legs {
		for _, answer := range leg.Answers {
			if answer.IP == "" || seen[answer.IP] {
				continue
			}
			if addr, err := netip.ParseAddr(answer.IP); err == nil {
				seen[answer.IP] = true
				addrs = append(addrs, addr)
			}
		}
	}
	if locate == nil || len(addrs) == 0 {
		return report, nil
	}
	places, err := locate(ctx, addrs)
	if err != nil {
		return report, nil
	}
	for i := range report.Legs {
		for j := range report.Legs[i].Answers {
			if place, ok := places[report.Legs[i].Answers[j].IP]; ok {
				p := place
				report.Legs[i].Answers[j].Place = &p
			}
		}
	}
	return report, nil
}

func probeDNSLeg(ctx context.Context, leg DNSLeg) DNSLegResult {
	result := DNSLegResult{Name: leg.Name, Server: leg.Server, Kind: leg.Kind}
	start := time.Now()
	answers := make([]DNSAnswer, len(whoamis))
	var wg sync.WaitGroup
	for i, source := range whoamis {
		wg.Add(1)
		go func(i int, source whoami) {
			defer wg.Done()
			answer := DNSAnswer{Source: source.source, Name: strings.TrimSuffix(source.name.String(), ".")}
			ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()
			ip, err := queryWhoami(ctx, leg.Server, source)
			if err != nil {
				answer.Error = err.Error()
			} else {
				answer.IP = ip.String()
			}
			answers[i] = answer
		}(i, source)
	}
	wg.Wait()
	result.Latency = ms(time.Since(start))
	var failures []string
	for _, answer := range answers {
		if answer.Error != "" {
			failures = append(failures, answer.Source+": "+answer.Error)
			continue
		}
		result.Answers = append(result.Answers, answer)
	}
	if len(result.Answers) == 0 && len(failures) > 0 {
		result.Error = strings.Join(failures, "; ")
	}
	return result
}

// queryWhoami sends one recursion-desired question to server and reads the
// address the answer carries.
//
// The resolver is asked rather than the authoritative server, so the address is
// the one the resolver used — which is the question: where does this leg leave
// from. No cache is bypassed: a whoami answer is not meant to be cached, and a
// resolver that caches one is reporting an address it is still leaving from.
func queryWhoami(ctx context.Context, server string, source whoami) (netip.Addr, error) {
	id := uint16(whoamiID.Add(1))
	builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: id, RecursionDesired: true})
	builder.EnableCompression()
	if err := builder.StartQuestions(); err != nil {
		return netip.Addr{}, err
	}
	if err := builder.Question(dnsmessage.Question{Name: source.name, Type: source.kind, Class: dnsmessage.ClassINET}); err != nil {
		return netip.Addr{}, err
	}
	query, err := builder.Finish()
	if err != nil {
		return netip.Addr{}, err
	}

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "udp", server)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("dialing %s: %w", server, err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if _, err := conn.Write(query); err != nil {
		return netip.Addr{}, err
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return netip.Addr{}, err
	}
	return answerAddress(buf[:n], id)
}

// answerAddress reads the first address an answer carries. Google's TXT may
// carry more than one string, and only the one that parses as an address is
// the answer; EDNS client-subnet text is not an address.
func answerAddress(message []byte, id uint16) (netip.Addr, error) {
	var parser dnsmessage.Parser
	header, err := parser.Start(message)
	if err != nil {
		return netip.Addr{}, err
	}
	if header.ID != id {
		return netip.Addr{}, errors.New("the answer does not belong to this question")
	}
	if err := parser.SkipAllQuestions(); err != nil {
		return netip.Addr{}, err
	}
	for {
		answer, err := parser.Answer()
		if errors.Is(err, dnsmessage.ErrSectionDone) {
			break
		}
		if err != nil {
			return netip.Addr{}, err
		}
		switch body := answer.Body.(type) {
		case *dnsmessage.AResource:
			return netip.AddrFrom4(body.A), nil
		case *dnsmessage.TXTResource:
			for _, text := range body.TXT {
				if addr, err := netip.ParseAddr(strings.TrimSpace(text)); err == nil {
					return addr, nil
				}
			}
		}
	}
	return netip.Addr{}, errors.New("the answer named no address")
}
