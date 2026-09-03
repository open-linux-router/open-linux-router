# `dns` module design

Status: **design only**. Nothing here is built. `internal/dns` does not exist,
and no daemon in the packaging renders DNS configuration. Section references
are to `design.md`.

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
| Buys | Packets reach the box at all: per-source `Internet via`, IP/GEOIP rules, byte accounting | Domain→exit routing, blocklists, per-tag blocking, the query log |
| Missing | Nothing works; the box never sees the traffic | Domain policy silently does not apply |
| Fails | Loudly | **Silently** |

The reverse mismatch is the safe one: resolver-here, gateway-elsewhere fails
loudly, because the client receives a fake IP and hands it to a router with no
route for it. So the only dangerous misconfiguration is gateway-yes /
resolver-no, which is exactly what §1.1 forecloses.

One constraint that is easy to miss: the resolver and the proxy must be in the
same **policy domain**. The resolver that mints a fake IP and the proxy that
receives the packet have to share the mapping table. Split them across boxes
and the fake IP means nothing on arrival.

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

That matters because a global `forward-zone: "."` to the proxy's resolver gives
full fidelity — every name gets a fake IP, and the proxy has an exact domain
rather than a sniff — but only if **every** device exits via the proxy. With
mixed exits, a modem-exit device also receives `198.18.x`, hands it to the
modem, and is blackholed.

The escape is one layer down, not one layer out. Source-matched DNAT on port 53
selects the resolver per client:

```
nft prerouting: ip saddr @proxy_clients udp dport 53 dnat to <resolver-B>
```

and this machinery is required anyway — option 6 is *advice*, DNAT is
*enforcement*, and the DoH/DoT defence already needs every forwarded `:53`
hijacked regardless of what a client has configured. Pointing different source
sets at different resolvers is an extension of a rule we must write, not a new
mechanism.

Which reframes the constraint: domain routing is global if we want one resolver,
and per-exit if we are willing to run one resolver process per distinct DNS
policy. That is a tradeable, and it should be argued to the operator in those
terms — *"a second resolver so Modem devices don't get Netflix through the
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

**A useful inversion:** fake IPs are 1:1 with names, so attribution for
proxy-routed traffic is exact, and it is directly-resolved traffic that gets the
messy many-to-many. The path hardest to route is the easiest to account for.
IP→ASN stays the fallback for what this can never cover — clients that resolved
before olr started, connected by address, or asked somewhere we cannot see.

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

Also new: a network-facing UDP listener is a posture olr does not currently have
at all — `olrd` is a unix socket and a config renderer. Bind to LAN interfaces
only and access-control by source, or we have shipped an amplifier.

---

## 6. Scope

| | | |
|---|---|---|
| **v1** | resolver leg: DNAT hijack of forwarded `:53`, upstream = unbound, per-group policy | |
| | passthrough relay with tee, query log, domain→IP map | the observability case is the whole reason to own :53 |
| | per-client blocking, DoT `:853` drop | blocking is native and cheap; the block is what protects everything else |
| **v2** | per-client upstream selection (proxy vs direct) | needs §2.1's return-path answer first |
| | DoH `:443` blocklist, TCP and UDP | ongoing maintenance, not a one-off |
| | fake-IP route as a global domain rule | belongs with §12, not here |
| **Never** | recursion, DNSSEC validation, our own cache | the moment this process needs a cache, it is a design change and not a refactor |
| | authoritative service, zone transfers | escape hatch, permanently |

---

## 7. Open

1. **Separate process or inside `olrd`** (§5). The one that decides whether
   owning :53 is acceptable.
2. **SNAT toward next-hop exits, or a dedicated segment for the proxy box**
   (§2.1). Determines whether byte accounting sees both directions.
3. **Fake-IP destination routing versus per-source assignment — which wins.**
   §12 lists the domain rule as global and `Internet via` as per-source; if
   global means global, the fake-IP route sits at higher RPDB priority and a
   device showing `Internet via Modem` still reaches some names through the
   proxy. That is exactly the surprise §5.6 exists to prevent, so whichever way
   it goes, the effective value and its source have to be visible.
4. **Whether unbound views really cannot carry `forward-zone`** (§3). Verify
   against trixie before the ladder in §3 is relied upon.
5. **Where the query log lives.** It is a second workload voting in §10's
   revision-storage decision, and the larger of the two.
