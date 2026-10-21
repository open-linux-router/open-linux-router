package gateway

import (
	"fmt"
	"net/netip"
	"sort"
	"time"
)

// The route table, read rather than configured.
//
// Every other card on Gateway answers for a module that owns something. This one
// answers for `ip route`: the tables the kernel actually consults, which are the
// main table olr deliberately does not squat (design.md §3.4, docs/dial.md §2),
// the per-exit tables olr writes, and whatever tables somebody else's rules
// point at. It is read-only by construction — there is no editor behind it and
// nothing here plans a change — and that is the point: the page that says
// whether the LAN gets out should be able to show the table it gets out
// through, without becoming a second place to edit routing.
//
// The kernel window is Kernel.ReadRoutes, separate from Observe because Observe
// reads the one default route per owned table that drift detection compares.
// This reads every route in every table, which is a different question asked at
// a different rate.

// KernelRoute is one route as the kernel reports it, before it is grouped by
// table or named for a human.
type KernelRoute struct {
	Family int          // 4 or 6
	Table  int          // routing table id
	Dst    netip.Prefix // netip.PrefixFrom(addr, 0) for a default route
	Via    netip.Addr   // invalid when the route has no gateway
	Dev    string
	Metric int
	Kind   string // unicast, unreachable, blackhole, prohibit, throw, …
	Proto  string // boot, kernel, static, ra, dhcp, …
}

// Default reports whether this route matches every destination.
func (r KernelRoute) Default() bool { return r.Dst.Bits() == 0 }

// routeTableName is a name for a table id when it has one.
//
// Only the three the kernel defines are named here. A table in our range is
// named for the exit that owns it, and anything else is named by its number —
// inventing a name for a foreign table would be guessing at what its owner calls
// it (the same reason §7.3 reports foreign rules rather than describing them).
func routeTableName(id int) string {
	switch id {
	case 253:
		return "default"
	case 254:
		return "main"
	case 255:
		return "local"
	}
	return fmt.Sprintf("table %d", id)
}

// routesView is `GET /routes` — the whole table, grouped.
type routesView struct {
	// Known reports whether the kernel could be read at all. False on a machine
	// that is not a router and on one where olrd lacks CAP_NET_ADMIN; a client
	// cannot tell "no routes" from "we could not look" without it.
	Known bool `json:"known"`

	// Error is why the read failed, verbatim, so a diagnostic is possible
	// without the daemon log (design.md §4.5: the reason travels with the
	// absence).
	Error string `json:"error,omitempty"`

	Tables []routeTableView `json:"tables"`

	AsOf time.Time `json:"as_of"`
}

// routeTableView is one route table, and who it belongs to.
type routeTableView struct {
	Family int    `json:"family"`
	Table  int    `json:"table"`
	Name   string `json:"name"`

	// Owner is one of main, exit, other. The distinction is the whole reason
	// the page exists: olr must never present its own tables and the box's main
	// table as the same kind of thing (§3.4 don't squat shared state).
	Owner string `json:"owner"`

	// Exit names the exit whose slot this table is, when Owner is exit.
	Exit string `json:"exit,omitempty"`

	// Priority is the RPDB rule priority that selects this table, when olr
	// wrote the rule. Published because §3.2's range is only plannable around
	// if the numbers this box actually used are visible.
	Priority int `json:"priority,omitempty"`

	Routes []routeRowView `json:"routes"`
}

type routeRowView struct {
	Dst     string `json:"dst"`
	Default bool   `json:"default,omitempty"`
	Via     string `json:"via,omitempty"`
	Dev     string `json:"dev,omitempty"`
	Metric  int    `json:"metric,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Proto   string `json:"proto,omitempty"`
}

// viewRoutes groups the kernel's routes into tables and names each one.
//
// Config is read only to name the tables olr owns — the exit a slot belongs to
// and the priority of its rule — never to decide what to show. A table the
// configuration does not know about is still shown, because the box may be
// running a rule and an exit that were deleted from the file, and hiding that
// would make the page agree with the file rather than with the router.
func viewRoutes(routes []KernelRoute, cfg Config, now time.Time) routesView {
	v := routesView{Known: true, Tables: []routeTableView{}, AsOf: now}

	type key struct {
		family, table int
	}
	grouped := make(map[key][]KernelRoute, len(routes))
	for _, r := range routes {
		k := key{r.Family, r.Table}
		grouped[k] = append(grouped[k], r)
	}

	byTable := make(map[int]Exit, len(cfg.Exits))
	for _, e := range cfg.Exits {
		byTable[e.Table()] = e
	}

	for k, rows := range grouped {
		// The local table is the kernel's own bookkeeping: every address on
		// every interface, one route each. It is not a routing decision, and a
		// page that listed it would bury the three tables that are.
		if k.table == 255 {
			continue
		}
		table := routeTableView{Family: k.family, Table: k.table, Name: routeTableName(k.table), Owner: "other"}
		if e, ok := byTable[k.table]; ok {
			table.Owner, table.Exit, table.Priority = "exit", e.Name, e.Priority()
			table.Name = e.Name
		} else if k.table == 254 {
			table.Owner = "main"
		}
		// The route that matches everything first, then the most general, so
		// the table reads top-down the way a packet is matched.
		sort.SliceStable(rows, func(i, j int) bool {
			a, b := rows[i], rows[j]
			if a.Dst.Bits() != b.Dst.Bits() {
				return a.Dst.Bits() < b.Dst.Bits()
			}
			if a.Metric != b.Metric {
				return a.Metric < b.Metric
			}
			return a.Dst.String() < b.Dst.String()
		})
		for _, r := range rows {
			row := routeRowView{
				Dst:     r.Dst.String(),
				Default: r.Default(),
				Dev:     r.Dev,
				Metric:  r.Metric,
				Kind:    r.Kind,
				Proto:   r.Proto,
			}
			if r.Via.IsValid() {
				row.Via = r.Via.String()
			}
			table.Routes = append(table.Routes, row)
		}
		v.Tables = append(v.Tables, table)
	}

	sort.Slice(v.Tables, func(i, j int) bool {
		a, b := v.Tables[i], v.Tables[j]
		if a.Family != b.Family {
			return a.Family < b.Family
		}
		// main first, then olr's tables in rule order, then anybody else's by
		// id. The order a packet meets them: the ordinary table, then the
		// policy rules that override it.
		if a.Owner != b.Owner {
			return ownerRank(a.Owner) < ownerRank(b.Owner)
		}
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		return a.Table < b.Table
	})
	return v
}

func ownerRank(owner string) int {
	switch owner {
	case "main":
		return 0
	case "exit":
		return 1
	}
	return 2
}
