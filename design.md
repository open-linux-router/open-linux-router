# open-linux-router — Design

Router software for personal, homelab, and small-org networks. Installed as
**packages on an ordinary Linux distro**, not flashed as a system image.

Status: architecture settled, no code yet. Two open decisions at the bottom.

---

## 1. Goals

- **Package-level.** `apt install` turns an existing Debian box into a router.
  No custom ISO, no image flashing, no reformatting. The box stays a normal
  Linux machine you can put other things on.
- **UX first.** UniFi-grade or better. Simplicity over feature count.
  Explicitly *not* chasing enterprise capability.
- **API-first, genuinely open.** The HTTP API is the substrate, not a wrapper.
  WebUI, CLI, and MCP are all equal clients of it.
- **Agent-manageable.** MCP server plus skills, so an agent can inspect and
  change the network through the same contract a human uses.
- **Open hardware.** Any x86/arm64 box with two NICs. No vendor lock.
- **Linux is the extension surface.** If you need something we don't ship, you
  don't write an `olr` plugin — you `apt install` it, drop in a systemd unit, or
  add your own nftables table. The distro *is* the plugin system, and it is
  better than any plugin API we could design. Our job is to stay out of its way.

### Non-goals

- Carrier or enterprise features (MPLS, VRF at scale, HA clustering).
- Managing third-party APs/switches as a fleet controller. Maybe later; not v1.
- A plugin API or third-party module ecosystem — see the extension surface
  above. Programmatic access is the HTTP API and MCP.
- Reimplementing what the distro already does well (supervision, logging,
  updates, DHCP/DNS/routing daemons). Wrap or defer.

---

## 2. Positioning

Everything adjacent is one of three things:

| Category | Examples | Why it isn't this |
|---|---|---|
| Image-level distros | OpenWrt, VyOS, OPNsense, pfSense, IPFire | You install *their* OS. VyOS is Debian-based but still ships as an ISO with an engineer-facing CLI. |
| Controllers for other vendors' hardware | OpenWISP, RadiusDesk, UniFi Network | Package-level with decent UX, but they manage *remote APs*, not the box they run on. |
| Blog-post DIY | nftables+dnsmasq guides | Hand-edited configs. No UI, no API, no safety net. |

The unoccupied position: *"`apt install` makes this Debian box a real router,
with consumer-grade UX and an API-first control plane."*

---

## 3. Architecture

Core is thin. It is a route table, a schema aggregator, auth, config storage,
and a lockout guard. Nothing more.

```
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│   Web UI     │  │   olr CLI    │  │  MCP server  │   all generated from
│    (SPA)     │  │              │  │   + skills   │   the same schema
└──────┬───────┘  └──────┬───────┘  └──────┬───────┘
       └─────────────────┼─────────────────┘
                         │  HTTP/JSON, unix socket + optional TLS
                  ┌──────▼──────────────────────────┐
                  │  olrd  — routes, schema, auth,   │
                  │        config store, guard       │
                  └──────┬──────────────────────────┘
        ┌────────────┬───┴────────┬────────────┬───────────┐
     ┌──▼──┐     ┌───▼──┐    ┌────▼───┐   ┌────▼───┐  ┌────▼───┐
     │link │     │ dial │    │  dhcp  │   │  dns   │  │firewall│  …
     └──┬──┘     └───┬──┘    └────┬───┘   └────┬───┘  └────┬───┘
     netlink     pppd/dhcpcd   dnsmasq      unbound     nftables
```

### 3.1 The critical inversion: library, not framework

Modules call core helpers. **Core does not call into modules through an
abstraction.** There is no `Module` interface.

This was tested against one question — *does anything actually iterate over
modules?* — and the answer is essentially no:

| Candidate iterator | What it really needs |
|---|---|
| `olr --help` module list | a list of strings |
| OpenAPI spec | concatenation of per-module schema fragments |
| MCP tool definitions | generated from that document |
| UI forms | consumes the document over HTTP; never sees a Go type |
| dependency-ordered apply | not needed — see §5, no cross-module transactions |
| `olr status` | fan-out over a route table |

So the uniformity lives in a **schema document**, not in Go types.

### 3.2 Module conventions

Four rules. No abstraction, no registry, no codegen framework.

1. A module owns a config namespace and its own file under
   `/etc/open-linux-router/`.
2. A module registers HTTP routes under `/api/<module>/`.
3. A module's config is a **tagged Go struct**; JSON Schema is derived from it
   by reflection. That struct is the single source for CLI flags, REST body,
   UI form, and MCP tool definition.
4. Shared verb vocabulary so the CLI stays predictable:
   `show`, `set`, `add`, `rm`, `status`, `logs`, `enable`, `disable`.
5. Every module exposes a **declared escape hatch** — a config field passed
   through to the underlying daemon or ruleset verbatim
   (`dns.extra_unbound_conf`, `firewall.raw_nft`). Declared in our config and
   rendered by us, so it stays single-source and revisioned, unlike editing the
   file out of band. We make the common 95% pleasant; we do not hide Linux.

Modules are mounted explicitly — the list is bounded, so it is a literal list:

```go
// cmd/olrd/main.go
core.Mount("link",     link.Handler(),     link.Schema)
core.Mount("dial",     dial.Handler(),     dial.Schema)
core.Mount("dhcp",     dhcp.Handler(),     dhcp.Schema)
// …
```

Because the contract is *HTTP routes + a schema fragment*, moving a module out
of process later is a one-line change and touches nothing else:

```go
core.Proxy("dns", "/run/olr/dns.sock", schemaFromSocket)
```

Start everything in-process. That decision is now cheap to revisit.

### 3.3 Shared library

What core offers modules (they call it, not the reverse):

- config file read/write with per-module revision history
- config templating and atomic file replacement
- systemd unit management for supervised daemons
- nftables table helpers (own table per module, documented hook priorities)
- netlink helpers
- structured logging and event publication

### 3.4 Good citizen rules

Because Linux is the extension surface (§1), the box must remain an ordinary
Linux machine. These are hard rules, not preferences.

**Read broadly, write narrowly.** Observe the whole system honestly — every
interface, route, and nftables table, including ones we don't manage. Write only
to what we own. `olr status` reporting *"3 foreign nftables tables present"* is
a feature; pretending we are the only actor is a bug.

- **`nft flush ruleset` is banned.** It silently kills Docker, podman, libvirt,
  and k8s networking. We create, replace, and delete *our own tables by name*,
  never the ruleset.
- **Documented hook priorities**, so a user can deliberately insert their own
  table before or after ours.
- **Adopt-only.** We never touch an interface that wasn't explicitly adopted
  (§7).
- **No machine-wide service disabling.** Disable NetworkManager *on adopted
  interfaces*, not globally.
- **Generated files are additive** — our own directory, `include`-d into the
  real daemon config, never replacing user files. Ownership header on each.
- **Don't squat shared state.** `/etc/resolv.conf`, the main route table, and
  sysctls are touched only when a module explicitly owns that concern.

**Reuse the distro instead of rebuilding it.** This deletes real scope:

| We don't build | Because Linux has |
|---|---|
| a process supervisor | systemd |
| a logging system | journald |
| an updater | apt |
| a plugin runtime | apt + systemd units |
| DHCP / DNS / routing implementations | dnsmasq or Kea, unbound, bird |

---

## 4. Modules

Bounded list. Not expected to grow much.

| | Module | Owns | v1 |
|---|---|---|---|
| **Foundation** | `link` | NICs, bridges, VLANs, bonds, addresses (netlink) | ✅ |
| | `dial` | WAN: DHCP client, PPPoE, static, LTE; IPv6 PD | ✅ |
| **Services** | `dhcp` | dnsmasq or Kea — **DHCP only** | ✅ |
| | `dns` | unbound — **DNS only** | ✅ |
| | `firewall` | nftables `olr_filter`, `olr_nat` — zones, rules, NAT, forwards | ✅ |
| | `qos` | tc — CAKE / fq_codel, per-device shaping | |
| | `routing` | static + policy routes; later bird (BGP/OSPF) | |
| | `vpn` | wireguard — remote access, site-to-site | |
| | `wifi` | hostapd — only if the box has radios | |
| **Operational** | `clients` | read-mostly: leases + ARP/ND + stations + conntrack | |
| | `system` | hostname, time, admin users, updates, backup, logs | ✅ |

### 4.1 Dependencies form a DAG

`link` is the foundation; everything references it. A module reads another
module's state **through that module's public API** — it never keeps its own
copy. Single ownership of every fact, so drift is structurally impossible.

```
link ──┬─→ dial ──┬─→ dns      (upstream resolvers)
       │          ├─→ qos      (WAN iface + rate)
       │          ├─→ firewall (NAT egress iface)
       │          └─→ routing  (default route, PD prefix)
       ├─→ dhcp ──┬─→ dns      (lease hostnames, one-way publish)
       │          ├─→ clients
       │          └─→ qos
       ├─→ firewall
       ├─→ routing
       └─→ wifi
```

**When a cycle appears, invert one side into a fact subscription.** The real
case: `dhcp` wants to register lease hostnames in `dns`, and `dns` wants lease
data. Direction is fixed — `dhcp` *publishes* leases, `dns` *subscribes*.
(This is precisely why dnsmasq fuses the two; see §4.2.)

### 4.2 One daemon, one owner

Exactly one module owns any given daemon or config file. dnsmasq can serve both
DHCP and DNS — if `dhcp` and `dns` both wrote `dnsmasq.conf`, module isolation
would break at the worst possible place. So:

- **dnsmasq (or Kea)** → `dhcp` only
- **unbound** → `dns` only
- **nftables** → each module writes its *own table*, never a shared ruleset

### 4.3 IPv6 is not a module

It is a dimension cutting through `dial` (prefix delegation), `dhcp` (DHCPv6,
RA), `firewall`, and `dns`. Modeling it as a module would be a mistake.

---

## 5. Apply semantics

### 5.1 Immediate apply

`olr dns set upstream=1.1.1.1` takes effect on return — like `ip` and `nft`,
not like VyOS `commit`. No staged commit, no global apply. The GUI follows:
toggling a switch applies instantly, no "Apply changes" bar.

### 5.2 No cross-module atomicity

Each module applies independently and keeps its own revision history
(`olr dhcp rollback`). There is no two-phase commit.

Rationale — the kernel already provides atomicity where risk is highest. An
nftables ruleset load is a single transactional netlink batch: all-or-nothing,
no partial ruleset ever visible. Individual netlink ops are atomic per-op. A
userspace 2PC layer would mostly protect the *low*-risk seams while the
high-risk one is already covered. Bad trade for the complexity.

### 5.3 What replaces it — three cheap mechanisms

1. **Cross-module validation before apply.** Atomic *validation* is far weaker
   than atomic *apply* — pure reads, no coordination. Before `dhcp` changes a
   pool, check it falls inside `link`'s subnet for that interface. Catches most
   real breakage (typos, half-finished renumbering) before anything changes.
   Highest value per line in the whole design.
2. **Idempotent re-apply instead of rollback.** If a multi-step change fails
   halfway, don't unwind — leave it partial, report exactly which steps landed,
   and let a re-run finish the job. More honest and more debuggable than a
   silent revert.
3. **Impact classification.** Every diff reports
   `none | reload | restart | disruptive`, so the UI can say *"this will drop
   all LAN connections"* instead of showing a spinner. Cheap now, impossible to
   retrofit.

### 5.4 Drift is free

Each module's plan step is pure and reads **actual system state**, not a cached
copy of intent. So "have we drifted?" is just: plan against unchanged intent and
see if the diff is empty. `olr status` is that, run across modules. No separate
health machinery.

Two things were hiding under the word "health":

- **drift** — applies to every module, derived as above
- **is the supervised daemon alive** — only daemon-backed modules
  (`dhcp`, `dns`, `dial`, `wifi`, dynamic `routing`); mostly a systemd query

### 5.5 Lockout guard

Not a transaction — a dead-man's switch. `olrd` remembers what changed in the
last 90 seconds; if admin reachability is lost and nobody confirms, it reverts
those modules. One **global** reachability probe, not per-module health.

The failure this prevents is not an enterprise failure. You are on your laptop,
on the LAN, changing the LAN. Your session dies mid-sequence and now you cannot
run the command that would fix it. Every homelab router does this eventually.

---

## 6. Surfaces

All four derive from the same per-module schema. **Derive, not fetch** — the
schema is a build-time source for the CLI and a runtime document for the REST,
UI, and MCP surfaces. See §10 resolved, *static command tree*.

### 6.1 CLI — `olr`, an `aws`-style hub

```
olr <module> <verb> [args]        olr dns show
                                  olr dhcp add reservation --mac … --ip …
                                  olr firewall rm rule 4

olr status                        aggregate: drift + daemon liveness
olr diff                          pending/drifted, per module
olr history | rollback            per-module revisions
olr adopt <iface> | release       take/hand back interface ownership

olr daemon start | stop | status  manage olrd itself — see below
```

The command tree is registered in Go, so `olr --help` is truthful with `olrd`
stopped and there is no schema to fetch, cache, or invalidate.

**Two tiers.** Most of `olr` is a client of `olrd` on equal footing with the
WebUI and MCP (§1). But `olr daemon …` manages `olrd` itself, so it cannot be
one — starting a daemon cannot be an HTTP call to that daemon, and `status` has
to answer when it is wedged. Those commands talk to systemd directly and are
grouped separately in `--help`. The "equal clients" rule in §1 governs
*configuration*; daemon lifecycle is not configuration.

The shared verb vocabulary (§3.2 rule 4) is enforced in code, not documented:
constructing a command with a verb outside the vocabulary panics at startup.
Verb drift across modules is invisible in review and obvious at startup.

### 6.2 REST API

`/api/<module>/…` over a unix socket, optional TLS for remote. OpenAPI derived
from the config structs. Read surface includes **observed resources** (leases,
WAN state, stations) declared alongside config but never stored or revisioned.

### 6.3 WebUI

SPA embedded in the binary, a pure client of the API.

**Composite tasks must live in core, not the UI.** "Set up a guest network"
touches `link` (VLAN), `dhcp` (pool), `dns` (scope), `firewall` (isolation),
`wifi` (SSID). If that orchestration lives in the WebUI, the CLI, the API, and
agents cannot do it — which rebuilds the exact UniFi limitation we're trying to
beat. Composite operations are first-class core operations with their own
routes; recipes state their own step order explicitly.

Live data (throughput, station lists, DNS query log) streams over SSE.

### 6.4 MCP + skills

MCP tools generated from the same schema. Skills shipped as markdown in-repo.
Because plans are pure and diffs are inspectable, an agent can propose a change,
a human can review the diff, and it applies without a bespoke code path.

---

## 7. Adoption and reversibility

**Installing the package must never break your SSH.** Install alone changes
nothing.

- `olr adopt <iface>` — take ownership from NetworkManager/systemd-networkd,
  recording prior state
- `olr release <iface>` — hand it back
- generated files live under `/etc/open-linux-router/rendered/`, included into
  the real daemons' configs; user files are never hand-edited
- every generated file carries an ownership header

---

## 8. Packaging

- **Debian 13 (trixie)** primary, amd64 + arm64. Own apt repo.
- Ubuntu LTS next. Any systemd + nftables distro as a secondary goal.
- `open-linux-router` (core + v1 modules) with optional
  `olr-module-{qos,vpn,wifi,routing}` splits, since selective install is a real
  payoff of package-level over image-level.

**Stack:** Go for core and modules — static binary, good netlink/nftables
libraries, trivial `.deb`, clean arm64 cross-compile. TypeScript SPA embedded
in the binary.

| | Choice | Note |
|---|---|---|
| Language | Go, `go.mod` floor **1.23** | `CGO_ENABLED=0`; nothing needs a newer language version |
| HTTP | stdlib `net/http` + 1.22 `ServeMux` | core is thin (§3); a framework would leak into the API contract |
| CLI | `spf13/cobra` | static tree, §6.1 |
| Schema | `invopop/jsonschema`, draft 2020-12 | OpenAPI 3.1 is a superset, so one dialect serves REST, MCP, and UI |
| netlink | `vishvananda/netlink` | |
| nftables | `google/nftables` | direct netlink, no `nft` binary |
| systemd | `coreos/go-systemd/dbus` | §5.4 daemon liveness |
| Logging | `log/slog` | stdlib, structured (§3.3) |
| `.deb` | `nfpm` | single binary, cross-arch, no Ruby |

Six direct dependencies for v1. For a router that is a feature.

---

## 9. Milestones

1. **Get a box online.** `link` + `dial` + minimal core (config store, routes,
   `olr` skeleton). Success: WAN up via DHCP and PPPoE, `olr status` truthful.
2. **Make it a router.** `firewall` (zones/NAT) + `dhcp` + `dns`. Success: a
   client on LAN reaches the internet with no hand-edited config.
3. **Make it safe.** Lockout guard, per-module revisions, impact classification.
4. **Make it visible.** WebUI shell, `clients` inventory, live throughput.
5. **Make it programmable.** OpenAPI publication, MCP server, skills.
6. **Then:** `qos`, `vpn`, `routing`, `wifi`.

---

## 10. Open decisions

1. **`access` module split.** Assumed here as three concerns —
   `firewall` (zones/rules/NAT/forwards), `clients` (inventory, blocking,
   parental), `auth` (who may log into olr, folded into `system`).
   Confirm this matches the intent.
2. **Immediate apply** (§5.1) — assumed yes; confirm.
3. **Single box or multi-node?** v1 is single box either way. But if managing
   APs/switches is the destination, the schema needs a node dimension now
   rather than later.
4. **Wi-Fi on-box or bring-your-own AP?** On-box hostapd is a driver and
   regulatory rabbit hole. Assuming wired router + separate APs on a VLAN trunk
   is far cheaper and matches most homelab setups.
5. **Revision storage.** Numbered snapshots under
   `/etc/open-linux-router/revisions/<module>/`, or a git repo in
   `/etc/open-linux-router`? Git gives `history`/`rollback` and real diffs
   nearly free, at the cost of a git dependency in the control plane and odd
   behaviour if a user pokes at it. Leaning snapshots.

### Resolved

- **Coexistence stance.** Previously open. Settled by §1: if the answer to
  *"I need X"* is *"install X the Linux way,"* then never breaking X is a
  requirement, not a courtesy. We take exclusive ownership of a narrow, declared
  scope and are rigorously non-invasive outside it (§3.4).
- **Third-party modules.** Not needed as a plugin API — the extension surface is
  the distro. Modules stay a bounded in-tree list, mounted explicitly (§3.2).
- **Language: Go.** Confirmed against Rust rather than assumed. This project is
  ~90% config rendering, schema plumbing, HTTP, and process supervision and ~10%
  netlink — Rust's advantages land on the 10% that §3.4 gives away to existing
  daemons. Two things decided it. Runtime reflection makes one tagged struct
  drive CLI, REST, UI, and MCP (§3.2 rule 3); `schemars` is compile-time, so the
  surfaces would need a hand-written interpreter. And `google/nftables` is pure
  Go over netlink, where Rust's `rustables` wraps libnftnl — a C dependency that
  would compromise both the static binary and the arm64 cross-build.
- **Static command tree.** The CLI does not fetch a schema from `olrd` at
  runtime. Alternative considered and rejected: fetch-and-cache, which would let
  `olr` drive a newer or remote daemon but makes `olr --help` depend on the
  daemon being reachable. See §6.1.
- **CLI is two-tier.** `olr daemon …` is below the API, everything else is a
  client of it (§6.1). §1's "equal clients" governs configuration, not lifecycle.
- **Config format: JSON.** Round-trips through the same struct tags as the REST
  body and the reflected schema, so there is no second dialect and no mapping
  layer. TOML would be friendlier to read and was rejected for that duplication.
  Three consequences:
  - `time.Duration` needs a wrapper type, or a 12h lease serialises as
    `43200000000000`.
  - JSON has no comments, so the §3.4 ownership header becomes a `"$schema"`
    key pointing at the published schema — which also makes the file
    editor-validatable. Generated *daemon* configs keep a real comment header;
    dnsmasq and nftables formats both support one.
  - Reflection derives `required` from the absence of `omitempty`, not from a
    tag. So core publishes two projections of every module schema: full for
    PUT, relaxed for PATCH and `olr set`. Without this a single-field update
    fails validation against its own schema.
- **DHCP backend: dnsmasq, not Kea.** dnsmasq serves DHCPv4, DHCPv6 **and RA**
  in one daemon; Kea does no RA at all and would need radvd as a third daemon —
  decisive given IPv6 is a dimension, not a module (§4.3). Also: trixie ships
  Kea 2.6.3, which **predates** the July 2025 Kea 3.0 hook relicensing, so
  `host_cmds`/`subnet_cmds` are still commercially licensed there, and pulling
  ISC's own repo would break the plain-`apt` promise in §1. Kept behind a
  backend interface; Kea is the v2 upgrade path for lease DB and HA.
  Two obligations follow: the module **must** render `port=0` or dnsmasq
  silently becomes a second resolver and violates §4.2; and the lease-to-DNS
  publish path §4.2 gives up has to be rebuilt over `dhcp-script`.
- **Test hardware.** Previously open. Resolved by workflow rather than
  purchase: code is authored and built on x86_64 Linux, cross-checked for
  arm64, and end-to-end tested by hand on real hardware. Consequence for
  layout — keep renderers, lease parsing, schema, config store, and validation
  free of Linux-only imports so they are unit-testable anywhere; isolate
  netlink, nftables, and systemd behind an interface.
