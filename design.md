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
  Corollary — the name of the backend we drive is an implementation detail, and
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
  The same choice caps us at hundreds of clients per group, not thousands.
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

Five rules. No abstraction, no registry, no codegen framework.

1. A module owns a config namespace and its own file under
   `/etc/open-linux-router/`.
2. A module registers HTTP routes under `/api/<module>/`.
3. A module's config is a **tagged Go struct**; JSON Schema is derived from it
   by reflection. That struct is the single source for CLI flags, REST body,
   UI form, and MCP tool definition.

   **A type whose Go shape differs from its JSON shape must say so, or the
   schema publishes a lie.** `netip.Addr` is a struct of unexported fields that
   marshals as a *string*; reflected naively it becomes
   `{"type":"object","properties":{}}`, and every surface derived from that
   document is then wrong about the wire format — while the code keeps working,
   so nothing fails until a UI form or an MCP tool is built on it. Core maps the
   stdlib cases centrally and applies one general rule — *a type that marshals
   as text is a JSON string* — so a module only needs its own
   `JSONSchema()` when it knows something the rule cannot: which strings are
   legal, or that a field is an enum. This is the same trap §10 names for
   durations, one level up: a wrapper that fixes the *encoding* still publishes
   `integer` unless the schema is fixed too.
4. Shared verb vocabulary so the CLI stays predictable:
   `show`, `set`, `add`, `rm`, `status`, `logs`, `enable`, `disable`.
5. Every module exposes a **declared escape hatch** — a config field passed
   through to the underlying backend or ruleset verbatim
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

Because the contract is *HTTP routes + a schema fragment*, a module's control
code could in principle be proxied to another process (`core.Proxy("dns",
"/run/olr/dns.sock", …)`) rather than mounted. **This hatch exists and is
deliberately unused** — §3.5 argues against ever taking it, and it is *not* the
mechanism by which backends run out of process. Backends were never in process
to begin with. The two are easy to confuse and mean opposite things:

| | Lives where | Why |
|---|---|---|
| Module control code (render, plan, validate) | **in `olrd`** | §3.5 |
| Backend (dnsmasq, unbound, ours) | **its own unit, always** | §3.5 |

### 3.3 Shared library

What core offers modules (they call it, not the reverse):

- config file read/write with per-module revision history
- config templating and atomic file replacement
- systemd unit management for **backends** (§3.5)
- nftables table helpers (own table per module, documented hook priorities)
- netlink helpers
- structured logging and event publication
- the global apply lock (§3.6) — modules do not take their own

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
| DHCP / DNS / routing implementations | dnsmasq, unbound, bird |

### 3.5 Process model

How many processes are **ours**, and what is allowed to live inside them. The
two get conflated easily, because most of the processes on a running box are
not ours at all.

```
  olrd ─────────────────────────────── 1 process, ours, resident
   │
   ├─ link      → netlink               (no process)
   ├─ firewall  → nftables netlink      (no process)
   ├─ qos       → tc netlink            (no process)
   ├─ clients   → reads                 (no process)
   ├─ system    → D-Bus to systemd      (no process)
   │
   ├─ dhcp      → dnsmasq  ──────────── 1  ours to supervise,
   ├─ dns       → unbound  ──────────── 1  not ours to write
   ├─ dial      → pppd / dhcpcd ─────── 0–1 per WAN
   ├─ wifi      → hostapd  ──────────── 1 per radio (later)
   └─ routing   → bird     ──────────── 1 (later)

  transient:   olr guard expire   armed only during the §5.5 window
               dhcp-script        per lease event, forked by dnsmasq
```

`dhcp` and `dns` already have their own processes. **The module is not the
server** — it is the controller that renders config and reloads the backend.

**The invariant that decides everything else:**

> **`systemctl restart olrd` must never drop a packet, expire a lease, or break
> a session.**

Which gives a decidable test for where any new code goes:

**Does it have to keep running while `olrd` is stopped?** Yes → its own binary
and its own unit. No → a package inside `olrd`.

The test discriminates correctly on the awkward cases. The netlink watcher that
feeds §6.3's live UI runs continuously inside `olrd`, and that is fine: if
`olrd` dies you lose live updates, not connectivity.

Generalised, this is the same rule as §5.5: **nothing that must survive `olrd`'s
death may live in `olrd`.** Data plane → its own unit. Guard state → a disk
snapshot plus a transient timer. One principle, two mechanisms.

**Modules stay in `olrd`.** Splitting module *control* code into per-module
processes was considered and rejected on three counts:

1. **It breaks §5.3.1, the highest-value mechanism in the design.** "Check the
   pool falls inside `link`'s subnet" is a function call on a pure read. Across
   processes it becomes an HTTP round trip that can fail, plus a TOCTOU window
   in which `link` can change between validate and apply.
2. **The lease→DNS path becomes two hops per lease.** §4.2 gives up dnsmasq's
   built-in fusion and rebuilds it over `dhcp-script`; that fires on every
   renewal.
3. **Startup ordering becomes a systemd graph.** In-process, `link` before
   `dial` before `dns` is the literal list in §3.2. Across processes it is
   `After=`/`Requires=` over six units — and §3.1 found that *nothing iterates
   over modules*. Splitting manufactures exactly the iteration requirement that
   finding removed.

The usual argument for splitting — fault isolation — does not apply. The risky
code is in the backends, and those are **already** separate processes **already**
supervised by systemd.

**Backends are separate processes even when we write them.** The rule is not
"reuse the distro"; it is stronger, and survives us writing our own
implementation. If `olr-dhcpd` ever exists it is a binary and a unit, never
a goroutine, because:

- **A goroutine server means building a process supervisor inside `olrd`** —
  restart policy, backoff, liveness — which is precisely what §3.4 refuses to
  build. As a unit it is free, and §5.4's "is the backend alive" stays one
  uniform D-Bus query with no special case.
- **It is the most dangerous code on the box.** A DHCP server binds `:67` and
  parses unauthenticated packets from every device on the LAN. `olrd` holds
  `CAP_NET_ADMIN` and the admin API. They must not share an address space.
- **§3.2's module rules stay uniform** — render config, reload unit, read
  status. One shape, no exceptions.

**Corollary, and the easy mistake:** we drive our own backend exactly the way we
drive dnsmasq — a rendered config file and a signal — **not** a private RPC
channel just because we happen to own both ends. A private channel reintroduces
two module shapes through the back door, and it costs two things for free: the
backend stops being independently runnable and debuggable, and §3.2 rule 5's
escape hatch stops working for it.

### 3.6 Concurrency and privilege inside `olrd`

**One global apply lock.** Every config write across every module takes it. Not
per-module.

The simplicity argument is obvious; the correctness one is better. A global lock
is what makes §5.3.1 sound — validate `dhcp`'s pool against `link`'s subnet,
then apply, with nothing able to move `link` in between. Per-module locks
reintroduce the TOCTOU that validation exists to prevent. The contention budget
is a human clicking a toggle; there is no throughput requirement to trade
against.

**Reads never take it.** §5.4 already requires reads to hit actual system state
— netlink, nftables, the lease file, D-Bus. There is nothing to serialise.

**Apply must be bounded, and this is what keeps the global lock viable.** Apply
is render, reload, return. It **never** waits for convergence: `dial` returns
when `pppd` is started, not when the session is up. Convergence is observed
state (§6.2), streamed over SSE. Without this rule one 30-second PPPoE dial
freezes every other module.

This does **not** forbid post-apply verification (§11.5). "Did the unit come up
and bind?" is bounded and belongs inside apply; "did a client get a lease?" is
convergence and does not. The line is whether the answer depends on some other
device on the network deciding to speak.

**The `dhcp-script` callback bypasses the lock.** It writes *observed* state —
not config, not revisioned (§4.5) — so it lands on a separate endpoint and
cannot deadlock against a running apply.

**`olrd` executes no subprocesses.** Per §8's stack, `google/nftables` and
`vishvananda/netlink` are direct netlink and `coreos/go-systemd/dbus` is D-Bus.
There is no `exec.Command("nft")` and no `exec.Command("systemctl")`. That makes
the sandbox nearly free:

```ini
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/etc/open-linux-router /var/lib/open-linux-router /run/olr
```

Worth stating as a rule, because the first `exec.Command` silently costs all of
it. **One exception is already visible and should be taken deliberately rather
than by accident:** `olr logs` (§3.2 rule 4) currently execs `journalctl` in the
*CLI* process, which is outside `olrd` and therefore fine. The moment the WebUI
wants logs, `olrd` needs journal access — and `go-systemd/sdjournal` is cgo,
which §8's `CGO_ENABLED=0` forbids. So the sanctioned form is `journalctl -o
json`, read-only, and it is the only exec `olrd` may ever hold.

**Privilege separation is deferred, not foreclosed.** The one split that could
ever be justified is not per-module but privsep —
network-facing HTTP/TLS unprivileged, netlink and file writes behind a socket.
Skipped for v1: Go is memory-safe, so the classic pre-auth memory-corruption
motivation is weak, and an authenticated admin can legitimately do everything
anyway.

The seam costs nothing to keep, because it already exists for another reason.
§10's test-hardware entry requires isolating netlink, nftables and systemd
behind an interface so the rest is unit-testable off-Linux. **That testability
seam is the privsep seam.** Keeping it clean for tests keeps privsep a cheap
option nobody has to plan for.

---

## 4. Modules

Bounded list. Not expected to grow much. The object they all key off is the
**group** (§4.4), not the kernel interface.

| | Module | Owns | v1 |
|---|---|---|---|
| **Foundation** | `link` | NICs, bridges, VLANs, bonds, addresses (netlink); **groups** (§4.4) | ✅ |
| | `dial` | WAN: DHCP client, PPPoE, static, LTE; IPv6 PD | ✅ |
| | `devices` | **device identity** (§4.4); joins presence from leases + ARP | ✅ |
| **Services** | `dhcp` | dnsmasq — DHCPv4, DHCPv6 **and RA** (§4.2) | ✅ |
| | `dns` | unbound — **DNS only** | ✅ |
| | `firewall` | nftables `olr_filter`, `olr_nat` — zones, rules, NAT, forwards | ✅ |
| | `qos` | tc — CAKE / fq_codel, per-device shaping | |
| | `routing` | static + policy routes; later bird (BGP/OSPF) | |
| | `vpn` | wireguard — remote access, site-to-site | |
| | `wifi` | hostapd — only if the box has radios | |
| **Operational** | `system` | hostname, time, admin users, updates, backup, logs | ✅ |

### 4.1 Dependencies form a DAG

`link` is the foundation; everything references it. A module reads another
module's state **through that module's public API** — it never keeps its own
copy. Single ownership of every fact, so drift is structurally impossible.

Concretely, what the dependents read from `link` is a **group** (§4.4): a
named network with a subnet and a router address. They do not read interface
names, and they do not restate a subnet `link` already owns.

```
link ──┬─→ dial ──┬─→ dns      (upstream resolvers)
       │          ├─→ qos      (WAN iface + rate)
       │          ├─→ firewall (NAT egress iface)
       │          └─→ routing  (default route, PD prefix)
       ├─→ dhcp ──┬─→ dns      (lease hostnames, one-way publish)
       │          ├─→ devices  (leases as presence; identity flows the other way)
       │          └─→ qos
       ├─→ firewall
       ├─→ routing
       └─→ wifi
```

**When a cycle appears, invert one side into a fact subscription.** The real
case: `dhcp` wants to register lease hostnames in `dns`, and `dns` wants lease
data. Direction is fixed — `dhcp` *publishes* leases, `dns` *subscribes*.
(This is precisely why dnsmasq fuses the two; see §4.2.)

### 4.2 One backend, one owner

Throughout this document **daemon** means `olrd`, ours, one of; **backend** means
a supervised service it drives — dnsmasq, unbound, `pppd`, hostapd, bird — of
which there are many and none are ours to write (§3.5). The distinction matters
most here, where the two would otherwise both be called "the daemon."

Exactly one module owns any given backend or config file. dnsmasq can serve both
DHCP and DNS — if `dhcp` and `dns` both wrote `dnsmasq.conf`, module isolation
would break at the worst possible place. So:

- **dnsmasq** → `dhcp` only
- **unbound** → `dns` only
- **nftables** → each module writes its *own table*, never a shared ruleset

**`dhcp` owns router advertisement, exclusively.** This is not obvious from the
module's name and is easy to violate by accident, so it is stated here rather
than left implied. dnsmasq's `enable-ra` means the DHCP module is what announces
the default route on every IPv6 group. No other module may run radvd, CoreRAD,
or `IPv6SendRA` — a second RA source on one group is the same class of failure
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
  "Won't Fix (Intended Behavior)."** Any group that relies on stateful DHCPv6
  silently loses every Android device on it. For our audience that is most of
  the handsets on the LAN.

So the smallest correct thing is also the most compatible thing. Stateful
DHCPv6 becomes an advanced mode later, labelled with the Android consequence.

### 4.4 The shared objects: group and device

Two objects are referenced by many modules and owned by one. They are the
reason the module list stays orthogonal without the product feeling like a pile
of daemons.

**A group is a named network:** name, VLAN, bridge/members, subnet, router
address. `link` owns it, because `link` already owns addresses and a second
owner of the subnet would be exactly the private copy §4.1 forbids.

Modules key off the **group**, never the kernel interface. `vlan30` is an
implementation detail; `guest` is the thing the operator named, and the thing
that survives being moved to a different bridge. This is what makes a "guest
network" an object with a lifecycle — creatable, renamable, inspectable,
deletable — rather than a write-only recipe whose output is scattered across
four modules. §6.3 composite operations compose *over* groups; they are not a
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

**Naming.** "Group" and "network" name the same object. `group` is what this
document, the schema, and the API call it; **network** is what the operator sees
(`olr net add iot`, "guest network"). That is progressive disclosure (§1), not
two things to keep in sync — there is one object with one owner, and the second
word is a label on the common path rather than a second model.

**"Tag" is reserved for *sets of devices*** — parental controls, firewall rules,
QoS classes — which cut across groups and are a different concept entirely. It
needs its own word: using one word for both forecloses the other, and a rule
that applies to "the kids' iPads wherever they connect" is not a rule about a
network.

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
- **is the backend alive** — only backend-backed modules (`dhcp`, `dns`,
  `dial`, `wifi`, dynamic `routing`); mostly a systemd query (§3.5)

### 5.5 Lockout guard

Not a transaction — a dead-man's switch. If admin reachability is lost within 90
seconds of a change and nobody confirms, the change is reverted. One **global**
reachability probe, not per-module health.

The failure this prevents is not an enterprise failure. You are on your laptop,
on the LAN, changing the LAN. Your session dies mid-sequence and now you cannot
run the command that would fix it. Every homelab router does this eventually.

**The guard must not live in `olrd`'s memory.** This is the §3.5 invariant
applied: state that has to survive `olrd`'s death cannot be held by `olrd`. An
in-process 90-second timer dies with the process — and a change catastrophic
enough to need reverting is exactly the kind that takes `olrd` down or reboots
the box. The mechanism would evaporate in precisely the case it exists for.

So the guard is on disk and the timer belongs to systemd:

```
apply    → write the revert snapshot to /run/olr/guard/<id>.json
         → systemd-run --on-active=90s olr guard expire <id>
confirm  → stop the transient unit, drop the snapshot
timeout  → olr guard expire probes reachability, reverts from the snapshot
```

Three properties fall out. It survives `olrd` crashing; it survives `olrd` never
having started; and `olr guard expire` is an ordinary CLI path, so the revert is
inspectable and testable without simulating a wedged daemon.

`/run` rather than `/etc` is deliberate: the snapshot must not outlive a reboot.
A box that came back up is reachable by definition, so an expired guard replayed
after boot would revert a change that turned out to be fine.

### 5.6 Automatic behaviour must be declared, never inferred

We observe foreign state broadly (§3.4) and we may act on it — but only where
the configuration *says so*. The distinction is not pedantry: it is what keeps
§5.4 working. Drift is "plan the stored intent against reality and see if the
diff is empty," so behaviour that changes without appearing in intent makes
drift undecidable — `olr status` can no longer tell "the operator chose this"
from "we drifted into it."

The pattern, using the DHCP server as the worked example:

```
auto   serve unless another server is already answering on this group
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
   `auto` working. "Not running because the backend died" is a fault. If those
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

olr status                        aggregate: drift + backend liveness
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

**Two listeners, authenticated differently, because they are different things.**
The unix socket is the local admin path and its access control is the socket's
mode and group — filesystem permissions *are* its authentication, and a token
would add nothing a local caller has not already proven. TCP has no such
property and is always authenticated. Wrapping both in one scheme would break
`olr` over the socket for no gain.

Until `auth` exists (§10 open decision 1) the TCP scheme is a **single bearer
token** in `/etc/open-linux-router/api-token`, generated on first start. This is
a floor, not a design: it is real authentication and one file, and it
deliberately does not imitate a session, a role, or an identity that `auth`
would then have to contradict. An unauthenticated listener is available only for
development and refuses to bind anything but loopback — an open admin API on a
router's LAN address is not a convenience.

**Observed resources are not schema-published yet, and that is a gap.** Config
structs are reflected; the read and plan shapes are not, so a typed client has
to hand-write them and finds out at runtime when one drifts. Publishing response
schemas beside the config schema is what closes it, and it is worth doing before
a second module doubles the hand-written surface.

### 6.3 WebUI

SPA embedded in the binary, a pure client of the API.

**Composite tasks must live in core, not the UI.** "Set up a guest network"
touches `link` (VLAN), `dhcp` (pool), `dns` (scope), `firewall` (isolation),
`wifi` (SSID). If that orchestration lives in the WebUI, the CLI, the API, and
agents cannot do it — which rebuilds the exact UniFi limitation we're trying to
beat. Composite operations are first-class core operations with their own
routes; recipes state their own step order explicitly.

**Instant, except when it would disconnect you.** §5.1 says toggling a switch
applies instantly, with no "Apply changes" bar; §5.3.3 says the UI should warn
*"this will drop all LAN connections"* instead of showing a spinner. Those only
look contradictory. The rule is: every change is planned first, applied
immediately, and **held for confirmation only when the plan comes back
`disruptive`** — at which point the diff and the affected clients are shown.
The extra round trip buys the one outcome an operator cannot cheaply recover
from, which is being disconnected from the router by their own click. Nothing
else interrupts them.

This is also why `disruptive` has to be a *fact* rather than a guess (§5.3.3):
a classification that cried wolf would train the operator to click through the
one dialog that matters.

Live data (throughput, station lists, DNS query log) streams over SSE.

**The browser cannot authenticate an `EventSource`** — it sends no
`Authorization` header — so consuming the stream from the SPA needs either a
cookie session or a fetch-based reader. Deferred rather than chosen: while the
only event is "something was applied", refetching after a mutation covers it,
and the decision is better made against real live data than in advance of it.

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
| systemd | `coreos/go-systemd/dbus` | §5.4 backend liveness |
| Logging | `log/slog` | stdlib, structured (§3.3) |
| `.deb` | `nfpm` | single binary, cross-arch, no Ruby |

Six direct dependencies for v1. For a router that is a feature.

The SPA is a separate budget, spent where the product is judged (§1, "UX first"):

| | Choice | Note |
|---|---|---|
| Build | Vite, TypeScript | output is static files; no Node on the router |
| UI | React + `react-router` | declarative routing, no SSR — the daemon serves files |
| Styling | Tailwind v4 + shadcn/ui | components are vendored into the repo, not a dependency to track |
| Server state | TanStack Query | the entire UI is server state; hand-rolling the cache is the one place real code would be written for nothing |
| Types | generated from the published schema | §3.2 rule 3 — a field added in Go appears in TypeScript |

The asymmetry is deliberate. Node is a build-time dependency only: `go build`
works on a clone with no npm installed, and a binary built that way serves an
explanatory page instead of a UI. Nothing about the router's runtime depends on
this column.

**The version is a file, not a tag.** `VERSION` at the repo root holds a bare
number — `0.1.0`, no leading `v` — and the Makefile derives everything else from
it: what `olr version` prints, what dpkg and apk sort on, what the tarballs are
called, what a release publishes. A tag records only *which commit claims that
number*, so cutting a release is a reviewable commit followed by a matching
`v0.1.0` tag, and CI refuses to publish when the two disagree (`make
version-check`).

The alternative — deriving the version from `git describe` — was what this
replaced. It makes the number appear for the first time in a command typed at a
shell, where a typo becomes a published release with no diff to review, and it
gives a clone that fetched no tags a different answer than CI. The one thing it
did buy is kept: a build that is not the tagged commit reports `0.1.0-dev`
(and `-dirty` for an edited tree), so a working-tree binary can never claim to
be the released artefact.

---

## 9. Milestones

1. **Get a box online.** `link` + `dial` + minimal core (config store, routes,
   `olr` skeleton). Success: WAN up via DHCP and PPPoE, `olr status` truthful.
   **`link` must land groups (§4.4) here**, not later — every module after this
   keys off them, so retrofitting the primary key is the one sequencing mistake
   that would be expensive.
2. **Make it a router.** `firewall` (zones/NAT) + `dhcp` + `dns`. Success: a
   client on LAN reaches the internet with no hand-edited config.
3. **Make it safe.** Lockout guard, per-module revisions, impact classification.
4. **Make it visible.** WebUI shell, `devices` inventory, live throughput.
   The inventory did move earlier: identity landed with the device list, which
   is what unblocked the §11.1 fixed-address surface.
5. **Make it programmable.** OpenAPI publication, MCP server, skills.
6. **Then:** `qos`, `vpn`, `routing`, `wifi`.

**Order taken so far, and why it departs from the list.** `olrd`, the core
control plane and the WebUI shell landed before milestone 1, against a single
module (`dhcp`) driven over HTTP. The reason is that the load-bearing bet of
this whole architecture is §3.2 rule 3 — *one tagged struct drives CLI, REST, UI
and MCP* — and nothing tests that bet until one struct has actually reached a
browser. It paid immediately: reflecting `netip.Addr` and a duration wrapper
published the wrong types on every surface, a defect invisible from Go and
uncorrectable later without breaking clients.

What that jump does **not** buy is milestone 1. Pools are still keyed by kernel
interface name, so the DHCP screen is keyed on something §4.4 says is an
implementation detail, and it is scaffolding until `link` lands groups. Nothing
downstream should be built on that key in the meantime.

---

## 10. Open decisions

1. **`access` module split.** Assumed here as three concerns —
   `firewall` (zones/rules/NAT/forwards), `devices` (inventory, blocking,
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
   `/etc/open-linux-router/revisions/<module>/`, a git repo in
   `/etc/open-linux-router`, or **SQLite** in `/var/lib`? Git gives
   `history`/`rollback` and real diffs nearly free, at the cost of a git
   dependency in the control plane and odd behaviour if a user pokes at it.
   SQLite is the newest candidate and the strongest on mechanics — atomic,
   indexed, no second process, and it answers `history` without walking a
   directory. Its cost is size, not correctness: `CGO_ENABLED=0` is
   non-negotiable (§8), so it would have to be `modernc.org/sqlite`, which is
   transpiled C and roughly ten megabytes of binary against a document that
   counts six direct dependencies as a feature.

   Whichever wins, **intent stays a JSON file** (§3.2 rule 1). The store holds
   past copies, never the live one: "SSH in and read the config" is the recovery
   path on a router, and a database is the wrong thing to meet at that moment.
   The same store is the natural home for the throughput history §6.3 needs,
   which is the one genuine query workload in the product and may end up
   deciding this.
6. **Does creating a network default DHCP on?** Leaning yes — a LAN without
   DHCP is the unusual case, and §5.1 immediate-apply makes it cheap to undo.
7. **What is a group, exactly?** (§4.4) Its field set is now the most
   load-bearing schema in the project: `link`, `dhcp`, `dns`, `firewall` and
   later `wifi` all key off it. Worth designing deliberately rather than
   growing.

### Resolved

- **`devices` owns the inventory, and it is a module of its own.** (was open
  decision 6, §4.4) `clients` is promoted to v1, renamed to `devices` after the
  object it owns, and grows the intent half: MAC, name, category, model, notes.
  The alternatives both failed on §4.1. Identity in `dhcp` would force
  `firewall` to depend on `dhcp` to write a rule about a laptop, which §4.4
  forbids in as many words. A separate identity module *beside* a `clients`
  reader would split one object across two owners and leave the join homeless.

  Presence stays read-through and arrives via a consumer-declared interface, the
  way `dhcp` reads `link` through `LinkView`: `devices` never imports `dhcp`, and
  the lease adapter is wired in `cmd/olrd`. What was going to be the `clients`
  module — conntrack, wifi stations — becomes further presence sources behind
  that same interface rather than a second module with a second name for one
  object.

  **The fixed address does not move.** `dhcp` keeps owning it; the device list
  joins against it per request. §11.1 asked that the *workflow* start from the
  device, not that the fact change hands — and moving it would have been the
  private copy §4.1 exists to prevent. This unblocks that surface.

- **The device list reads ARP, from `/proc/net/arp`.** (was open decision 7)
  The screen is called Devices, so it has to be one: a lease-derived list omits
  the statically-addressed printer, and operators notice. Reading the kernel's
  neighbour table costs no new dependency, which is why it beat the netlink
  library — `go.mod` has three direct dependencies and that restraint is worth
  more than IPv6 coverage in v1.

  The cost is stated rather than hidden. `/proc/net/arp` is IPv4-only, so ND
  waits for `link` to bring netlink in properly (milestone 1), and a v6-only
  client is invisible to both sources until then. A device that neither takes a
  lease nor answers ARP is absent until added by hand, and the screen says so
  next to its `as_of` rather than implying the list is complete. On a machine
  with no `/proc/net/arp` at all — every non-Linux developer box — that is
  reported as a problem on the response, never swallowed.

- **HTTP stays stdlib; no web framework.** Reaffirmed against a concrete
  proposal to adopt `gin`, and the deciding argument was not taste. A framework's
  binding tags (`binding:"required"`) are a *second* source of requiredness on
  the same structs that this document already derives `required` from — the
  absence of `omitempty` (see *Config format: JSON* below). Two sources for one
  fact is what §4.1 forbids, and here they would disagree silently, in the
  document every other surface is generated from. What remains of the framework's
  value is routing that 1.22 `ServeMux` already does, middleware that is thirty
  lines, and SSE that is `http.Flusher` either way — against a dozen transitive
  dependencies inside the process holding `CAP_NET_ADMIN`.
- **No database in the control plane, for now.** Also reaffirmed against a
  concrete proposal. Config is a file per module (§3.2 rule 1) and stays one;
  SQLite's real candidacy is revision history and throughput series, which is
  open decision 5 above rather than settled here. The distinction worth keeping
  is *which data* — intent, which must survive being read by a human with a
  broken box, versus derived history, which never has to.
- **The API's auth floor is a bearer token** (§6.2), scoped to the TCP listener
  only, with the unix socket relying on its file mode. Chosen because the
  alternative was not "wait for `auth`" but "ship an unauthenticated admin API",
  and because a token is the one credential shape that does not prejudge what
  `auth` will be.
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
- **Process model: one resident `olrd`; modules in, backends out** (§3.5).
  Settled by the invariant that restarting `olrd` must never disturb traffic.
  Two alternatives were considered and rejected:
  - **Module-per-process.** Rejected on §5.3.1 (cross-module validation becomes
    a fallible round trip with a TOCTOU window), on the per-lease `dhcp-script`
    path becoming two hops, and on startup order degenerating into a six-unit
    systemd graph — reintroducing the module iteration §3.1 had eliminated.
  - **Socket activation with idle exit.** Rejected: it optimises idle RSS, the
    cheapest resource on the box, and pays for it where it hurts. A monitoring
    loop calling `olr status` becomes a cold spawn per poll, each re-reflecting
    the schema, so the access pattern a router actually sees is the one it
    handles worst. It also makes `inactive` the healthy state in
    `systemctl status`, adds a second unit, and adds an idle timeout with no
    principled value. Keeping `olrd` resident also preserves §6.1's two-tier
    rationale unchanged, since `olr daemon start` remains a real operation.
- **`olrd` is a control plane, not a worker.** It may cache only what is derived
  and cheap to rebuild — the reflected schema, the route table, the OpenAPI
  document. Never config, never observed state, never revision history. The test:
  `kill -9 olrd` followed by a restart must not change a single answer the API
  gives. Residency is what makes this rule necessary — a long-lived process is
  somewhere to put state, and §5.4's drift detection silently stops being true
  the moment intent is compared against memory instead of reality (§4.5).
- **One global apply lock, and apply is bounded** (§3.6). Not per-module: a
  single lock is what makes §5.3.1's validate-then-apply free of a TOCTOU
  window. Viable only because apply never waits for convergence — it renders,
  reloads and returns, leaving "is the WAN actually up" to observed state.
- **Vocabulary: `daemon` is `olrd`, `backend` is what it drives** (§4.2). The
  document previously used "daemon" for both, which made `olr daemon status`
  read ambiguously against "is dnsmasq alive" — two different questions with
  two different answers.
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

- **The group is the unit of network** (§4.4), owned by `link`, and modules
  key off it rather than off kernel interface names. Considered and rejected: a
  separate `network` module, which would own a name and a subnet that `link`
  must know anyway — two sources for one fact, which §4.1 forbids. Consequence
  for `dhcp`: the pool's primary key is a group, its range is derived from the
  group's prefix by default, and most of the cross-module validation §5.3.1
  celebrates stops being *needed* for the common case, because the subnet is
  given rather than asserted and re-checked.

- **The object is called a `group`, not a `segment`.** Renamed on comprehension:
  "segment" is a network engineer's word for a broadcast domain, and the
  audience for this product is not reliably a network engineer. The object was
  always presented to the operator as a *network* (§4.4 naming), so the rename
  costs nothing on the common path and makes the design vocabulary match the
  product's.

  One consequence, and it is the reason this entry exists rather than being a
  silent find-and-replace: **"group" was previously reserved for sets of
  devices**, and that concept is real — parental controls and QoS classes cut
  across networks. It is now called a **tag** (§4.4). Recorded so the next
  reader does not rediscover the collision and assume one of the two concepts
  was dropped.

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

- **Network settings** attach to a **group**. `link` owns the group; `dhcp`
  owns what it serves there.
- **A fixed address** is a property **of a device**, not a standalone row. It is
  set from the device list, not by typing a MAC into a form. The difference
  between those two sentences is the difference between the OPNsense experience
  and the UniFi one.

Modelling fixed addresses as a flat MAC→IP table is the failure mode to avoid;
it is what forces every workflow to start from a hardware address the operator
has to go and find. `devices` now owns identity (§10, resolved), so this surface
is unblocked: the device list is where a fixed address is set, with the MAC
already known, and `dhcp` still owns the reservation itself.

### 11.2 Per-group settings

| Field | Default | Note |
|---|---|---|
| `dhcp` | `on` | `auto \| on \| off` per §5.6 |
| `range` | derived | from the group prefix; explicit overrides |
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
its configuration file. Reservations and per-group options therefore live in
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
| **v1** | per-group `auto\|on\|off`, derived range, fixed addresses, lease/DNS/domain, IPv6 SLAAC+RDNSS | |
| | pool usage, foreign-DHCP-server detection, recent DHCP events | "why did this device not get an address" is the single most common support question; these three answer it |
| **v2** | PXE / netboot (`dhcp-boot`) | high demand in *this* audience — Proxmox, netboot.xyz |
| | deny DHCP to a device (`dhcp-ignore`) | belongs with inventory blocking, not here |
| | option 121 static routes, stateful DHCPv6 | escape hatch until then |
| **Never** | DHCP relay | different object shape; settled once, not revisited |
| | per-vendor-class options, multiple pools per group | escape hatch, permanently |

### 11.5 Known gaps

- **Post-apply verification — closed.** A dead DHCP server is invisible for
  hours and then breaks everything at once when leases expire, and §5.5's
  lockout guard does not cover it: the operator keeps their own session
  throughout. Apply now watches the unit for a bounded window after the service
  action and fails if it does not stay active, which §3.6 permits explicitly.

  The window is the point, not the check. The unit is `Type=simple`, so systemd
  reports the start job `done` the moment the process is forked — before
  dnsmasq has read a config file or bound a socket. A single sample straight
  after the job therefore passes for a backend that is about to exit. Asserting
  on UDP/67 instead was considered and rejected: a group serving only RA never
  binds it, so that check would fail exactly where IPv6 is configured
  correctly. Staying alive is the stronger signal anyway, because dnsmasq exits
  on a config it rejects rather than idling.
- **`dhcp-script` is not wired up.** It is the only real IPC dnsmasq offers, and
  it is what turns leases from a polled file into an event stream — the live
  device list (§6.3 SSE), the §4.1 lease publish to `dns`, and the event history
  above all depend on it. Highest-leverage single item in the module.
- **Drift is byte-comparison against rendered files,** so changing a comment in
  the renderer marks every deployed install as drifted and schedules a restart on
  next apply. Inherent to the approach; needs a deliberate answer before there
  are installs in the field.
