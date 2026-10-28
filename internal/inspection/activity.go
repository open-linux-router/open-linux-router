package inspection

import (
	"context"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"
)

// What this device did, per site, while the inspection session ran.
//
// The device page's chart asks a question nothing else in olr answers: not "who
// is this device talking to now" (conntrack) and not "what names did it look
// up" (the relay's log), but "where did the bytes go, over time, and under which
// name" while inspection runs. That join is (device address, remote address)
// from conntrack against the relay's observed name map, accumulated into
// buckets.
//
// Three properties, each a decision:
//
//   - **Only while the switch is on.** The chart is a live view, not history:
//     Begin starts the series with the session and End drops it, and nothing
//     survives the process. Turning inspection off clears what it saw rather
//     than leaving a week of somebody's evening behind. That is the same
//     argument design.md §10 makes against writing flow events to disk,
//     applied to keeping them after the operator stopped looking.
//   - **Attribution is fixed at sample time.** A bucket is written once, from
//     the name the relay had at that moment. Later queries never rewrite it, so
//     the numbers already on screen do not change while nobody is looking.
//   - **Unnamed is a bucket, not a gap.** Traffic whose address was never
//     resolved is kept under "未命名"; dropping it would make the totals lie.
const (
	// activityWidth is the width of a stored bucket. The switch stays on as
	// long as the operator leaves it, so a minute is the resolution the chart
	// can show without an unreadable axis.
	activityWidth = time.Minute
	// activityBuckets bounds the ring: four hours at one minute each. The
	// chart scrolls past that rather than resetting, and a router left on for
	// days still spends a fixed amount of memory on the series.
	activityBuckets = 240
	// activityFloor is the chart's smallest frame. It keeps a two-minute
	// inspection from drawing a sliver one bucket wide; the axis grows with
	// the time the switch has been on and stops at the ring.
	activityFloor = 15 * time.Minute
	// activitySites bounds the sites kept per (bucket, address). Beyond it the
	// smallest are folded into one row rather than dropped.
	activitySites = 24
	// activityEntries bounds the whole ring, so a device enumerating a wildcard
	// cannot grow this without limit.
	activityEntries = 50000
	// unnamedSite is the row every unresolved flow lands in.
	unnamedSite = "未命名"
	// otherSite collects the sites that fall outside activitySites.
	otherSite = "其他"
)

// Flow is one conntrack entry as the sampler needs it.
type Flow struct {
	Src, Dst          netip.Addr
	Sport, Dport      uint16
	Proto             string
	BytesOut, BytesIn uint64
}

// Name is one observed domain→address pairing for a client.
type Name struct {
	Client, Addr netip.Addr
	Name         string
}

// SiteUsage is one row of a bucket.
type SiteUsage struct {
	Site string `json:"site"`
	Up   uint64 `json:"up"`
	Down uint64 `json:"down"`
}

// ActivityBucket is one minute of a session, as the API and the chart read it.
type ActivityBucket struct {
	At    time.Time   `json:"at"`
	Sites []SiteUsage `json:"sites"`
}

// Activity keeps a bounded ring of per-site traffic for one session's addresses.
//
// Sampling and reading never block each other for longer than a copy: the
// sampler holds the lock for the length of one diff, and a reader copies the
// buckets it wants.
type Activity struct {
	mu      sync.Mutex
	flows   func(context.Context) ([]Flow, error)
	names   func(context.Context) ([]Name, error)
	now     func() time.Time
	prev    map[flowKey][2]uint64
	buckets map[int64]map[netip.Addr]map[string]*counters
	order   []int64
	entries int

	// watched and started are the open session. A zero started means idle: the
	// sampler reads nothing and the endpoint has nothing to return.
	watched map[netip.Addr]bool
	started time.Time
	// baseline asks the next sample to record counters without counting them.
	// It is only needed when reading the table at Begin failed: the first
	// successful reading is then still history from before the session.
	baseline bool

	// namesSeen is rebuilt at most every nameRefresh; the relay's map is short
	// lived, so a stale copy would name traffic after the wrong site.
	siteByFlow map[flowKey]string
	namesAt    time.Time
}

type flowKey struct {
	src, dst     netip.Addr
	sport, dport uint16
	proto        string
}

type counters struct{ up, down uint64 }

// NewActivity builds a recorder. flows and names may be nil for a build with no
// way to observe; Run then does nothing.
func NewActivity(flows func(context.Context) ([]Flow, error), names func(context.Context) ([]Name, error)) *Activity {
	return &Activity{
		flows: flows, names: names, now: time.Now,
		prev:       map[flowKey][2]uint64{},
		buckets:    map[int64]map[netip.Addr]map[string]*counters{},
		siteByFlow: map[flowKey]string{},
	}
}

// SampleInterval is how often Run reads the table. Thirty seconds is short
// enough that a minute bucket is accurate to a rounding error, and long enough
// that reading conntrack is not the busiest thing the box does.
const SampleInterval = 30 * time.Second

// Run samples until ctx ends.
func (a *Activity) Run(ctx context.Context) {
	if a == nil || a.flows == nil {
		return
	}
	tick := time.NewTicker(SampleInterval)
	defer tick.Stop()
	for {
		if err := a.Sample(ctx); err != nil && ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// Begin starts a session's series for the addresses it inspects, dropping
// anything a previous session left behind. The counters already on the table
// belong to before the session, so they are read once as a baseline and never
// charged to it.
func (a *Activity) Begin(ctx context.Context, addresses []string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reset()
	a.watched = make(map[netip.Addr]bool, len(addresses))
	for _, raw := range addresses {
		if addr, err := netip.ParseAddr(strings.TrimSpace(raw)); err == nil {
			a.watched[addr.Unmap()] = true
		}
	}
	if a.flows != nil {
		flows, err := a.flows(ctx)
		if err != nil {
			// The first reading that succeeds becomes the baseline instead.
			a.baseline = true
		} else {
			a.remember(flows)
		}
	}
	a.started = a.now()
}

// remember records the counters of the flows the session watches, without
// counting any of them as traffic. The caller holds the lock.
func (a *Activity) remember(flows []Flow) {
	for _, f := range flows {
		src := f.Src.Unmap()
		if !attributable(f) || !a.watched[src] {
			continue
		}
		a.prev[flowKey{src, f.Dst, f.Sport, f.Dport, f.Proto}] = [2]uint64{f.BytesOut, f.BytesIn}
	}
}

// End stops recording and forgets the session.
func (a *Activity) End() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reset()
}

// Started is when the current series began, or the zero time while idle.
// activityWindow is the chart's time frame for a session that has run for
// elapsed: at least activityFloor so a short inspection stays readable, at most
// the ring's span so the axis never promises memory that was pruned, and
// rounded up to whole buckets so slots line up.
func activityWindow(elapsed time.Duration) time.Duration {
	window := max(elapsed, activityFloor)
	window = min(window, activityBuckets*activityWidth)
	return (window + activityWidth - 1) / activityWidth * activityWidth
}

func (a *Activity) Started() time.Time {
	if a == nil {
		return time.Time{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.started
}

// reset drops the session's counters. The caller holds the lock.
func (a *Activity) reset() {
	a.prev = map[flowKey][2]uint64{}
	a.buckets = map[int64]map[netip.Addr]map[string]*counters{}
	a.order = nil
	a.entries = 0
	a.siteByFlow = map[flowKey]string{}
	a.namesAt = time.Time{}
	a.watched = nil
	a.started = time.Time{}
	a.baseline = false
}

func (a *Activity) recording() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.started.IsZero()
}

// Sample reads the table once and folds the differences into the current bucket.
func (a *Activity) Sample(ctx context.Context) error {
	if a == nil || a.flows == nil || !a.recording() {
		return nil
	}
	flows, err := a.flows(ctx)
	if err != nil {
		return err
	}
	now := a.now()

	a.mu.Lock()
	defer a.mu.Unlock()
	// The session can end while conntrack is being read; its ring is already
	// gone and must not be rebuilt from a reading nothing asked for.
	if a.started.IsZero() {
		return nil
	}

	if now.Sub(a.namesAt) > 15*time.Second && a.names != nil {
		if names, err := a.names(ctx); err == nil {
			a.reindex(names)
			a.namesAt = now
		}
	}

	seen := make(map[flowKey][2]uint64, len(flows))
	watched := make([]flowKey, 0, len(flows))
	for _, f := range flows {
		if !attributable(f) {
			continue
		}
		src := f.Src.Unmap()
		if !a.watched[src] {
			continue
		}
		k := flowKey{src, f.Dst, f.Sport, f.Dport, f.Proto}
		seen[k] = [2]uint64{f.BytesOut, f.BytesIn}
		watched = append(watched, k)
	}
	if a.baseline {
		// The table could not be read at Begin; this reading is still history
		// from before the session, so it becomes the baseline instead of traffic.
		a.prev = seen
		a.baseline = false
		return nil
	}
	for _, k := range watched {
		before := a.prev[k]
		var up, down uint64
		if seen[k][0] >= before[0] {
			up = seen[k][0] - before[0]
		}
		if seen[k][1] >= before[1] {
			down = seen[k][1] - before[1]
		}
		if up == 0 && down == 0 {
			continue
		}
		a.add(now, k.src, a.siteFor(k), up, down)
	}
	a.prev = seen
	a.prune()
	return nil
}

// attributable reports whether a flow is a device reaching the internet. The
// router's own traffic and LAN-to-LAN traffic never appear in a device's chart.
func attributable(f Flow) bool {
	return f.Src.IsValid() && f.Dst.IsValid() && !f.Src.IsLoopback() && !f.Src.IsUnspecified() &&
		f.Src.IsPrivate() && !f.Dst.IsPrivate() && !f.Dst.IsLoopback() && !f.Dst.IsMulticast()
}

func (a *Activity) reindex(names []Name) {
	next := make(map[flowKey]string, len(names))
	for _, n := range names {
		next[flowKey{src: n.Client, dst: n.Addr}] = siteOf(n.Name)
	}
	a.siteByFlow = next
}

// siteFor looks up the name for a flow's device and address. The name map is
// keyed by client and address only; the ports do not matter to a name.
func (a *Activity) siteFor(k flowKey) string {
	if site, ok := a.siteByFlow[flowKey{src: k.src, dst: k.dst}]; ok {
		return site
	}
	return unnamedSite
}

// siteOf reduces a host name to the site it belongs to: the registrable domain
// under the public suffix list. Two labels was that for everything a household
// talks to until a name ended in `com.cn`, where the suffix is the last two
// labels and the site is the third. The list is the maintained answer to "which
// suffix is this", so a CDN's per-edge names under one domain stay one row.
//
// Only ICANN suffixes count. A private suffix such as `s3.amazonaws.com` names
// an object store, not a site, and treating it as one would give every bucket
// its own row.
func siteOf(name string) string {
	name = strings.TrimSuffix(strings.ToLower(name), ".")
	if name == "" {
		return unnamedSite
	}
	labels := strings.Split(name, ".")
	// The registrable domain is the longest registered public suffix plus one
	// label. The first exact match walking down the labels is that suffix.
	for i := 1; i < len(labels); i++ {
		candidate := strings.Join(labels[i:], ".")
		if suffix, icann := publicsuffix.PublicSuffix(candidate); icann && suffix == candidate {
			return strings.Join(labels[i-1:], ".")
		}
	}
	// Unknown or single-label namespace: two labels is the best available guess.
	if len(labels) < 2 {
		return name
	}
	return strings.Join(labels[len(labels)-2:], ".")
}

func (a *Activity) add(now time.Time, addr netip.Addr, site string, up, down uint64) {
	key := now.Truncate(activityWidth).Unix()
	byAddr, ok := a.buckets[key]
	if !ok {
		byAddr = map[netip.Addr]map[string]*counters{}
		a.buckets[key] = byAddr
		a.order = append(a.order, key)
		sort.Slice(a.order, func(i, j int) bool { return a.order[i] < a.order[j] })
	}
	sites, ok := byAddr[addr]
	if !ok {
		sites = map[string]*counters{}
		byAddr[addr] = sites
	}
	c, ok := sites[site]
	if !ok {
		c = &counters{}
		sites[site] = c
		a.entries++
	}
	c.up += up
	c.down += down
}

// prune drops the oldest buckets until the ring fits. History is never evicted
// to make room for new data in the same bucket: the direction that keeps what is
// already on screen correct.
func (a *Activity) prune() {
	for len(a.order) > activityBuckets {
		a.dropBucket(a.order[0])
	}
	for a.entries > activityEntries && len(a.order) > 0 {
		a.dropBucket(a.order[0])
	}
}

func (a *Activity) dropBucket(key int64) {
	byAddr := a.buckets[key]
	for addr := range byAddr {
		a.entries -= len(byAddr[addr])
	}
	delete(a.buckets, key)
	if len(a.order) > 0 && a.order[0] == key {
		a.order = a.order[1:]
		return
	}
	for i, k := range a.order {
		if k == key {
			a.order = append(a.order[:i], a.order[i+1:]...)
			return
		}
	}
}

// Snapshot returns the current session's buckets for the given addresses,
// oldest first. Sites beyond activitySites per bucket are folded into one row.
func (a *Activity) Snapshot(addresses []netip.Addr) []ActivityBucket {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.started.IsZero() {
		// No session, no series: an empty list rather than a missing one, so the
		// device page can keep counting on a list after the session ends.
		return []ActivityBucket{}
	}

	wanted := make(map[netip.Addr]bool, len(addresses))
	for _, addr := range addresses {
		wanted[addr.Unmap()] = true
	}
	out := make([]ActivityBucket, 0, len(a.order))
	for _, key := range a.order {
		byAddr, ok := a.buckets[key]
		if !ok {
			continue
		}
		merged := map[string]*counters{}
		for addr, sites := range byAddr {
			if !wanted[addr] {
				continue
			}
			for site, c := range sites {
				m, ok := merged[site]
				if !ok {
					m = &counters{}
					merged[site] = m
				}
				m.up += c.up
				m.down += c.down
			}
		}
		if len(merged) == 0 {
			continue
		}
		bucket := ActivityBucket{At: time.Unix(key, 0), Sites: foldSites(merged)}
		out = append(out, bucket)
	}
	return out
}

// foldSites sorts a bucket's rows by size and folds the tail into one row.
func foldSites(merged map[string]*counters) []SiteUsage {
	rows := make([]SiteUsage, 0, len(merged))
	for site, c := range merged {
		rows = append(rows, SiteUsage{Site: site, Up: c.up, Down: c.down})
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Up+rows[i].Down > rows[j].Up+rows[j].Down
	})
	if len(rows) <= activitySites {
		return rows
	}
	out := rows[:activitySites-1]
	var other SiteUsage
	other.Site = otherSite
	for _, row := range rows[activitySites-1:] {
		other.Up += row.Up
		other.Down += row.Down
	}
	return append(out, other)
}
