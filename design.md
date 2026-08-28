# open-linux-router — Design

Router software for personal, homelab, and small-org networks. Installed as
**packages on an ordinary Linux distro**, not flashed as a system image.

Status: architecture settled. `dhcp` is the first module under construction; its
design is §11. Open decisions at the bottom.

---

## 1. Goals

- **Package-level.** `apt install` turns an existing Debian box into a router.
  No custom ISO, no image flashing, no reformatting. The box stays a normal
  Linux machine you can put other things on.
- **UX first.** UniFi-grade or better. Simplicity over feature count.
  Explicitly *not* chasing enterprise capability.
- **Progressive disclosure.** The default surface speaks the operator's
  vocabulary — networks, devices, fixed addresses — not the daemon's. Every
  mechanism underneath stays reachable, through an advanced view, a declared
  escape hatch (§3.2 rule 5), or plain Linux. **We hide complexity; we never
  hide capability.** This is the rule that reconciles "UX first" with "we do not
  hide Linux": those are not in tension, they describe the default and the floor.
  Corollary — the name of the daemon we drive is an implementation detail, and
  belongs in `status`, `logs`, and the escape hatch, not on the common path.
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

- Carrier or enterprise features (MPLS, VRF at scale, HA clustering). **HA is
  now structural, not merely descoped:** §10 commits to dnsmasq permanently, and
  dnsmasq has no lease synchronisation, so two boxes can never share DHCP state.
  The same choice caps us at hundreds of clients per segment, not thousands.
  Both ceilings are accepted deliberately; neither is a gap to be closed later.
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

Bounded list. Not expected to grow much. The object they all key off is the
**segment** (§4.4), not the kernel interface.

| | Module | Owns | v1 |
|---|---|---|---|
| **Foundation** | `link` | NICs, bridges, VLANs, bonds, addresses (netlink); **segments** (§4.4) | ✅ |
| | `dial` | WAN: DHCP client, PPPoE, static, LTE; IPv6 PD | ✅ |
| **Services** | `dhcp` | dnsmasq — DHCPv4, DHCPv6 **and RA** (§4.2) | ✅ |
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

Concretely, what the dependents read from `link` is a **segment** (§4.4): a
named network with a subnet and a router address. They do not read interface
names, and they do not restate a subnet `link` already owns.

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

- **dnsmasq** → `dhcp` only
- **unbound** → `dns` only
- **nftables** → each module writes its *own table*, never a shared ruleset

**`dhcp` owns router advertisement, exclusively.** This is not obvious from the
module's name and is easy to violate by accident, so it is stated here rather
than left implied. dnsmasq's `enable-ra` means the DHCP module is what announces
the default route on every IPv6 segment. No other module may run radvd, CoreRAD,
or `IPv6SendRA` — a second RA source on one segment is the same class of failure
as two modules writing one config file, except it presents as intermittent
IPv6 breakage rather than as a merge conflict.

The module is therefore misnamed: it is really *LAN client provisioning*
(DHCPv4 + DHCPv6 + RA). The name stays `dhcp` because that is what an operator
will look for, which is itself an application of the progressive disclosure
rule in §1.

### 4.3 IPv6 is not a module

It is a dimension cutting through `dial` (prefix delegation), `dhcp` (DHCPv6,
RA), `firewall`, and `dns`. Modeling it as a module would be a mistake.

**v1 serves RA with SLAAC + RDNSS, and no DHCPv6 at all.** Two facts decide
this, and both are structural rather than matters of taste:

- **DHCPv6 has no default-gateway option.** There is no equivalent of DHCPv4's
  option 3 and there never was; it was deliberately left out. IPv6 hosts learn
  their next hop from RA and nowhere else. So RA with no DHCPv6 is a fully
  working network, while DHCPv6 with no RA is a network where every client has
  an address and cannot route. RA is not the IPv6 analogue of DHCP — it is more
  fundamental than DHCPv6.
- **Android has never implemented DHCPv6, and Google closed the request as
  "Won't Fix (Intended Behavior)."** Any segment that relies on stateful DHCPv6
  silently loses every Android device on it. For our audience that is most of
  the handsets on the LAN.

So the smallest correct thing is also the most compatible thing. Stateful
DHCPv6 becomes an advanced mode later, labelled with the Android consequence.

### 4.4 The shared objects: segment and device

Two objects are referenced by many modules and owned by one. They are the
reason the module list stays orthogonal without the product feeling like a pile
of daemons.

**A segment is a named network:** name, VLAN, bridge/members, subnet, router
address. `link` owns it, because `link` already owns addresses and a second
owner of the subnet would be exactly the private copy §4.1 forbids.

Modules key off the **segment**, never the kernel interface. `vlan30` is an
implementation detail; `guest` is the thing the operator named, and the thing
that survives being moved to a different bridge. This is what makes a "guest
network" an object with a lifecycle — creatable, renamable, inspectable,
deletable — rather than a write-only recipe whose output is scattered across
four modules. §6.3 composite operations compose *over* segments; they are not a
substitute for having them.

**A device is a client on the network,** keyed by MAC. It is a **join of two
halves**, and keeping them distinct is the whole trick:

| Half | Example | Owned how |
|---|---|---|
| **Identity** — intent | user-given name, fixed address, blocked | ours, stored, revisioned |
| **Presence** — observed | online, current address, last seen | read-through, never stored as truth |

Devices are a *foundation* object, not an operational one: `dhcp` references
them for fixed addresses, `firewall` for per-device rules, `qos` for shaping,
`dns` for names. `firewall` must not have to depend on `dhcp` to write a rule
about a laptop. The apparent cycle — `dhcp` needs identity, the inventory needs
lease data — resolves with §4.1's own rule: identity flows one way as config,
lease facts flow the other way as a subscription. Which module owns the
inventory is **open** (§10).

**Naming.** "Network" is the segment. "Group" is reserved for *sets of devices*
— parental controls, firewall rules, QoS classes — which cut across networks and
are a different concept entirely. Using one word for both forecloses the other.

### 4.5 Own the model, never the state

olr's model is canonical and daemon configuration is a **projection of it**. An
operator meets a *device*, not dnsmasq's idea of a host, unbound's idea of a
name and nftables' idea of an address. Parsing a daemon's format dies at the
module boundary; nothing above it sees "column 4 of the lease file."

That applies in both directions, but with one asymmetry that has to stay sharp:

- **The model and the query interface are ours, always** — including for
  observed things. A lease is an olr type.
- **Caching observed state is fine**, and event-fed caching (`dhcp-script`) is
  better than polling.
- **The cache is never the source of truth.** Upstream is.
- **Every observed object carries `as_of`,** so no surface can imply a freshness
  it does not have.
- **The plan/drift path always reads fresh.** This is the one hard exception.
  §5.4 works because planning compares intent against *reality*; comparing it
  against our own cache would make drift undetectable precisely when it matters.

**The guardrail on all of this:** the model's size is bounded by the *product
surface*, not by the union of the daemons' capabilities. If we do not expose it,
we do not model it — it goes through the declared escape hatch (§3.2 rule 5).
Without that discipline, "olr models everything" becomes a second, worse
configuration language for every daemon we drive.

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

### 5.6 Automatic behaviour must be declared, never inferred

We observe foreign state broadly (§3.4) and we may act on it — but only where
the configuration *says so*. The distinction is not pedantry: it is what keeps
§5.4 working. Drift is "plan the stored intent against reality and see if the
diff is empty," so behaviour that changes without appearing in intent makes
drift undecidable — `olr status` can no longer tell "the operator chose this"
from "we drifted into it."

The pattern, using the DHCP server as the worked example:

```
auto   serve unless another server is already answering on this segment
on     always serve; refuse to start into a detected conflict, loudly
off    never serve
```

`auto` is a declared value, so intent still predicts behaviour and drift stays
decidable. Silently toggling between on and off without a config value that says
to is what is forbidden. Three obligations come with any `auto`:

1. **Hysteresis, one-way.** Once serving, keep serving until told otherwise.
   A foreign server that appears for ten minutes must not make us flap; flapping
   is worse than either steady state. Log every transition.
2. **Effective state is first-class.** `auto` is not sufficient for status — the
   surfaces must report `effective: serving | standing-by` *and the reason*.
   This is a real cost: a new dimension on the CLI, API and UI. It is also
   better than the boolean it replaces.
3. **Faults must not hide inside it.** "Standing by because a peer exists" is
   `auto` working. "Not running because the daemon died" is a fault. If those
   collapse into one status, `auto` becomes the place failures go to hide.

**Refuse, do not disable.** When `on` meets a conflict, we decline to start and
say why. Refusal demands action and leaves intent untouched; auto-disabling
would rewrite intent behind the operator's back and hide a real misconfiguration
— a second DHCP server handing out an unknown gateway is a problem to surface,
not to paper over.

**Be smart at setup, not at runtime.** The moment to resolve a conflict is when
a network is created, as a question the operator has the context to answer
("something is already serving DHCP here — serve anyway, or leave it to them?").
The probe decides what we *suggest*; `auto` is what they pick for the ongoing
behaviour. The two compose. This is also the mode that fits §7's promise that
installing changes nothing: `auto` is the right answer when olr is being added
to a network that already works.

Structural exceptions do not need a config value because they follow from role
rather than from observation: we never serve DHCP on a WAN interface, and never
on an unadopted one.

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
   **`link` must land segments (§4.4) here**, not later — every module after this
   keys off them, so retrofitting the primary key is the one sequencing mistake
   that would be expensive.
2. **Make it a router.** `firewall` (zones/NAT) + `dhcp` + `dns`. Success: a
   client on LAN reaches the internet with no hand-edited config.
3. **Make it safe.** Lockout guard, per-module revisions, impact classification.
4. **Make it visible.** WebUI shell, `clients` inventory, live throughput.
   The device inventory may have to move earlier — see §10 open decision 6.
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
6. **Who owns the device inventory?** (§4.4) Devices are a foundation object —
   `firewall` and `qos` reference them and must not depend on `dhcp` to do it —
   but `clients` is currently a v2 read-mostly module. Either it is promoted to
   v1 and grows an intent half, or something else owns identity. This blocks the
   `dhcp` fixed-address surface, since a fixed address is a property *of a
   device*. **The most urgent of these.**
7. **Does the device list read ARP/ND in v1?** A statically-addressed printer
   never speaks DHCP, so a lease-derived list silently omits it and operators
   will notice. Either read the neighbour table too, or name the screen "DHCP
   clients" and be honest that it is not an inventory.
8. **Does creating a network default DHCP on?** Leaning yes — a LAN without
   DHCP is the unusual case, and §5.1 immediate-apply makes it cheap to undo.
9. **What is a segment, exactly?** (§4.4) Its field set is now the most
   load-bearing schema in the project: `link`, `dhcp`, `dns`, `firewall` and
   later `wifi` all key off it. Worth designing deliberately rather than
   growing.

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
- **DHCP backend: dnsmasq. Permanently — not "for v1".** dnsmasq serves DHCPv4,
  DHCPv6 **and RA** in one daemon; Kea does no RA at all and would need radvd as
  a third daemon — decisive given IPv6 is a dimension, not a module (§4.3).
  Two obligations follow: the module **must** render `port=0` or dnsmasq
  silently becomes a second resolver and violates §4.2; and the lease-to-DNS
  publish path §4.2 gives up has to be rebuilt over `dhcp-script`.

  Three alternatives were considered properly and are now closed:

  - **Kea.** The original objection was licensing — trixie ships Kea 2.6.3,
    predating the July 2025 Kea 3.0 hook relicensing. **That objection has since
    expired: trixie-backports carries Kea 3.0.3.** The decision stands anyway,
    on daemon count (Kea is two or three processes to dnsmasq's one) and on
    still needing a separate RA source. Recorded so the next reader does not
    re-derive it from the stale premise.
  - **A DHCP library in-process** (`insomniacslk/dhcp`). Real prize — it would
    delete the entire render/plan/apply file machinery, the reload-vs-restart
    distinction, and lease-file parsing. Rejected on scope and blast radius:
    the library is packet marshalling plus a handler signature, so allocation,
    lease persistence, conflict detection and the DHCPv4 interop tail are all
    ours, and **RA is not in it at all**. A bug takes the LAN down. Our
    differentiation is UX, API coherence and package-level install — not
    protocol implementation.
  - **Owning RA alone** (`mdlayher/ndp`, ~1.2k lines, stateless, bounded blast
    radius — a broken RA loses IPv6 while IPv4 keeps routing). Genuinely the
    better half to own if we owned one, and the inverse of what the
    "use a DHCP library" framing suggests. Declined under the same "no custom
    protocol code" line; dnsmasq's `constructor:` handles prefix-following and
    the renumbering deprecation dance, which is the hard part.

  **Consequences of "permanently":** the `Backend` interface is deleted — by
  §3.1's own test, nothing iterates over backends and there is exactly one — and
  we lean *into* dnsmasq's idioms (tags, `dhcp-hostsdir`, `ra-param`,
  `dhcp-script`) rather than hedging toward a portable abstraction. HA and the
  thousands-of-clients ceiling become permanent product boundaries (§1
  non-goals), not gaps to close.

- **The segment is the unit of network** (§4.4), owned by `link`, and modules
  key off it rather than off kernel interface names. Considered and rejected: a
  separate `network` module, which would own a name and a subnet that `link`
  must know anyway — two sources for one fact, which §4.1 forbids. Consequence
  for `dhcp`: the pool's primary key is a segment, its range is derived from the
  segment's prefix by default, and most of the cross-module validation §5.3.1
  celebrates stops being *needed* for the common case, because the subnet is
  given rather than asserted and re-checked.

- **Progressive disclosure** (§1) as the rule reconciling "UX first" with "we do
  not hide Linux." They describe the default and the floor, not a tension.
- **Test hardware.** Previously open. Resolved by workflow rather than
  purchase: code is authored and built on x86_64 Linux, cross-checked for
  arm64, and end-to-end tested by hand on real hardware. Consequence for
  layout — keep renderers, lease parsing, schema, config store, and validation
  free of Linux-only imports so they are unit-testable anywhere; isolate
  netlink, nftables, and systemd behind an interface.

---

## 11. `dhcp` module design

First module under construction. It drives dnsmasq (§10) and owns DHCPv4,
DHCPv6 and RA (§4.2).

### 11.1 Object model

Two nouns, both defined in §4.4, neither of them owned here:

- **Network settings** attach to a **segment**. `link` owns the segment; `dhcp`
  owns what it serves there.
- **A fixed address** is a property **of a device**, not a standalone row. It is
  set from the device list, not by typing a MAC into a form. The difference
  between those two sentences is the difference between the OPNsense experience
  and the UniFi one.

Modelling fixed addresses as a flat MAC→IP table is the failure mode to avoid;
it is what forces every workflow to start from a hardware address the operator
has to go and find. Device inventory ownership is §10 open decision 6, and it
blocks this surface.

### 11.2 Per-segment settings

| Field | Default | Note |
|---|---|---|
| `dhcp` | `on` | `auto \| on \| off` per §5.6 |
| `range` | derived | from the segment prefix; explicit overrides |
| `lease` | 12h | |
| `dns` | the router | |
| `domain` | the base domain | |
| `ipv6` | `on` | SLAAC + RDNSS (§4.3); `stateful` is advanced and labelled |

**The acceptance criterion for the UX goal is that creating a network takes only
a name:**

```
olr net add iot
```

deriving the next free /24, gateway `.1`, DHCP on, a range that leaves a low
block free for statics, DNS = the router, IPv6 on, lease 12h. Everything
overridable, nothing required. If that command needs four flags, §1's "UX first"
has not been met — this is a test, not an aspiration.

The derived range matters for a second reason: the real collision hazard is not
a fixed address inside the dynamic range, it is a **statically-configured device**
inside it, which DHCP will hand to someone else. dnsmasq has no exclusion
primitive, so the only defence is a range that deliberately leaves a static block
free. Deriving it gives that for free; asking the operator to type it does not.

### 11.3 The guarantee the file layout exists to buy

dnsmasq re-reads its hosts and options directories on SIGHUP but never re-reads
its configuration file. Reservations and per-segment options therefore live in
directories, and that asymmetry is the whole reason for the layout. Stated as a
promise, because it is user-facing and testable:

> Adding, changing or removing a fixed address **never interrupts service**.
> Changing a range restarts the server, but clients keep their addresses.
> Disabling drops clients.

Mapped onto the §5.3.3 impact vocabulary, in client terms rather than daemon
terms:

| Impact | What a client on the LAN experiences |
|---|---|
| `none` | nothing |
| `reload` | nothing |
| `restart` | nothing; a request arriving mid-restart is retried |
| `disruptive` | loses the address it holds |

`disruptive` is computed from the **live lease database**, not from comparing
config fields — a range change is disruptive exactly when someone currently
holds an address the new range no longer covers. That is what makes it a fact
rather than a guess.

### 11.4 Scope

| | | |
|---|---|---|
| **v1** | per-segment `auto\|on\|off`, derived range, fixed addresses, lease/DNS/domain, IPv6 SLAAC+RDNSS | |
| | pool usage, foreign-DHCP-server detection, recent DHCP events | "why did this device not get an address" is the single most common support question; these three answer it |
| **v2** | PXE / netboot (`dhcp-boot`) | high demand in *this* audience — Proxmox, netboot.xyz |
| | deny DHCP to a device (`dhcp-ignore`) | belongs with inventory blocking, not here |
| | option 121 static routes, stateful DHCPv6 | escape hatch until then |
| **Never** | DHCP relay | different object shape; settled once, not revisited |
| | per-vendor-class options, multiple pools per segment | escape hatch, permanently |

### 11.5 Known gaps

- **Post-apply verification.** A dead DHCP server is invisible for hours and
  then breaks everything at once when leases expire. §5.5's lockout guard covers
  *admin reachability*, not this. Nothing currently confirms the daemon is alive
  and bound after an apply, and it should.
- **`dhcp-script` is not wired up.** It is the only real IPC dnsmasq offers, and
  it is what turns leases from a polled file into an event stream — the live
  device list (§6.3 SSE), the §4.1 lease publish to `dns`, and the event history
  above all depend on it. Highest-leverage single item in the module.
- **Drift is byte-comparison against rendered files,** so changing a comment in
  the renderer marks every deployed install as drifted and schedules a restart on
  next apply. Inherent to the approach; needs a deliberate answer before there
  are installs in the field.
