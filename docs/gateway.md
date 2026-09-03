# `routing` module design — exits and traffic assignment

Status: **design only**. Nothing here is built. `internal/routing` does not
exist. Section references are to `design.md` unless prefixed `dns:`, which means
`docs/dns.md`.

This is the document `docs/dns.md` calls §12. The two were designed together and
have to be read together: this one decides where a packet goes, that one decides
which packets we can see well enough to decide about. Neither works alone.

---

## 1. The object: an exit

Every attempt to name this failed while we were reaching for a *destination*
word. In routing the pair is always *(destination, via)* — `netflix.com` is a
destination, `10.8.0.0/24` is a destination, and the thing we kept failing to
name is the **via**. "Gateway" and "proxy" both feel half-right because they
describe what the box on the far side does, which is not what this object is
about.

### 1.1 Definition

> **An exit is anything that accepts traffic addressed somewhere else and takes
> responsibility for delivering it.**

That is a membership test, not a label, and it settles the recurring questions
without further argument:

| | Exit? | Why |
|---|---|---|
| A WireGuard or proxy TUN interface | yes | accepts packets for anywhere |
| A box on the LAN running mihomo | yes | same, one hop away |
| A TPROXY port on this box | yes | accepts connections addressed elsewhere |
| `unreachable` | yes | takes responsibility by refusing, explicitly |
| **A SOCKS5 or HTTP proxy port** | **no** | the client has to *ask*, in its own protocol |
| A DNS server | no | answers questions, carries nothing |
| A domain | no | that is a destination |
| Policy-based IPsec | no | no interface, nothing to point at |

The SOCKS5 row is the one this test earns its keep on. Transparency — *does it
accept traffic addressed elsewhere* — is the real property, and "you can point a
default route at it" was only ever a proxy for it. `tun2socks` exists precisely
to manufacture an exit out of a thing that is not one.

### 1.2 The four forms

```
via: { interface: "utun0" }                    # wg0, tailscale0, ppp0, a proxy TUN
via: { next_hop: "192.168.1.50", dev: "lan" }  # a box on the LAN, the modem
via: { local_socket: "127.0.0.1:7893" }        # TPROXY
via: blocked                                    # unreachable
```

**The box's own default gateway is an exit — the default one.** In the reference
topology (dns:§1) olr is a side router with no WAN uplink, so its default exit is
the modem as a next hop. Nothing about that is a special case, which is why
multi-WAN falls out of this model rather than being built into it: a second ISP
is a second next-hop exit and nothing else.

Two forms share a mechanism and one does not. `interface` and `next_hop` are both
**routing** — the packet leaves through a route table, and they differ by one
field. `local_socket` is **not routing**: the packet is never routed, it is
delivered to a socket on this box. That split matters in §3 and nowhere else; the
operator never meets it.

**There is deliberately no `tun` form, and no `vpn` form.** A TUN device is an
instance of `interface`, alongside WireGuard, Tailscale and PPPoE. Giving TUN its
own kind forces the same question for every other interface-shaped thing, and
within two releases the schema has a kind per vendor. Modules that create
interfaces (`vpn`, `dial`) *produce candidates*; this module names some of them as
exits.

### 1.3 Naming

`exit` is the schema word. The operator never sees it. The surface is one
sentence with the preposition doing the work:

```
Internet via  [ Clash ▾ ]        上网经由  [ Clash ▾ ]
```

Same construction as `group` / "network" (§4.4) — one object, two registers,
progressive disclosure rather than two models.

Rejected: **`gateway`**, which already means "next hop" specifically while this
object has three forms that are not one, and which is an engineer's word of the
same class as the retired "segment". **`outbound`** (sing-box's term) was
considered and rejected on a concrete collision: one of our exits *contains* a
list of sing-box outbounds, so the two words would name different things one
level apart in a deployment running both. sing-box itself later had to split
`endpoint` out of `outbound` for WireGuard and Tailscale — at exactly the
interface-shaped boundary this object spans.

---

## 2. The operator model: a property, not a rule list

An ordered first-match list is what the kernel gets. It is the wrong thing to
show, because answering *"where does my phone actually go?"* then requires
simulating evaluation, and rules shadow each other invisibly.

> **Everything has one way out. Change it for a network, a group of devices, or
> one device — the most specific setting wins.**

### 2.1 The ladder

```
box default  →  network (group)  →  tag  →  device
                                            most specific wins
```

The two cases that motivated this:

- *IoT devices go direct* — the IoT network keeps the default. Nothing to
  configure; it is already right.
- *Phones go through Clash* — the `Phones` tag gets `Internet via: Clash`. One
  setting.

And the case an ordered list handles badly falls out for free: *everything
through Clash except the NAS* is `default: Clash` plus one device set back. No
negation, no rule at position 1 that everyone forgets.

Nothing is draggable and there is no precedence to learn. We derive the kernel
ordering — device marks, then tag, then network, then default — deterministically
from the ladder.

### 2.2 Effective value, with its source

Inheritance is unusable if the answer is not visible:

```
Living Room TV
  Internet via   Clash          from tag "Phones"        [override]
  Status         ● via Clash · 14 GB this month
```

This is §5.6's *effective state is first-class* applied to inheritance instead of
to `auto`. It is also where a failed exit surfaces — `no internet — Clash is
down` is a diagnosable state in the place the operator already looks.

### 2.3 Conflicts are refused, not resolved

A device in two tags with different exits is genuinely ambiguous. Inventing a
tie-break (creation order, alphabetical) produces behaviour nobody can predict
from the screen, so the plan step refuses:

> *Phone-3 is in both `Phones` (Clash) and `Kids` (Modem) — pick one.*

§5.6's *refuse, do not disable*, one level down.

### 2.4 Four objects, four questions

| Object | Answers | Scope |
|---|---|---|
| **Exit** | how traffic leaves | — |
| **`Internet via`** assignment | who uses which exit | per network / tag / device |
| **Static route** | how to reach one specific place | everyone |
| **Domain rule** | which names go somewhere else | per source (§4) |

Only the second is per-source, which is why only it needs the ladder and the
other three stay flat lists. Keeping them apart is what stops any one of them
growing a precedence model.

Two of the six most common requests — *"reach the office subnet"* and *"IoT gets
no internet"* — turn out not to involve a proxy at all. The first is a static
route; the second is the `blocked` exit.

### 2.5 Staging

The full ladder depends on devices and tags, and who owns the device inventory is
§10 open decision 6 — the one design.md already calls the most urgent. So:

- **First: network-level assignment.** Needs only groups, which milestone 1
  delivers. Covers the motivating case if phones sit on their own network or
  SSID, which is a reasonable recommendation regardless.
- **Then: tag and device overrides**, refining the same field. The mental model
  does not change, because *most specific wins* was true from the first version.

---

## 3. Mechanism

### 3.1 nftables classifies; the RPDB routes

These get conflated, and the distinction decides what is possible:

| | Does | Cannot |
|---|---|---|
| **nftables** | select packets, set a mark, deliver to a local socket | choose a next hop |
| **`ip rule` + route tables** | choose a route table by mark, hold the exit's default route | select on anything richer than the RPDB's match set |

There is no nft expression that picks a next hop. `dup` copies, `fwd` is L2-only
in the netdev family, and DNAT would rewrite the destination and break the
connection. So a routing exit costs one nft rule, one `ip rule`, and one route —
about four netlink calls, created once and never touched again.

**Consequence recorded because it was proposed:** REDIRECT (`redir-port`) is the
only mechanism needing nftables alone, and it is rejected in §10. The one `ip
rule` it saves is one we are already paying for the next-hop form.

### 3.2 The resources we own, declared

Three shared namespaces, and §3.4's good-citizen rule says we take a documented
slice of each rather than assuming we are alone. Docker, libvirt, k8s, WireGuard
and mihomo all use marks; mihomo and sing-box both install `ip rule` entries.

| Resource | Ours | Rule |
|---|---|---|
| fwmark | one documented byte, `0x00ff0000` | always set *and match* with the mask; never touch other bits |
| RPDB priority | a documented contiguous range | so a user can deliberately sit in front of or behind us |
| route table ids | a documented range | never `main`, never a bare number chosen at runtime |
| nftables | `olr_route`, our own table | `nft flush ruleset` is banned (§3.4) |

The numbers are constants in the docs, not incidental values, because the whole
point is that someone can plan around them.

### 3.3 The ruleset

```
table inet olr_route {
  chain classify {                    # type filter hook prerouting, priority mangle
    ct mark and 0x00ff0000 != 0  meta mark set ct mark  return    # restore, §3.4
    ip  saddr @dev_clash  counter name "r1" meta mark set … or 0x00120000
    ip6 saddr @dev_clash  counter name "r1" meta mark set … or 0x00120000
    ip  daddr 198.18.0.0/15 counter name "r2" meta mark set … or 0x00120000
    counter name "unpoliced"
    ct mark set meta mark                                          # save
  }
}

100:   from all lookup main suppress_prefixlength 0     # LAN and connected stay local
110:   fwmark 0x00120000/0x00ff0000 lookup 8112
32766: from all lookup main                            # unpoliced → default exit
```

The rule at priority 100 is load-bearing and the most common way a hand-rolled
setup breaks: without it, an exit table's default route swallows traffic to your
own LAN.

**Named counters, not anonymous inline ones.** Re-rendering a chain zeroes
anonymous counters, so editing rule 3 would reset rule 1's numbers. Named objects
survive rule replacement. The `unpoliced` counter is what makes per-exit totals
reconcile against the box total — see §7's residual rule.

### 3.4 Flow stickiness

`ct mark` save and restore buys three things for two lines: an in-flight session
survives a policy edit, per-flow classification replaces per-packet set lookups,
and editing a rule stays `reload` rather than `disruptive`.

It depends on olr seeing both directions of the flow, which dns:§2.1 records as
**open** for LAN-side next-hop exits — see §5.2.

### 3.5 Forward-only classification, and what it gives away free

We classify **forwarded traffic only**, never locally-originated. The router's own
traffic follows `main`, always.

That is a scope decision, and it happens to eliminate the failure everyone hits
when running a proxy on the router: the proxy's own upstream connections are
locally-generated, so they never meet our classifier, and cannot be routed back
into the proxy. No mark exclusion, no `routing-mark` coordination, no loop.

```
LAN client → prerouting (we mark) → utun0 → proxy reads
                                              ↓
                             proxy opens its own connection
                                              ↓
                          output path → main table → the default exit ✓
```

The declared limitation: traffic olr itself originates cannot be assigned to an
exit. If that is ever wanted, it needs a `type route hook output` chain, which
re-runs the routing decision after the mark changes — and it needs the exclusion
above written by hand.

### 3.6 The TPROXY form

For a proxy on this box, the packet is never routed:

```
ip  saddr @dev_clash meta l4proto { tcp, udp } tproxy to 127.0.0.1:7893 \
    meta mark set 0x00120000 accept

ip rule  add fwmark 0x00120000/0x00ff0000 lookup 8112
ip route add local default dev lo table 8112
```

The mark plus that `local default` route is what tells the kernel to deliver
locally rather than forward, while the packet keeps its original destination.

**TPROXY carries TCP and UDP only.** ICMP, ESP and GRE from an assigned source
match nothing, and the exit must declare which it wants:

- `drop` — **the default**. A failed ping is diagnosable.
- `direct` — leaks the real address to anything the client pings, and lets a
  client's own IPsec ignore the exit entirely. Available, never silent.

### 3.7 `blocked` is `unreachable`, not `blackhole`

Blackhole drops silently and every connection hangs for thirty seconds.
`unreachable` sends ICMP admin-prohibited and applications fail immediately.
Same feature, completely different experience for whoever is holding the tablet.

---

## 4. Domain rules, and the answer to dns:§7.3

A domain cannot be matched at the routing layer — routing sees addresses. The
mechanism is the proxy's own fake-IP range, which is a *destination prefix*, and
prefixes are something the RPDB routes natively:

```
client asks for netflix.com
  → olr's :53 relay forwards this (client, name) to the proxy's resolver   [dns:§4]
  → proxy answers 198.18.x.x from its fake-IP pool
  → client connects to 198.18.x.x
  → nft matches the fake-IP prefix → mark → the proxy exit
  → proxy maps it back to netflix.com
```

No nftables set population, no dnstap, no DNS interception beyond the `:53` hijack
dns:§3 requires anyway.

### 4.1 Per-source wins, and the conflict is prevented rather than resolved

dns:§7.3 asks which wins when a device set to `Internet via: Modem` resolves a
name carrying a global domain rule. It is a real trap: if that device receives a
fake IP and the fake-IP prefix routes to the proxy for everyone, the device
silently reaches some names through an exit its own setting says it does not use.

The resolution is at the resolver, not the router:

> **A device receives a fake IP if and only if it is routed to that proxy.**

Because olr owns `:53` and applies policy per client, the upstream is chosen per
`(device, domain)`. A device on `Internet via: Modem` is never handed
`198.18.x.x` at all, so the ambiguity cannot arise. The fake-IP route is not a
global rule that competes with per-source assignment — it is the mechanical tail
of a per-source decision already made.

This is why **DNS policy is derived from the exit assignment and is not
independently configurable**. Letting an operator point a device's resolver at
one exit and its traffic at another is a way to build a network where names
resolve to addresses nothing can route.

Two consequences:

- **Each proxy exit needs a distinct fake-IP range**, or an address cannot be
  attributed back to an exit. Irrelevant with one proxy; a required field with
  two.
- **A domain rule for a device whose exit is direct works** — olr forwards just
  that name to the proxy's resolver, the device gets a fake IP for that name
  only, and everything else resolves and routes normally.

### 4.2 What it does not cover

A client that resolves elsewhere never receives a fake IP, so nothing triggers.
dns:§2.2 ranks the defences by cost to defeat; the short version is that DoH/DoT
blocking is load-bearing for domain routing, blocklists and the query log at
once, and browsers auto-upgrade to DoH by default, so this is the common case
rather than the adversarial one.

---

## 5. Failure modes

Each of these presents as *"I set an exit and the internet stopped"*, so they are
plan-step checks and declared behaviour, not troubleshooting notes.

### 5.1 The ones that are checks

| | | |
|---|---|---|
| **NAT on the egress** | traffic out a new exit needs masquerade, owned by `firewall` | plan *validates* and reports; a composite op (§6.3) can fix it later |
| **Next hop not directly reachable** | must fall inside a prefix we hold | catches entering the proxy's public address instead of its LAN one |
| **`rp_filter`** | drops asymmetric return traffic | this module owns the sysctl on its interfaces, declared |
| **MSS clamping** | tunnel and PPPoE egress | `firewall`'s table, but this feature is what makes it necessary |
| **A rule matching the admin's own address** | §5.5's lockout scenario exactly | classify `disruptive`, hold, show the diff (§6.3) |

### 5.2 ICMP redirects, in both directions

When the exit's next hop shares a segment with the clients — the normal case —
two things happen that do not happen with a WAN gateway:

- We forward a packet back out the interface it arrived on, and the kernel
  helpfully tells the client to talk to the proxy box directly. Some clients
  obey. → `send_redirects=0` on any interface carrying a same-segment next hop.
- When the proxy box's own rules say DIRECT it forwards back out the same
  interface and sends **us** a redirect, and our table quietly acquires routes we
  did not choose (dns:§2.1). → `accept_redirects=0` on olr.

Both are sysctls this module owns on the interfaces it uses, declared per §3.4's
*don't squat shared state*.

### 5.3 The return path, and what it costs the statistics

**Open, inherited from dns:§2.1.** A same-segment next hop replies straight to
the device over the shared L2, so olr sees one direction only. Byte counts halve
and `ct mark` restore misses.

| Fix | Costs |
|---|---|
| **SNAT at olr toward the next hop** | the proxy box loses per-source visibility — acceptable, since we took source selection over anyway |
| A dedicated segment for the proxy box | correct, costs a VLAN, keeps real client addresses |

Recommendation is SNAT for v1, on the grounds that the thing it gives up is
already ours. It should be a per-exit field rather than a global choice, because
an operator who wants the proxy box to keep doing its own per-source work needs
the other answer.

### 5.4 IPv6 leaking around a v4-only exit

If the exit carries v4 only and clients have working IPv6, everything with an
AAAA record goes out the default path at full speed, unnoticed. Every exit needs
an explicit answer — a v6 form, or block v6 for assigned sources. Not optional;
it is the difference between working and appearing to work.

### 5.5 When the exit dies

The health check is a **through-path probe, not a ping**. A crashed mihomo on a
live Debian box answers ARP and ICMP indefinitely while forwarding nothing —
worse, it loops our traffic back at us, because its own default gateway is us.

Failure behaviour is declared per §5.6, with hysteresis:

- `block` — **the default.** The UI says *"Living Room TV: no internet — Clash is
  down"*, which is diagnosable.
- `direct` — silently leaks exactly the traffic the operator asked to route.
  Available, never the default.

dns:§1.2 raises the stakes on this: the argument that DHCP hands out olr and only
olr rests on olr being able to re-point a dead exit, because IPv4 has no
gateway failover at the device layer. So exit health is not a later refinement —
it is what the topology rule is trading against.

---

## 6. Coexistence with a proxy that wants to route

mihomo's `auto-route` and sing-box's equivalent install their own `ip rule`
entries, route tables and nftables rules. On the same host as olr that is two
owners of one decision surface, with the worst available failure mode: it works
until a version bump moves a priority number, and then some traffic silently
takes the wrong path.

The requirement is `auto-route: false`. It is a setting in a file we do not own
(§10), so:

> **Detect and refuse.** If adding an exit finds foreign `ip rule` entries
> pointing at a table carrying a default route, the plan does not proceed:
>
> *mihomo is managing routing itself (4 foreign ip rules, priority 9000–9003,
> table 2022). olr cannot share the routing table with it. Set
> `auto-route: false` and retry.*

§5.6's *refuse, do not disable* — we do not rewrite their file and we do not
silently work around it. The check must be **structural** (any foreign rule at a
priority that shadows ours; any foreign table with a default route), never a
hardcoded priority, because those numbers move between versions.

The same detection earns its keep permanently: `olr status` reporting *"4 foreign
ip rules present"* is the routing-layer counterpart of *"3 foreign nftables
tables present"*, and it is what makes someone else's hand-rolled setup legible
instead of mysterious.

**This is also why TPROXY is preferred over TUN for a same-host proxy.** The
operator configures one setting either way, but the failure modes are not
comparable: a missing `tproxy-port` is connection-refused on the first packet,
while a wrong `auto-route` misroutes silently.

---

## 7. Statistics

### 7.1 Per device and per exit, in two rules

```
table inet olr_stat {
  set dev_up   { type ipv4_addr . mark ; flags dynamic ; counter ; }
  set dev_down { type ipv4_addr . mark ; flags dynamic ; counter ; }
  chain account {
    type filter hook forward priority 0 ; policy accept ;
    update @dev_up   { ip saddr . meta mark }
    update @dev_down { ip daddr . meta mark }
  }
}
```

The concatenated key gives per-device **and** per-exit from one structure,
because the mark is already there from §3.3. *"Living Room TV: 40 GB, of which 38
via Clash"* costs nothing extra. With no routing installed the mark is 0 and it
degrades to plain per-device totals, so this table does not depend on that one.

Live throughput is the delta between samples. Per-flow detail — connection counts
and who talked to what — comes from **conntrack destroy events**, which hand over
a complete byte count once per flow rather than requiring a table walk. Both are
needed: a four-hour stream reports nothing until it ends.

### 7.2 Domains: attribution, not measurement

There is no per-domain byte counter anywhere in the kernel. Joining dns:§4.3's
`(device, IP) → name` map against conntrack gives an *attribution*, and it fails
in known ways: one CDN address serves many names, clients cache past the answer,
flows outlive the mapping, hardcoded addresses never produce a name at all.

**Do not fabricate a split.** Group bytes by the thing that is measured — the
address, resolved to an organisation — and list domains as *what this device
asked for*:

```
Cloudflare                                   42 MB
  looked up here: example.com · foo.io · bar.net
Google Video                                8.1 GB
  googlevideo.com
Unattributed (direct IP, no lookup seen)     310 MB
```

The reframe that makes this acceptable: **the ambiguity is worst exactly where
the bytes are smallest.** Video — the traffic that actually fills a link — runs
on dedicated hostnames and address ranges and attributes cleanly. Shared CDN
addresses carry the long tail of pages and API calls.

dns:§4.3's inversion applies here too: fake IPs are 1:1 with names, so
proxy-routed traffic attributes exactly, and it is directly-resolved traffic that
is messy.

### 7.3 The residual is always a visible row

`unpoliced` in §3.3, *unattributed* above, and foreign `ip rule` entries in §6 are
the same rule three times:

> **Show what you cannot account for.** A number nobody can trust is worse than
> a number with a stated boundary.

dns:§1.2 names the one case this rule cannot reach: a device pointed at another
gateway produces no counter at all, so there is nothing to show. That is a
different class of gap, and it is why the topology rule exists.

### 7.4 Limits worth printing in the UI

- **Traffic that does not cross the router is invisible.** Two devices on one
  segment — a NAS backup, a Plex stream — never reach the forward hook. *"Why
  does my NAS show 200 MB when I copied 40 GB?"* will be asked.
- IPv6 privacy addresses rotate, so per-IP counters fragment across one device.
- A device that changes address splits its history. Fixed leases mostly fix it.
- Anything behind a second router counts as one device.

### 7.5 Storage, and a stance

Counters are monotonic-since-load; everything anyone wants is deltas over time.
This is the second workload voting in §10 open decision 5, alongside dns:§7.5's
query log — and they should be decided together rather than separately.

A per-device domain history is a record of everyone in the building's browsing,
on a box in the hallway. Default retention window, a visible off switch, and
per-device exclusion — decided now rather than after someone asks. It is also
worth stating plainly as a property: it never leaves the box, which the
ISP-supplied router cannot say.

---

## 8. What each form carries

| | interface (incl. TUN) | next hop | TPROXY |
|---|---|---|---|
| Mechanism | routing | routing | local delivery |
| Proxy runs | on this box | on another box | on this box |
| TCP | ✓ | ✓ | ✓ |
| UDP, QUIC | ✓ | ✓ | ✓ |
| ICMP | emulated by the proxy | forwarded to the far box | **not covered** (§3.6) |
| ESP, GRE | dropped by the proxy | forwarded to the far box | **not covered** |
| Failure presents as | interface gone, route withdrawn | probe fails | connection refused |
| Operator must set | `auto-route: false` ⚠ | nothing on our side | `tproxy-port` |

---

## 9. Scope

| | | |
|---|---|---|
| **v1** | exits: `next_hop`, `interface`, `blocked` | `interface` is nearly free once `next_hop` exists, and it is how WireGuard and Tailscale arrive |
| | `Internet via` at **network** level | tag and device tiers wait on §10 #6 |
| | nft classify + RPDB, documented mark/priority/table ranges | |
| | per-exit health probe, `block` on failure | dns:§1.2 depends on it |
| | named counters, the `ipv4_addr . mark` set | |
| | foreign `ip rule` detection and refusal | |
| **v2** | `local_socket` (TPROXY) | wants dns:§2.1's return-path answer settled first |
| | tag and device tiers of the ladder | |
| | domain rules (fake-IP), per source | needs dns v2 |
| | conntrack-derived per-flow detail | |
| **Later** | multi-WAN failover policy beyond `block` / `direct` | hysteresis and probe design are their own scope |
| | an advanced source+destination rule list | the only thing the ladder cannot express |
| **Never** | our own proxy engine, or wrapping one | §10 |

---

## 10. Considered and rejected

- **Wrapping mihomo or sing-box as a managed backend.** Structurally legal — a
  separate unit, rendered config, supervised like dnsmasq. It fails on packaging
  and cadence: neither is in Debian, so we would be distributing a proxy binary
  and its CVEs, and sing-box makes breaking config changes across minor versions,
  which is fine for a human with a migration guide and expensive for a renderer
  that must survive `apt upgrade`. The operator runs it; we route to it. If it is
  ever revisited, mihomo is the candidate — its config format is the stable one,
  and it parses subscriptions natively.
- **sing-box as the base layer, with no nftables at all.** It is more featureful
  at routing, and that is not the question. Everything through a userspace TUN
  costs: inbound port forwards stop working (a TUN is outbound-only), a client's
  own IPsec breaks, ICMP becomes emulated so the tools people debug with lie, and
  every byte crosses a userspace stack with no offloads. mDNS was *not* a valid
  objection and is recorded here as withdrawn — it is link-local with TTL 1 and
  never crosses a router.
- **REDIRECT (`redir-port`).** The only nftables-only mechanism, and TCP-only.
  QUIC is UDP/443 and carries much of Google, YouTube and Cloudflare, so a
  browser tries HTTP/3, is not intercepted, and goes straight out the default
  exit — while everything else is proxied. Silent, partial, undiagnosable. The
  hole is patchable by blocking UDP/443, but games, voice and a client's own
  WireGuard are not. TPROXY costs one more `ip rule` and covers them.
- **Modelling a SOCKS5 or HTTP endpoint as an exit.** Fails §1.1. Supporting it
  badly is worse than not supporting it: someone routes a TV through it and loses
  UDP, ICMP and IPv6 with no signal. The empty state should guide instead —
  *a SOCKS5 proxy cannot carry all traffic on its own* — with the path to making
  it into one.
- **Populating nftables sets from the resolver** (dnstap, or dnsmasq's `nftset`).
  Made unnecessary by §4: the fake-IP range is already a prefix, and prefixes
  route natively.
- **Fake-IP for the entire LAN**, to make byte attribution exact by construction.
  It is the only complete answer to shared-CDN-address ambiguity, and it requires
  synthesising every answer, breaks IP literals and out-of-band resolvers, and
  turns every flow into a NAT entry keyed on a name. Recorded because it is the
  idea someone will have.
- **TLS SNI inspection** to split shared addresses. The only real answer, and it
  is expiring: ECH encrypts the SNI, and the shared CDN addresses causing the
  problem are furthest along in deploying it. Also eBPF or nfqueue in the data
  path of a box whose job is not breaking. Recorded with the reason, so it is not
  re-proposed without knowing about ECH.
- **An ordered rule list as the default surface** (§2). It is what the kernel
  gets; it is not what the operator should have to simulate.

---

## 11. Open

1. **The operator word in Chinese.** `exit` and *Internet via* are settled;
   "上网经由" versus "出口" is not. Cosmetic, but it should be decided once.
2. **SNAT toward next-hop exits, or a dedicated segment** (§5.3, dns:§7.2).
   Determines whether accounting sees both directions and whether `ct mark`
   restore works at all. Leaning SNAT, as a per-exit field.
3. **Who owns the device inventory** (§10 #6). Gates the tag and device tiers of
   the ladder, and per-device statistics ownership with it.
4. **Time-series storage** (§10 #5). Now with two workloads voting — these
   counters and dns:§7.5's query log.
5. **Whether `google/nftables` can read per-element stateful counters** over
   netlink, or only set membership. §7.1 depends on it, and the pure-Go netlink
   bet is load-bearing (§10 resolved, *Language: Go*). Cheap to find out with a
   spike; expensive to discover late. Fallback is `nft -j list set`, which
   forfeits the reason Go was chosen for this layer.
6. **Whether the ladder needs a fifth tier for "this device, to that
   destination".** Deferred in §9 as an advanced list; if it turns out to be
   common, it belongs in the ladder rather than beside it.
