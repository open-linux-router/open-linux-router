# `dns` module design

Status: **§6's v1 row is built.** `internal/dns` is the module, `internal/dnsrelay`
and `cmd/olr-dnsd` are the relay, and the packaging carries `olr-dns.service`
(unbound, on loopback:5353) and `olr-dnsd.service` (ours, on :53). Section
references are to `design.md`.

Three things this document argued about have since been settled, and each is
recorded where it arises: §7.1 turned out to have been decided already by
`design.md` §3.5; §6's "blocking is native and cheap" does not survive the relay
(§4.4); and **olr no longer routes by domain name at all**, which closes §7.3 and
takes fake-IP out of the design entirely — `docs/gateway.md` §4 has that
argument, and §2 and §4.3 here are trimmed to match.

This document exists because DNS turned out not to be a service olr configures.
It is the layer that decides which traffic can be policed at all, and that
makes it a routing concern wearing a resolver's clothes.

---

## 1. The topology this has to survive

A real deployment is not one router. It is several boxes on one L2, each
willing to act as a gateway, and devices free to pick any of them:

| | Role | Has the WAN uplink |
|---|---|---|
| 1 | Modem / ISP router — dials, NATs, serves DNS, usually serves DHCP too | yes |
| 2 | The olr box — DHCP, DNS, routing | no |
| 3 | A proxy box — mihomo/clash or similar, DNS + routing | no |
| 4 | Devices | |

Boxes 2 and 3 are side routers: they hang off box 1 and forward to it. A device
sets a default gateway and a DNS server, **independently**, and each may point
at any of the three. That is not a configuration option we offer. It is a
property of the wire, and it has to be treated as a fault mode.

### 1.1 The rule

> **DHCP hands out olr, and only olr, for both the gateway and the resolver.
> Boxes 1 and 3 are exits, never client-facing.**

Everything below is a consequence. The proxy box is a **next hop** exit
(`via 192.168.1.x`) in the §12 sense, and the modem is another one. Neither is
something a device is ever told about.

### 1.2 Why — two failures that have no fix at the device layer

**A device pointed elsewhere is invisible, not merely unaccounted.** Traffic
that never crosses olr's forward hook is absent from the `ipv4_addr . mark`
counters, and absent from the residual too, because nothing knows it should be
there. This is a different class of problem from unattributed bytes, and it
defeats the "always show what you cannot account for" rule by construction —
there is no counter to show. Detection is limited to the heuristic *has a lease
and answers ARP but has produced no traffic*.

**IPv4 has no default-gateway failover.** One gateway, no liveness check, no
second choice. If the proxy box is a device's gateway and it dies, that device
is offline until a human intervenes. The workarounds are all bad, and it is
worth writing down why so they are not re-proposed:

| Approach | Why not |
|---|---|
| Short lease, rewrite option 3 | Half the lease time to converge; many clients ignore a changed gateway on renew |
| VRRP between olr and the proxy box | Needs keepalived *on the proxy box* — a box we neither own nor version |
| olr gratuitous-ARPs the proxy's address | Races the box if it half-recovers |
| IPv6 RA router lifetime 0 | Genuinely works, for v6 only, which is not the problem |

One layer up it is not a problem at all: olr health-probes the exit and
re-points it. No device knows anything happened, so there is nothing to
converge. The probe must be the **through-path probe**, not a ping — a crashed
mihomo on a live Debian box answers ARP and ICMP indefinitely while forwarding
nothing.

### 1.3 The limits of the rule, stated

On a shared L2 an operator with admin on their own laptop can always
static-configure their way onto box 1 or 3. Port isolation on a managed switch
is the only real answer, and we do not require one. We can make it *detectable*
and we can make it *inconvenient* — block client→proxy-box traffic except from
olr — and then we document it rather than implying coverage we do not have.

---

## 2. The two legs

Pointing a device at olr means two independent things, and they fail
differently.

| | Gateway leg | Resolver leg |
|---|---|---|
| Buys | Packets reach the box at all: per-source `Internet via`, IP/GEOIP rules, byte accounting | Blocklists, per-tag blocking, the query log, and exact names for the proxy's own domain rules |
| Missing | Nothing works; the box never sees the traffic | Domain policy silently does not apply |
| Fails | Loudly | **Silently** |

The reverse mismatch is the milder one: resolver-here, gateway-elsewhere costs
the policy but not the connectivity — the client still receives a routable
address and still reaches the internet, and only the name-dependent half goes
unapplied. So the dangerous misconfiguration is gateway-yes / resolver-no, which
is exactly what §1.1 forecloses.

One constraint that is easy to miss, and which — since gateway:§4 — applies to
the *operator's* proxy rather than to us: whatever resolver a proxy derives its
domain rules from has to be the one its clients actually used, or it is matching
on names nobody asked it for. olr's part is to make the recommended arrangement
expressible — point `upstream` at the proxy's resolver, so one answer serves
both — not to model the mapping itself.

### 2.1 Gateway leg

nftables marks forwarded traffic by source; RPDB routes the mark to a table
whose default is the proxy box. Standard §12 next-hop exit. Two things the
proxy box does that have to be absorbed:

- When its rules say DIRECT it forwards back out the same interface and sends
  olr an **ICMP redirect**. `accept_redirects=0` on olr, or the table quietly
  acquires routes we did not choose. This is the mirror of the `send_redirects=0`
  obligation already recorded for LAN-side next hops.
- Its reply goes straight back to the device over the shared L2, so olr sees one
  direction only — byte counts halve and `ct mark` restore misses. Fix by SNAT
  at olr toward the next hop, or by giving the proxy box a segment of its own.
  SNAT is cheaper and costs the proxy its own per-source rules; a dedicated
  segment is correct and costs a VLAN. **Open.**

### 2.2 Resolver leg, and why a proxy's DNS matters

A proxy matches domain rules by one of three means. Only one of them survives a
client that did not use our resolver:

| Source of the domain | Survives? |
|---|---|
| Fake-IP reverse map (`198.18.0.0/15` → name) | No — the client never received a fake IP |
| `redir-host` ip→domain cache | No — same dependency |
| Sniffing TLS SNI / HTTP Host / QUIC ClientHello | **Yes**, for TLS and HTTP |

So sniffing rescues rule matching *inside* the proxy, and nothing rescues a
routing decision that was supposed to be triggered by an address the client
never got. It also degrades: ECH encrypts the SNI, and non-TLS protocols have
nothing to sniff. Treat it as a fallback, not a design.

This is what makes **DoT/DoH blocking load-bearing** rather than hygiene, and it
ranks by cost to defeat: plaintext :53 to a hardcoded server (cheap — DNAT
hijack), DoT on :853 (cheap — drop by port, do not reject), DoH on :443
(expensive — IP/SNI blocklist, kept current, TCP *and* UDP or it moves to
HTTP/3). Browsers and Apple devices auto-upgrade to DoH by default, so this is
the common case rather than the adversarial one.

---

## 3. Why we do not use unbound's forwarding

`forward-zone` is instance-global. Views carry `local-zone`, so per-client
*blocking* is native and per-tag parental controls are nearly free; views do not
carry forwarding, so **per-client upstream selection is not expressible in
unbound**. (Established from documentation, not verified against the build in
trixie. It is load-bearing — confirm before relying on it.)

> **Built:** the view half of this stopped mattering. Once the relay owns `:53`,
> every query reaches unbound from `127.0.0.1` and all views collapse into one,
> so blocking moved into the relay (§4.4) and unbound is configured with no
> views at all. §7.4's verification is therefore no longer blocking — it becomes
> load-bearing again only if v2's per-client upstream selection is attempted
> inside unbound rather than by pointing clients at a second resolver.

The forwarding half still binds, and it is the constraint behind `upstream`
being one setting for the whole box: **one resolver, one upstream.**

That has a sharp edge, and it is the one hazard gateway:§4.1's recommendation
carries. Pointing `upstream` at the proxy's own resolver is the arrangement that
gives the proxy exact names to match its domain rules on — but the upstream is
global, so *every* device gets whatever that resolver answers. If the proxy is
running **fake-IP**, a device on `Internet via: Modem` receives `198.18.x`, hands
it to the modem, and is blackholed. It will read as "the internet is broken for
the tablet and fine for the laptop", which is a miserable thing to debug.

Two ways out, and the first is the recommendation:

- **Ask the proxy for real addresses.** mihomo's `redir-host` mode answers with
  the genuine address and keeps its own ip→domain cache, so its rules still
  match and every device — proxied or not — gets something routable. This costs
  the operator nothing olr was relying on, because since gateway:§4 olr never
  wants a fake IP for anything.
- **Or forward only when everything exits via the proxy.** Then there is no
  device left to blackhole. Fine for the single-exit network, and it stops being
  fine the moment a second exit appears, which is not a footgun worth leaving
  armed.

The escape from the global constraint itself is one layer down, not one layer
out. Source-matched DNAT on port 53 selects the resolver per client:

```
nft prerouting: ip saddr @proxy_clients udp dport 53 dnat to <resolver-B>
```

and this machinery is required anyway — option 6 is *advice*, DNAT is
*enforcement*, and the DoH/DoT defence already needs every forwarded `:53`
hijacked regardless of what a client has configured. Pointing different source
sets at different resolvers is an extension of a rule we must write, not a new
mechanism. That is v2's per-client upstream selection, and it is what lets a
fake-IP proxy coexist with mixed exits properly rather than by convention.

Either way it is a tradeable to be argued to the operator in their terms —
*"a second resolver so the devices on Modem don't get Netflix's address from the
proxy"* — never as a limitation of a daemon they did not choose.

---

## 4. The decision: a splitter, not a resolver

Writing a resolver is the wrong project. The hard parts — recursion and referral
chasing, DNSSEC validation and trust-anchor rollover, NSEC3 nonexistence proofs,
cache-poisoning defence, twenty years of accumulated edge cases — are all parts
we do not want and would get subtly wrong for years.

What we want is three things, and all three sit at the *front* of the pipeline:

1. See every query, with the client's address.
2. Decide, per client, whether to answer it ourselves.
3. Decide, per client, **which upstream answers it**.

So olr listens on :53, applies policy, and relays to unbound — which keeps doing
recursion, DNSSEC and caching. This does not remove unbound. The box ends up
running dnsmasq for leases, unbound for resolution, and olr for policy and
observation. That is a layer, not a simplification, and pricing it as one is the
mistake to avoid.

`internal/dhcp` already renders `port=0` into dnsmasq's config, with a test
enforcing it — dnsmasq is deliberately confined to DHCP and :53 left free. The
shape below is what claims it.

### 4.1 The invariant

> **Parse a copy for observation. Forward the original bytes untouched.**

Never re-serialise a response. Re-serialising breaks DNSSEC signatures, drops
EDNS options we did not model, and mangles record types we have never heard of.
Relaying the original means a parse failure costs a statistics entry and nothing
else — resolution still works. That makes the parser **best-effort by
construction**: it never has to be right, only useful. This property, not the
absence of parsing, is what keeps this out of resolver territory.

The one place it bends: returning NXDOMAIN or an override requires synthesising
a response. Small, and the only code path that must be exactly right.

### 4.2 Passthrough on the fast path, observation on a tee

```go
// fast path: serve the client first, observe second
rn, _ := upstream.Read(rbuf)
conn.WriteToUDP(rbuf[:rn], src)          // client is done here

select {
case tee <- obs{src: src.IP, msg: clone(rbuf[:rn])}:
default:
    dropped.Add(1)                        // statistics lag; DNS never does
}
```

The tee buys **isolation**, not speed. Three obligations come with it:

1. **Copy the bytes, never send the slice.** Handing `buf[:n]` to a channel
   while the read loop reuses `buf` misattributes answers to the wrong device,
   intermittently, under load only.
2. **`default:` on the send, always.** A channel that blocks when full turns a
   slow parser into DNS latency — the exact failure the tee exists to prevent.
   Drop, and count the drops: the gap must be visible, same rule as the
   `unpoliced` counter.
3. **`recover()` in the parser.** Malformed responses are routine and
   compression-pointer handling is where they bite. On the fast path a panic
   ends DNS for the house; on the tee it increments a counter.

A convenience worth exploiting: the response echoes the question section, so
`{srcIP, responseBytes}` is fully self-describing. No correlation table, no
pending-query map, no timeout sweeper. The gap is queries that never get an
answer, which produce no observation — keep a counter and accept that v1's query
log is a log of *answered* queries.

Once the relay is `read → policy → forward → write → try-send` it is **done**.
Every later feature lands on the far side of the channel and cannot regress DNS.
That is the line to hold in review: anything that influences the response belongs
in the policy lookup before the forward; anything that merely observes goes over
the channel.

### 4.3 The domain→IP map

The one thing that does require reading responses, and the reason 4.1 is phrased
as it is.

- **Compression pointers** must be resolved, with a loop guard, or a malformed
  response hangs the parser. `golang.org/x/net/dns/dnsmessage` handles this
  correctly and is most of the argument for using it over hand-rolling.
- **Attribute to the QNAME, not the record owner.** `www.example.com` commonly
  answers as a CNAME to `example.cdn.net` with the A record owned by the latter.
  Mapping owner→IP naively bills the device for a CDN hostname it never asked
  for. Keep the chain as well — its tail is the organisation signal §12's
  statistics model groups by.
- **It is many-to-many.** One name resolves to several addresses; one CDN
  address serves dozens of names. This is precisely the case where a per-domain
  byte split must not be fabricated.
- **Expire at TTL plus grace.** Conntrack flows routinely outlive the TTL that
  created them, and an address reassigned to another tenant misattributes
  silently.
- **Key by `(device, IP)`.** The source address is free and global keying
  cross-attributes when two devices reach one CDN address by different names.

Skip authority and additional sections, RRSIG/NSEC (relay, never validate), and
everything non-address. Log the qtype; map only A and AAAA.

The many-to-many applies to everything, with no exception for proxy-routed
traffic. An earlier draft had one — fake IPs are 1:1 with names, so anything
routed by name attributed exactly — but gateway:§4 removed the mechanism that
produced them, and one honest story about accuracy beats two. IP→ASN stays the
fallback for what this can never cover: clients that resolved before olr
started, connected by address, or asked somewhere we cannot see.

### 4.4 Blocking lives in the relay, not in unbound's views

A correction to §3 and to §6's "blocking is native and cheap", found while
building and forced by the rest of this document rather than chosen.

unbound selects a view by the **client's netblock**. The moment the relay owns
`:53`, every query reaches unbound from `127.0.0.1`, so every view matches the
same netblock and the whole mechanism collapses into one. The relay is the only
thing left that still knows who asked.

So the relay answers blocked names itself, which §4.1 already anticipated as the
one place the invariant bends — "returning NXDOMAIN or an override requires
synthesising a response. Small, and the only code path that must be exactly
right." It is about a hundred lines, and it buys two things beyond making the
feature work at all: unbound stays a plain recursive resolver with no views, so
the escape hatch stays a plain passthrough; and the answer is exact about the
client rather than about a netblock the resolver had to be told about twice.

The synthesised response echoes the query's ID and question section, because a
client matches on both and silently discards anything else — a blocked name that
produced a discarded answer would present as a timeout, which is the failure
blocking exists to avoid.

### 4.5 Where the observations are read from

The query log and the name map live in the relay's memory and are served over a
read-only unix socket at `/run/olr/dns/observe.sock`, which `olrd` reads through
on every request.

This is the *read* direction only. §3.5's "not a private RPC channel" governs
how we **drive** a backend, and that stays rendered files plus SIGHUP: the relay
starts from `relay.json` and `policy.d/` with nothing else alive on the box. A
read-only socket costs neither of the two things that corollary names — the
relay is still independently runnable, and `curl --unix-socket` on it is a
better debugging story than parsing a lease file, not a worse one.

A socket rather than a state file because the alternative is a line per query
appended to disk, and a great many olr boxes boot from an SD card. It also
leaves §7.5 genuinely open instead of quietly answering it.

---

## 5. The risk, which is availability

Perf is not a concern: a relay plus a memcpy against a house doing tens of
queries per second. Parser risk is contained by §4.2. The real cost is
different.

Today a DNS bug would produce a bad config file and a daemon that refuses to
start — loud, contained, and the thing answering queries is twenty years
hardened. Owning :53 means any olr crash, bad apply or OOM takes DNS down for
the whole building at once, and DNS-down does not read as DNS-down; it reads to
every occupant as *the internet is broken*. That is wider than §5.5's lockout
guard was scoped for, which strands only the operator.

Two things make it manageable:

**It is stateless.** No cache to warm, so a restart costs milliseconds and
clients retry unprompted. A crash loop is survivable in a way it would not be
for a real resolver.

**Which makes the process boundary the decision that matters.** In-process with
`olrd`, every config apply and upgrade blips DNS for the house, and an unrelated
panic in an HTTP handler takes the LAN offline. A separate process with
`Restart=always`, reading policy from `olrd` over the existing socket, is the
shape to argue for — at the cost of the single-daemon simplicity that is
currently a property of the design. **Open, and it determines whether the risk
above stays manageable.**

> **Built, and it was never open:** `design.md` §3.5 had already decided this.
> "Backends are separate processes even when we write them… if `olr-dhcpd` ever
> exists it is a binary and a unit, never a goroutine", and its test — *does it
> have to keep running while `olrd` is stopped?* — DNS fails plainly. So the
> relay is `cmd/olr-dnsd`, `Type=notify`, `Restart=always`.
>
> One correction to the paragraph above: it is *not* configured by reading from
> `olrd` over a socket. §3.5's corollary is that we drive our own backend the way
> we drive dnsmasq — rendered files and a signal — because a private channel
> costs the backend its ability to be run and debugged on its own. `relay.json`
> is a restart; `policy.d/` is a SIGHUP.
>
> The observation direction is a socket; see §4.5 for why that is not the same
> thing.

Also new: a network-facing UDP listener is a posture olr does not currently have
at all — `olrd` is a unix socket and a config renderer. Bind to LAN interfaces
only and access-control by source, or we have shipped an amplifier.

---

## 6. Scope

| | | |
|---|---|---|
| **v1** | resolver leg: DNAT hijack of forwarded `:53`, upstream = unbound, per-group policy | built; policy keys off client prefixes until `link` lands groups |
| | passthrough relay with tee, query log, domain→IP map | the observability case is the whole reason to own :53 |
| | per-client blocking, DoT `:853` drop | built **in the relay**, not in unbound views — §4.4; the block is what protects everything else |
| **v2** | per-client upstream selection (proxy vs direct) | needs §2.1's return-path answer first |
| | DoH `:443` blocklist, TCP and UDP | ongoing maintenance, not a one-off |
| **Never** | recursion, DNSSEC validation, our own cache | the moment this process needs a cache, it is a design change and not a refactor |
| | authoritative service, zone transfers | escape hatch, permanently |
| | minting or routing fake IPs | gateway:§4 — routing by name is the proxy's job |

---

## 7. Open

1. ~~**Separate process or inside `olrd`** (§5).~~ **Closed** — `design.md` §3.5
   had already decided it. `cmd/olr-dnsd`, its own unit, `Restart=always`,
   driven by rendered files and a signal.
2. **SNAT toward next-hop exits, or a dedicated segment for the proxy box**
   (§2.1). Determines whether byte accounting sees both directions.
3. ~~**Fake-IP destination routing versus per-source assignment — which wins.**~~
   **Closed by deletion**, which is the only way a question like this closes
   cleanly: it asked which of two things wins, and `gateway.md` §4 removed one of
   them. olr does not route by name, so there is no global domain rule to
   compete with `Internet via`, and the §5.6 surprise it worried about cannot
   arise. Routing by name stays the proxy's job.
4. **Whether unbound views really cannot carry `forward-zone`** (§3). Verify
   against trixie before the ladder in §3 is relied upon. **No longer blocking:**
   blocking moved into the relay (§4.4) and unbound is rendered with no views at
   all, so this only matters again if v2's per-client upstream selection is
   attempted inside unbound rather than with a second resolver process.
5. **Where the query log lives.** It is a second workload voting in §10's
   revision-storage decision, and the larger of the two. v1 keeps it in the
   relay's memory and serves it over a socket (§4.5), which bounds the memory
   and pre-decides nothing — the log starts empty after a restart, and the API
   says so rather than implying a history it lacks.

---

## 8. Known gaps in what is built

Stated here rather than left to be discovered, per the rule that we document
what we do not cover instead of implying coverage we do not have.

- **A redirect that failed to load is not detected.** `olr-dnsd.service` loads
  `hijack.nft` in `ExecStartPost` with a leading `-`, so a failure is logged and
  does not take DNS down with it — the right trade, since a broken redirect
  costs the redirect and a failed unit costs the building its name resolution.
  But nothing reads the ruleset back, so `olr dns status` cannot say the
  redirect is missing. Reading nftables belongs to the `firewall` module; this
  closes when that lands.
- **IPv6 is captured only if there is an IPv6 listen address.** The renderer
  emits per-family redirect rules and warns when one family has none, which is
  honest but not the same as covering it. A client resolving over IPv6 on a
  dual-stack network bypasses the redirect entirely — the same failure the
  routing model records for a v4-only exit.
- **Policies key off client prefixes, not groups.** `link` has not landed, so
  this is the same stand-in `internal/dhcp` makes by keying pools off kernel
  interface names, and it changes shape at the same time.
- **The query log is of *answered* queries.** A query that never gets an answer
  produces no observation, by §4.2's design — there is no pending-query map and
  no timeout sweeper. The `failed` counter is where those show up.
- **Not verified end to end.** The renderers, the planner, the policy matcher
  and the relay's own request path are unit-tested, including byte-for-byte
  passthrough against a fake upstream. Real `:53` capture, `nft -f` loading and
  unbound interop need Linux with `CAP_NET_ADMIN` and have not been exercised.
