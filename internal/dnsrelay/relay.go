package dnsrelay

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// TeeBuffer is how many observations may be in flight behind the fast path.
//
// Small on purpose. The tee exists for isolation, not for throughput: its whole
// job is to keep a slow or panicking observer away from the path that answers
// queries. A large buffer would only delay the moment we notice, and the
// Dropped counter is a better answer than latency.
const TeeBuffer = 256

// Relay is the running data plane: it owns :53, applies policy, and forwards
// everything else to unbound.
//
// Once the fast path is read → access-control → policy → forward → write →
// try-send, it is *done* (docs/dns.md §4.2). Every later feature lands on the
// far side of the tee and cannot regress DNS. That is the line to hold in
// review: anything that influences the response belongs before the forward;
// anything that merely observes goes over the channel.
type Relay struct {
	cfg    Config
	logger *slog.Logger

	// policies is swapped wholesale on SIGHUP. A query in flight finishes
	// against the set it started with, which is correct — a reload is not a
	// promise to re-decide questions already asked.
	policies atomic.Pointer[PolicySet]

	log      *QueryLog
	names    *NameMap
	counters Counters
	clients  *ClientTable
	started  time.Time

	// allow is the compiled access-control list, masked once at startup.
	allow []netip.Prefix

	tee chan observation
}

// observation is one thing to look at later. It carries a *copy* of the
// response bytes; see Relay.observe for why that is not optional.
type observation struct {
	at       time.Time
	client   netip.Addr
	msg      []byte
	blocked  bool
	policy   string
	question Question
	haveQ    bool
}

// New builds a relay from a rendered configuration.
func New(cfg Config, logger *slog.Logger) (*Relay, error) {
	if len(cfg.Listen) == 0 {
		return nil, fmt.Errorf("no listen address configured, so there is nothing to answer on")
	}
	if !cfg.Upstream.IsValid() {
		return nil, fmt.Errorf("no upstream resolver configured, so there is nothing to forward to")
	}
	if logger == nil {
		logger = slog.Default()
	}

	r := &Relay{
		cfg:     cfg,
		logger:  logger,
		log:     NewQueryLog(cfg.QueryLogEntries),
		names:   NewNameMap(),
		clients: NewClientTable(),
		started: time.Now(),
		tee:     make(chan observation, TeeBuffer),
	}
	for _, p := range cfg.AllowFrom {
		r.allow = append(r.allow, p.Masked())
	}

	if err := r.Reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// Reload re-reads the policy directory and swaps the compiled set in.
//
// Called on SIGHUP, which is how olrd tells this process that a blocklist
// changed. The listen addresses and the upstream are not re-read: rebinding a
// socket is a restart, and the split between the two rendered files is exactly
// that distinction (see config.go).
func (r *Relay) Reload() error {
	policies, err := LoadPolicies(r.cfg.PolicyDir)
	if err != nil {
		// The old set stays in place. A policy file that a partial write left
		// unparseable must not silently unblock everything it used to block —
		// failing closed on the previous rules is the safe direction, and the
		// error reaches the journal.
		return err
	}
	r.policies.Store(Compile(policies))
	r.logger.Info("policies loaded", "count", len(policies), "dir", r.cfg.PolicyDir)
	return nil
}

// Policies returns the live set, for the query path and for tests.
func (r *Relay) Policies() *PolicySet { return r.policies.Load() }

// LoadPolicies reads every policy file in a directory.
//
// A missing directory is not an error: it means no policy has been configured,
// which is the ordinary state of a box that only wants the query log.
func LoadPolicies(dir string) ([]Policy, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}

	var out []Policy
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		p, err := UnmarshalPolicy(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, p)
	}

	// Sorted so that two boxes with the same files compile the same set, and so
	// that a directory read order cannot change which default policy wins.
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// allowed reports whether a source may query us.
//
// An empty list denies everybody. That is the safe direction to fail: a relay
// that answers nobody is a visible outage somebody fixes in minutes, where one
// that answers the internet is an amplifier nobody notices until it is used
// against a third party (docs/dns.md §5).
func (r *Relay) allowed(client netip.Addr) bool {
	client = client.Unmap()
	for _, p := range r.allow {
		if p.Contains(client) {
			return true
		}
	}
	return false
}

// result is what resolving one query produced.
type result struct {
	response []byte
	question Question
	haveQ    bool
	decision Decision
}

// resolve applies policy and produces the bytes to send back.
//
// It is the whole of the fast path's decision-making, and it is deliberately
// short. Note what it does *not* do: it never inspects or rebuilds a relayed
// response. The bytes that come back from upstream are the bytes that go to the
// client.
func (r *Relay) resolve(ctx context.Context, client netip.Addr, query []byte, overTCP bool) (result, error) {
	var res result

	// Best-effort. A query we cannot parse is still relayed — unbound is far
	// better placed to reject a malformed message than we are, and refusing it
	// here would be us inventing a stricter DNS than the one on the wire.
	if q, _, err := ParseQuestion(query); err == nil {
		res.question, res.haveQ = q, true
		res.decision = r.Policies().Decide(client, q.Name)
	}

	if res.decision.Blocked {
		response, err := Synthesize(query, res.decision.Response)
		if err != nil {
			// Falling through to the upstream rather than failing: not being
			// able to build a refusal is our bug, and taking the name down as a
			// side effect of it would be the worse outcome.
			r.logger.Warn("could not synthesise a blocked response; relaying instead",
				"name", res.question.Name, "error", err)
			res.decision.Blocked = false
		} else {
			r.counters.Blocked.Add(1)
			res.response = response
			return res, nil
		}
	}

	response, err := r.forward(ctx, query, overTCP)
	if err != nil {
		r.counters.Failed.Add(1)
		return res, err
	}
	res.response = response
	return res, nil
}

// observe hands a copy of the answer to the tee, and never blocks.
//
// Three obligations come with the tee, and all three are discharged here so
// that no caller can get them wrong:
//
//  1. **Copy the bytes, never send the slice.** Handing a read buffer to a
//     channel while the read loop reuses it misattributes answers to the wrong
//     device — intermittently, under load only, which is the worst way to find
//     a bug.
//  2. **Never block on the send.** A channel that blocks when full turns a slow
//     observer into DNS latency, which is the exact failure the tee exists to
//     prevent. Drop, and count the drops: the gap must be visible.
//  3. The parser on the far side runs under recover(); see observeLoop.
func (r *Relay) observe(client netip.Addr, res result, at time.Time) {
	if !r.log.Enabled() && r.names == nil {
		return
	}

	msg := make([]byte, len(res.response))
	copy(msg, res.response)

	select {
	case r.tee <- observation{
		at: at, client: client, msg: msg,
		blocked: res.decision.Blocked, policy: res.decision.Policy,
		question: res.question, haveQ: res.haveQ,
	}:
	default:
		// Statistics lag; DNS never does.
		r.counters.Dropped.Add(1)
	}
}

// observeLoop is the far side of the tee: everything that merely watches.
//
// Nothing in here can affect an answer. That is the property the whole design
// rests on, so it is worth saying plainly: if this goroutine wedged, panicked
// or ran a hundred times too slowly, every device on the network would still
// resolve names at full speed and the only symptom would be the Dropped
// counter climbing.
func (r *Relay) observeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case o := <-r.tee:
			r.record(o)
		}
	}
}

// record parses one observation, under recover().
//
// Malformed responses are routine — not rare, routine — and compression-pointer
// handling is where they bite. On the fast path a panic here would end DNS for
// the house; on this side of the tee it increments a counter.
func (r *Relay) record(o observation) {
	defer func() {
		if p := recover(); p != nil {
			r.counters.Unparsed.Add(1)
			r.logger.Warn("recovered from a panic while parsing a response",
				"client", o.client, "panic", p)
		}
	}()

	obs, err := Observe(o.msg)
	if err != nil {
		r.counters.Unparsed.Add(1)
		// Still logged, with what we know from the question. A response we
		// could not read is not a query that did not happen, and dropping the
		// row entirely would make the log quietly incomplete.
		if o.haveQ {
			r.log.Add(Query{
				At: o.at, Client: o.client, Name: o.question.Name,
				Type: o.question.TypeName, Rcode: "UNPARSED",
				Blocked: o.blocked, Policy: o.policy,
			})
		}
		return
	}

	r.log.Add(Query{
		At: o.at, Client: o.client, Name: obs.Name, Type: obs.TypeName,
		Rcode: obs.Rcode, Blocked: o.blocked, Policy: o.policy,
		Answers: obs.Addrs, Chain: obs.Chain,
	})

	// A blocked answer's addresses are ours, not the name's. Recording 0.0.0.0
	// against a domain would pollute the attribution map with an address no
	// traffic will ever go to.
	if !o.blocked {
		r.names.Record(o.client, obs, o.at)
	}
}

// Queries returns the query log, newest first.
func (r *Relay) Queries() []Query { return r.log.Snapshot() }

// Names returns the live domain→address map.
func (r *Relay) Names(now time.Time) []Name { return r.names.Snapshot(now) }
