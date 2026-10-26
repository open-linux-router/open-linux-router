package inspection

import (
	"context"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"
)

// What this device did, per site, over the last week.
//
// The device page's chart asks a question nothing else in olr answers: not "who
// is this device talking to now" (conntrack) and not "what names did it look
// up" (the relay's log), but "where did the bytes go, over time, and under which
// name". That join is (device address, remote address) from conntrack against
// the relay's observed name map, accumulated into buckets.
//
// Three properties, each a decision:
//
//   - **In memory.** design.md §10 rejected writing flow events to disk, and the
//     reason has not changed: a line per sample is flash wear on an SD-card
//     router, and a per-device history is a profile of somebody's evening. A
//     week of half-hour buckets is a bounded ring that costs a few megabytes and
//     disappears with the process. A restart loses the chart; that is the trade.
//   - **Attribution is fixed at sample time.** A bucket is written once, from
//     the name the relay had at that moment. Later queries never rewrite it, so
//     last night's numbers do not change while nobody is looking.
//   - **Unnamed is a bucket, not a gap.** Traffic whose address was never
//     resolved is kept under "未命名"; dropping it would make the totals lie.
const (
	// activityWidth is the width of a stored bucket.
	activityWidth = 30 * time.Minute
	// ActivityRetention is how far back the ring reaches.
	ActivityRetention = 7 * 24 * time.Hour
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

// ActivityBucket is one half-hour, as the API and the chart read it.
type ActivityBucket struct {
	At    time.Time   `json:"at"`
	Sites []SiteUsage `json:"sites"`
}

// Activity keeps a bounded ring of per-site traffic per address.
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
// enough that a half-hour bucket is accurate to a rounding error, and long
// enough that reading conntrack is not the busiest thing the box does.
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

// Sample reads the table once and folds the differences into the current bucket.
func (a *Activity) Sample(ctx context.Context) error {
	flows, err := a.flows(ctx)
	if err != nil {
		return err
	}
	now := a.now()

	a.mu.Lock()
	defer a.mu.Unlock()

	if now.Sub(a.namesAt) > 15*time.Second && a.names != nil {
		if names, err := a.names(ctx); err == nil {
			a.reindex(names)
			a.namesAt = now
		}
	}

	seen := make(map[flowKey][2]uint64, len(flows))
	for _, f := range flows {
		if !attributable(f) {
			continue
		}
		k := flowKey{f.Src, f.Dst, f.Sport, f.Dport, f.Proto}
		seen[k] = [2]uint64{f.BytesOut, f.BytesIn}
		before := a.prev[k]
		var up, down uint64
		if f.BytesOut >= before[0] {
			up = f.BytesOut - before[0]
		}
		if f.BytesIn >= before[1] {
			down = f.BytesIn - before[1]
		}
		if up == 0 && down == 0 {
			continue
		}
		a.add(now, f.Src, a.siteFor(k), up, down)
	}
	a.prev = seen
	a.prune(now)
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

// siteOf reduces a host name to the site it belongs to. Two labels is the
// registrable domain for everything a household talks to, and it keeps a CDN's
// per-edge names from turning one site into forty rows.
func siteOf(name string) string {
	name = strings.TrimSuffix(strings.ToLower(name), ".")
	if name == "" {
		return unnamedSite
	}
	labels := strings.Split(name, ".")
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

// prune drops buckets past retention, then whole devices, then whole buckets,
// until the ring fits. History is never evicted to make room for new data in the
// same bucket: the direction that keeps what is already on screen correct.
func (a *Activity) prune(now time.Time) {
	cutoff := now.Add(-ActivityRetention).Truncate(activityWidth).Unix()
	for len(a.order) > 0 && a.order[0] < cutoff {
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

// Snapshot returns the retained buckets for the given addresses, oldest first.
// Sites beyond activitySites per bucket are folded into one row.
func (a *Activity) Snapshot(addresses []netip.Addr, since time.Time) []ActivityBucket {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	wanted := make(map[netip.Addr]bool, len(addresses))
	for _, addr := range addresses {
		wanted[addr.Unmap()] = true
	}
	cutoff := since.Truncate(activityWidth).Unix()
	out := make([]ActivityBucket, 0, len(a.order))
	for _, key := range a.order {
		if key < cutoff {
			continue
		}
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
