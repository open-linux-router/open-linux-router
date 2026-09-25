# `gateway` module design — the boundary, in both directions

Status: **built.** `internal/gateway` is the module. It was called `routing`
until v0.1.1 — the box as a whole is the router, and this is the part of it
that forwards traffic, alongside `dhcp` and `dns`. It absorbed the deleted
`firewall` module later; §0 is the current scope and this document's spine is
still the outbound half it was written about. The document keeps the file name
it was written under. Section references are to `design.md` unless prefixed
`dns:`, which means `docs/dns.md`.

This is the document `docs/dns.md` calls §12. The two were designed together and
have to be read together: this one decides where a packet goes, that one decides
which packets we can see well enough to decide about. Neither works alone.

---

## 0. What this module owns

**The boundary between the networks this box serves and everything else**, in
both directions. Three concerns, three nftables tables, three lifetimes:

| Concern | Table | Governed by | Where |
|---|---|---|---|
| Exits, and which network uses which | `olr_route` | `Enabled` | this document, §1–§6 |
| Egress NAT | `olr_nat` | `Enabled`, and only when `dial` has an uplink | §3.9 |
| Port forwards, and hairpin | `olr_nat` | `Enabled` | `docs/port-forwarding.md` |
| Byte accounting | `olr_stat` | **not** `Enabled` (§7.1) | §7 |

Port forwarding arrived here from a module called `firewall`, deleted because it
did no filtering and olr is not building any for now; `docs/port-forwarding.md`
§0 has that argument and this document does not repeat it. Two consequences
worth stating here, because they are this module's rather than that feature's:

- **`Enabled` now governs both tables.** Turning this module off stops policy
  routing *and closes every forwarded port*. That is what "disable the module
  that does the NAT" should mean, and it is a sharper edge than the switch had
  before, so §8a and the UI say it in those words.
- **Only `Stats` escapes `Enabled`**, and §7.1 has always argued why. Nothing
  else gets its own lifecycle switch; a concern's condition is its own
  configuration existing.

What this module does **not** own: the box's own way out. That is `dial`'s
uplink (`docs/dial.md`) — §1.2 has the split, and it is the one an operator gets
wrong first.

---

## 1. The object: an exit

Every attempt to name this failed while we were reaching for a *destination*
word. In routing the pair is always *(destination, via)* — `example.com` is a
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
| A box on the LAN running a proxy | yes | same, one hop away |
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

Where that gateway comes from is **`dial`'s** and not this module's
(`docs/dial.md` §1). If olr owns the way out, the operator sets an uplink there —
an interface, an address and a next hop — and `dial` writes the default route
into the main table and puts it back after a reboot. If they do not, the main
table is whatever the distribution or a DHCP client put there, which is the
reference topology and is supported. Either way this module reads that route and
never writes it: `Config.Default` left empty means "whatever is in the main
table", and there is no reserved exit name standing for it, because one value
gets one owner.

The distinction that costs an afternoon if it is missed: an uplink connects
**this box**, an exit chooses a **different** way out for the networks behind
it. A network that simply wants to go the way this router already goes needs no
exit at all — what it needs is source NAT, and that is §3.9's rule rather than
an exit's, precisely so that the default path does not have to be spelled as an
exit repeating the uplink's own next hop.

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
Internet via  [ Proxy ▾ ]        上网经由  [ Proxy ▾ ]
```

Progressive disclosure (design.md §1) — one object, two registers, rather than
two models. The network used to be the other example of this, called `group`
below the UI; it is now *network* at every layer (design.md §4.4), so `exit` is
the one that stands alone.

Rejected: **`gateway`**, which already means "next hop" specifically while this
object has three forms that are not one, and which is an engineer's word of the
same class as the retired "segment". **`outbound`** — the word several proxy
tools use — was considered and rejected on a concrete collision: one of our
exits *contains* a list of such a tool's outbounds, so the two words would name
different things one level apart in a deployment running both. At least one such
tool later had to split `endpoint` out of `outbound` for WireGuard and Tailscale
peers — at exactly the interface-shaped boundary this object spans.

---

## 2. The operator model: a property, not a rule list

An ordered first-match list is what the kernel gets. It is the wrong thing to
show, because answering *"where does my phone actually go?"* then requires
simulating evaluation, and rules shadow each other invisibly.

> **Everything has one way out. Change it for a network, a group of devices, or
> one device — the most specific setting wins.**

### 2.1 The ladder

```
box default  →  network  →  group  →  device
                                      most specific wins
```

The two cases that motivated this:

- *IoT devices go direct* — the IoT network keeps the default. Nothing to
  configure; it is already right.
- *Phones go through Proxy* — the `Phones` group gets `Internet via: Proxy`. One
  setting.

And the case an ordered list handles badly falls out for free: *everything
through Proxy except the NAS* is `default: Proxy` plus one device set back. No
negation, no rule at position 1 that everyone forgets.

Nothing is draggable and there is no precedence to learn. We derive the kernel
ordering — device marks, then group, then network, then default — deterministically
from the ladder.

### 2.2 Effective value, with its source

Inheritance is unusable if the answer is not visible:

```
Living Room TV
  Internet via   Proxy          from group "Phones"      [override]
  Status         ● via Proxy · 14 GB this month
```

This is §5.6's *effective state is first-class* applied to inheritance instead of
to `auto`. It is also where a failed exit surfaces — `no internet — Proxy is
down` is a diagnosable state in the place the operator already looks.

### 2.3 Conflicts are refused, not resolved

A device in two tags with different exits is genuinely ambiguous. Inventing a
tie-break (creation order, alphabetical) produces behaviour nobody can predict
from the screen, so the plan step refuses:

> *Phone-3 is in both `Phones` (Proxy) and `Kids` (Modem) — pick one.*

§5.6's *refuse, do not disable*, one level down.

**Groups are exclusive, so this one cannot arise.** The tier was *tags* when
this section was written, and a device could carry two. It is now *groups*
(design.md §4.4), and every device is in exactly one, so there is one group
setting per device and nothing to refuse — the conflict is prevented where it
would be stored rather than caught at plan time. The rule stands for whatever
can still genuinely conflict.

### 2.4 Three objects, three questions

| Object | Answers | Scope |
|---|---|---|
| **Exit** | how traffic leaves | — |
| **`Internet via`** assignment | who uses which exit | per network / group / device |
| **Static route** | how to reach one specific place | everyone |

Only the second is per-source, which is why only it needs the ladder and the
other two stay flat lists. Keeping them apart is what stops any one of them
growing a precedence model.

There was a fourth — a domain rule, answering *which names go somewhere else*.
It is gone, and §4 explains why: routing by name is the proxy's job, and it was
the one object the ladder could not express.

Two of the six most common requests — *"reach the office subnet"* and *"IoT gets
no internet"* — turn out not to involve a proxy at all. The first is a static
route; the second is the `blocked` exit.

### 2.5 Staging

The full ladder depends on devices and groups, and who owns the device inventory is
§10 open decision 6 — the one design.md already calls the most urgent. So:

- **First: network-level assignment.** Needs only networks, which milestone 1
  delivers. Covers the motivating case if phones sit on their own network or
  SSID, which is a reasonable recommendation regardless.
- **Then: group and device overrides**, refining the same field. The mental model
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
and proxy daemons all use marks; proxy and VPN daemons install their own `ip
rule` entries.

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
    ip  saddr @dev_proxy  counter name "r1" meta mark set … or 0x00120000
    ip6 saddr @dev_proxy  counter name "r1" meta mark set … or 0x00120000
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
anonymous counters, so adding an exit would reset every other exit's numbers.
Named objects survive rule replacement. The `unpoliced` counter is what makes per-exit totals
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

### 3.5.1 Translated connections are not ours to classify

`fib daddr type != local` is not a sufficient guard on its own, and the case it
misses is not exotic — it is **every port forward on a box that also has an
exit**, which is to say the two features silently did not work together until
port forwarding landed and this was fixed.

Follow the reply leg of a forwarded connection. The inbound `SYN` is addressed to
the router, so `fib daddr type != local` is false and it is correctly left
unmarked. The server's `SYN/ACK` is a different matter: conntrack does not
restore the original destination until postrouting, so in prerouting it is a
packet from `192.168.1.10` to somewhere on the internet — indistinguishable from
an ordinary LAN machine dialling out, and duly marked for that network's exit.
The connection dies half-open.

So the classify rules carry a fourth guard:

```
ct status & 0x20 (IPS_DST_NAT) == 0
```

> **A connection whose destination we rewrote is not one we choose an exit for.**

The semantics are general rather than a courtesy to one module: the reply leg of
any DNATed connection belongs to whoever originated the translation, and its path
is already decided by the conntrack entry.

**The declared cost.** Traffic DNATed by *somebody else's* table — a Docker
published port, a hand-written rule — also loses its exit assignment. That is a
behaviour change for a setup mixing the two, and it is the correct direction to
be wrong in: an unassigned connection takes the box's normal path and works,
while the alternative breaks it.

The guard is part of each rule's canonical text (`… unless dnat`) and not only of
its expression list, so a box still holding the older rules reads back as drift
and has them replaced on the next apply. `docs/port-forwarding.md` §6 has the
full trace.

### 3.6 The TPROXY form

For a proxy on this box, the packet is never routed:

```
ip  saddr @dev_proxy meta l4proto { tcp, udp } tproxy to 127.0.0.1:7893 \
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

### 3.8 IP forwarding, and the one machine-wide sysctl

Everything above describes what happens to a packet **addressed to somewhere
else**. With `net.ipv4.ip_forward` at 0 the kernel drops that packet before any
of it is consulted — the marks are never read, the tables are never looked up,
the SNAT rule never fires. A module that programs the entire forwarding path and
then leaves the box not forwarding is not being a good citizen, it is being
broken.

So **this module writes it**, and it is the one machine-wide sysctl in olr.

> `net.ipv4.ip_forward = 1`, whenever the module is enabled.

#### Against §3.4's *don't squat shared state*

That rule's own wording is what permits this. Sysctls are touched "only when a
module explicitly owns that concern", and *whether this box forwards* is owned
here and nowhere else. The rule exists to stop us changing behaviour on things
nobody handed us — and forwarding is not a property of an interface we were or
were not given, it is a property of the box the operator installed a router on.

The neighbouring keys stay per-interface and the distinction is exactly that:
`send_redirects` on an unadopted NIC is somebody else's business, and §5.2
reports `conf.all.send_redirects` rather than writing it for that reason.

#### Not keyed on an exit existing

The configuration that needs forwarding most is the one with **no exits at
all**: `default` unset means *"everything uses this box's own connection"*,
which is precisely a box that has to forward. It is also the shape every olr
install has before its first exit is added. Keying the sysctl on an exit would
have left the commonest working setup as the one that does not work.

#### Why the plain key and not `conf.<dev>.forwarding`

Per-interface forwarding exists and IPv4 honours it — the forwarding decision
reads the **ingress** device's flag — so the narrower write is available. Two
things argue against it while the module is young:

- It has to be set on **both** the arrival and the departure interface to pass a
  packet *and its reply*, so "the LAN interface" is not the answer; the set is
  "every interface any traffic we route enters or leaves by", which is a list
  this module does not reliably have until `link` grows networks (§2.5).
- Any third party writing the global key resets every per-device value
  underneath us. Docker, libvirt and k8s all do this on startup. The narrow
  write is the one more likely to be silently undone.

The narrow form is the better long-term answer and is not ruled out — §9 carries
it. What is not acceptable in the meantime is a router that does not route.

#### It is read back, not written once

`Observe` reads `net.ipv4.ip_forward` like every other piece of live state, so a
box that stopped forwarding because something else was installed shows up as
drift and `olr gateway` offers to put it back. Writing it once at apply time and
trusting it afterwards would make this the one setting in the module that can be
wrong without anybody being told.

#### IPv6 is deliberately absent

`net.ipv6.conf.all.forwarding` switches every interface out of host mode, and an
interface in router mode **stops accepting the RAs** this box may be getting its
own address and default route from. Turning it on would cost the box its own
IPv6 connectivity on exactly the deployment dns:§1 leads with. Doing it safely
needs `accept_ra=2` on the uplink, which needs an uplink object to hang it on.
That object now exists — `dial.Uplink` (`docs/dial.md` §1) — so this is no
longer blocked on anything but the work, and §9 carries it.

Exits block IPv6 by default (§5.4), so the gap fails visibly rather than
silently, and §9 carries the work.

### 3.9 Egress NAT, and the other thing a LAN needs

§3.8 is one of the two machine-level facts that have to be true before a network
behind this box reaches the internet. This is the other, and it is the one that
fails in a way nobody can read.

Take the arrangement `docs/dial.md` §1 describes: `enp1s0` is the uplink at
`192.168.1.2` via the modem at `192.168.1.1`, `enp2s0` carries the network `lan`
on `172.16.1.0/24`. Forwarding is on, the default route is in `main`, and a
packet from `172.16.1.5` leaves correctly — **with `172.16.1.5` still on it**.
The modem NATs it out, gets the reply, un-NATs it back to `172.16.1.5`, and has
no route for `172.16.1.0/24`. The packet dies there.

The symptom is *the router reaches the internet and nothing behind it does*,
which reads like DNS, or like a firewall, and is neither.

> `oifname "enp1s0" ip saddr { 172.16.1.0/24 } counter masquerade`

in `olr_nat`'s postrouting chain, one rule, with the source set built from the
networks `link` holds.

#### Where the interface comes from

`dial`'s uplink, read through a narrow view — design.md §4.1 draws that arrow
(`dial → gateway`) and this is the first thing that walks it. **No uplink, no
rule.** The reference topology (dns:§1) puts olr beside the modem with the
default route owned by the distribution, and on that box olr has no business
guessing which interface faces outward.

That is also the whole answer to "why does the operator not type the next hop
twice". Before this existed, the only source of SNAT was an exit, so a box whose
LAN needed nothing more than *out the way this router already goes* had to
declare an exit repeating the uplink's own next hop. An exit is for choosing a
**different** way out; the default way out needs no exit and never did.

#### Why the source set, and not `oifname` alone

A consumer router writes `oifname "wan" masquerade` and stops. Scoping to the
subnets `link` declares costs one set and buys three things: traffic from Docker,
libvirt or k8s that happens to route out this way is not silently rewritten by
us; locally-originated traffic is untouched, which it should be because its
source is already the uplink's own address; and `nft list table inet olr_nat` at
2am says *whose* traffic this rule is for.

The subnets come from `link`'s networks — **intent, not the addresses observed on
an interface**. A network that has been declared and whose interface has not come
up yet still gets its rule, which is the same reason `dhcp` validates a range
against a network rather than against a NIC.

#### Not keyed on an exit existing

The same argument §3.8 makes about the sysctl, and it lands harder here: the
configuration that needs egress NAT most is the one with **no exits at all**.

#### It has an off switch, and that is not a lifecycle switch

`snat` on the module, nil meaning on — the same word and the same pointer
reasoning as `Exit.SNAT`, because it is the same question asked about the
default path instead of about an exit. A `*bool` rather than a `bool` so that
"the operator turned it off" and "this field was never written" stay
distinguishable.

Turning it off is a real configuration, not a hypothetical: an operator who has
added a static route for `172.16.1.0/24` on the modem wants the client's own
address to survive the trip, and masquerading would throw away exactly what they
set that route up to preserve.

What it is *not* is a second `Enabled`. §0 has the rule — a concern's condition
is its own configuration existing, and `Stats` is the single documented
exception.

#### Against per-exit SNAT

Both rules live in `olr_nat`'s postrouting chain, so where they could overlap —
an exit that is `next_hop 192.168.1.1 dev enp1s0`, the same path the uplink
already takes — **we order them ourselves** rather than inheriting whatever two
tables' hook priorities happen to resolve to. Conntrack binds a connection's NAT
once, so there is no double translation either way; what the ordering decides is
which source address wins, and that is a decision we get to make and write down
rather than discover.

#### IPv6

Absent, and not owed. A device behind this box holding a delegated global
address needs no translation to be replied to — `docs/port-forwarding.md` §7
makes the same point from the inbound side. What v6 needs instead is a filtering
policy, which §0 records olr is not building for now.

---

## 4. Domain rules are the proxy's, not ours

An earlier draft of this section routed by name, using the proxy's fake-IP range
as a destination prefix. That is out. **olr routes by source and by destination
prefix; it does not route by name**, and the mechanism is recorded as rejected
in §10 so it is not re-proposed on its merits — which were real.

The argument is not that it could not be built. It is that the operator already
owns a domain-routing engine:

> **Whoever runs a proxy router has already written their domain rules, in a
> tool built for it, with rule sources and geo/category sets we would never
> match. A second list in olr is two places that can disagree.**

And when they disagree it does not fail cleanly. It produces *"I put a domain
in olr and it still goes direct"*, whose cause is a rule inside the
proxy that won — a thing olr cannot see, explain, or show in a diff.

**So the division is by layer, and each side does the part it can do well.**
olr decides *which traffic reaches the proxy at all*: `Internet via` per network,
group or device (§2). The proxy decides *what to do with it*, by name, using its
own configuration. Neither needs to know the other's rules.

### 4.1 What the operator does instead

Assign the source to the proxy exit and let the proxy sort it out. For the
proxy's own domain rules to work it has to see names, and there are two ways
that happens. Both are worth stating: the first is a setting an operator has to
be told to make, and the second is what silently happens if they do not.

- **Point olr's resolver at the proxy's.** `olr dns set --mode forward --upstream
  <the proxy's resolver>` makes every answer the network gets come from the
  thing that also routes it, so its rules match on an exact name. This is the
  configuration to recommend, **with one condition attached**: the proxy must
  answer with real addresses — its real-address mode — and not fake ones.
  `upstream` is global (dns:§3), so a fake-IP proxy hands `198.18.x` to *every*
  device including the ones on `Internet via: Modem`, and those are blackholed.
  It presents as "the internet works on the laptop and not the tablet", which is
  a miserable thing to debug. Nothing is lost by asking: olr has no use for a
  fake IP anywhere, so real addresses cost the operator nothing they were
  relying on.
- **Otherwise the proxy sniffs.** TLS SNI and HTTP Host, per dns:§2.2, which
  works today and degrades as ECH deploys. Fine as a fallback, not something to
  design around.

An operator who genuinely wants fake-IP can still run it — that is their proxy's
business, and olr's rendered unbound deliberately does not strip `198.18.0.0/15`
from answers (`rebindPrefixes` in `internal/dns/render.go`). What they cannot do
yet is combine it with mixed exits, which waits on dns v2's per-client upstream
selection.

### 4.2 What this costs, stated

*"Send only one site through the proxy and everything else direct"* is **not
expressible in olr**. It is expressible in the proxy, which is where the rest of
that operator's domain policy already lives.

What it buys back is worth more than it looks:

- **`Internet via` becomes the only per-source question.** §2.4 drops from four
  objects to three, and the ladder covers all of what remains.
- **DNS policy and exit assignment are now independent.** The earlier draft had
  to forbid configuring them separately — a device handed a fake IP for an exit
  it does not use reaches that exit silently, so the resolver's upstream had to
  be derived from the routing decision. With no fake IPs there is nothing to
  desynchronise: blocking, the query log and `Internet via` are three unrelated
  settings, and an operator can reason about each without the others.
- **dns:§7.3 closes by deletion.** It asked which wins between a global domain
  rule and a per-source assignment. There is no longer a global domain rule.

---

## 5. Failure modes

Each of these presents as *"I set an exit and the internet stopped"*, so they are
plan-step checks and declared behaviour, not troubleshooting notes.

### 5.1 The ones that are checks

| | | |
|---|---|---|
| **NAT on the egress** | traffic out a new exit needs masquerade — `Exit.SNAT`, on by default for a next hop (§5.3); the default path's is §3.9's | plan *validates* and reports |
| **Next hop not directly reachable** | must fall inside a prefix we hold | catches entering the proxy's public address instead of its LAN one |
| **`rp_filter`** | drops asymmetric return traffic | this module owns the sysctl on its interfaces, declared |
| **MSS clamping** | tunnel and PPPoE egress | `olr_nat` is this module's now, so there is nowhere else for it to go |
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

The health check is a **through-path probe, not a ping**. A crashed proxy daemon
on a live box answers ARP and ICMP indefinitely while forwarding nothing —
worse, it loops our traffic back at us, because its own default gateway is us.

Failure behaviour is declared per §5.6, with hysteresis:

- `block` — **the default.** The UI says *"Living Room TV: no internet — Proxy is
  down"*, which is diagnosable.
- `direct` — silently leaks exactly the traffic the operator asked to route.
  Available, never the default.

dns:§1.2 raises the stakes on this: the argument that DHCP hands out olr and only
olr rests on olr being able to re-point a dead exit, because IPv4 has no
gateway failover at the device layer. So exit health is not a later refinement —
it is what the topology rule is trading against.

---

## 6. Coexistence with a proxy that wants to route

A proxy or VPN daemon in its automatic-routing mode (`auto-route` and its
equivalents) installs its own `ip rule` entries, route tables and nftables
rules. On the same host as olr that is two owners of one decision surface, with
the worst available failure mode: it works
until a version bump moves a priority number, and then some traffic silently
takes the wrong path.

The requirement is `auto-route: false`. It is a setting in a file we do not own
(§10), so:

> **Detect and refuse.** If adding an exit finds foreign `ip rule` entries
> pointing at a table carrying a default route, the plan does not proceed:
>
> *something else is managing routing on this box (4 foreign ip rules, priority
> 9000–9003, table 2022). olr cannot share the routing table with it. Turn its
> automatic routing off (`auto-route: false` or the equivalent) and retry.*

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
    ct direction original  update @dev_up   { ip saddr . meta mark }
    ct direction reply     update @dev_down { ip daddr . meta mark }
  }
}
```

The concatenated key gives per-device **and** per-exit from one structure,
because the mark is already there from §3.3. *"Living Room TV: 40 GB, of which 38
via Proxy"* costs nothing extra. With no routing installed the mark is 0 and it
degrades to plain per-device totals, so this table does not depend on that one.

**The direction match is what makes the address mean "a device here."** An
earlier draft of this section had the two `update` statements unqualified, and
that version is wrong in a way worth recording, because it looks right: on a
packet *leaving* for the internet, `ip daddr` is the far end. Unqualified, the
sets gained an element for every remote address the house ever contacted — the
device rows were correct, and beside each one sat a mirrored row for a web
server. Three costs at once: a usage list half full of addresses nobody
recognises, a set that fills its cap with strangers and then stops recording new
devices, and a full dump of all four sets on every read of the endpoint.

Matching `ct direction` first fixes it without a table of local prefixes to
maintain: the original direction is the one the connection was opened in, so its
source is the opener, and the reply direction's destination is that same opener.
Both rules key on the device, and each packet now matches exactly one of them
rather than both — which halves the per-packet work on the forward path.

The declared cost is a connection opened **from** the internet — a forwarded
port — where the opener really is the remote address, so it lands in the list
looking like a device. That is bounded by the number of forwarded ports rather
than by the size of the internet, and §7.4 states it. Untracked packets are not
counted at all, which is theoretical on a box whose classify chain already reads
`ct mark` on the same packets.

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

This applies uniformly, which it did not in an earlier draft: when olr routed by
name, proxy-routed traffic carried a 1:1 fake IP and attributed exactly, so the
ambiguity above was only the direct path's problem. With §4's decision every
flow attributes through the same many-to-many map, so there is one story about
accuracy rather than two — and the honest one is the weaker of the two.

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
- **A connection opened from the internet is counted against the address that
  opened it** (§7.1), so a forwarded port puts a public address in a list of
  your own devices.
- **The table has a cap**, and a full one goes on counting the devices it
  already knows while recording no new ones. That is §7.3's rule pointed at
  ourselves: the endpoint reports how full it is, because "this device uses no
  data" and "this device is missing" must not look the same.

### 7.5 Storage, and a stance

Counters are monotonic-since-load; everything anyone wants is deltas over time.

This was the second workload voting in §10 open decision 5, alongside
dns:§7.5's query log, and the vote is over: **there is no store.** The counters
are read from nftables when somebody asks, `statTimeout` ages them out in the
kernel, and olr keeps no copy. The decision and its price are recorded at §10
#5 — the price being that "traffic today" is answerable and "traffic last
month" does not exist.

The privacy paragraph below is most of why. Bounded retention over a fixed set
of devices is fixed-size data, and fixed-size data does not need a database; the
feature that would have needed one is the feature this section already refuses
to build.

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

## 8a. The write surface: one request per change

Every change names the one thing it changes, and the daemon does the whole
read-modify-write under the global apply lock (design.md §3.6).

| | |
|---|---|
| `PUT /api/gateway/exits/{name}` | add, replace — or **rename**, when the body's `name` differs from the path's |
| `DELETE /api/gateway/exits/{name}` | remove |
| `PUT /api/gateway/assignments/{interface}` | body `{"exit": "…"}`; `""` means *explicitly follows the box-wide setting* |
| `DELETE /api/gateway/assignments/{interface}` | stop overriding, so the row goes back to having no opinion |
| `PUT /api/gateway/forwards/{name}` | add, replace, or rename a port forward (`docs/port-forwarding.md`) |
| `DELETE /api/gateway/forwards/{name}` | remove |
| `PATCH /api/gateway/config` | `enabled`, `default`, `stats`, `snat` |
| `PUT /api/gateway/config` | the whole document — restoring a backup, or several changes at once |

**`enabled` is the sharpest field on this surface**, and it got sharper when
port forwarding moved here (§0): setting it false tears down `olr_route` *and*
`olr_nat`, so policy routing stops and every forwarded port closes in the same
request. The plan says so in those words and the impact is `disruptive`, because
a switch that silently shuts a port somebody is reaching a service through is
the one thing this surface must not do quietly.

**Why lists get their own routes and scalars do not.** A merge patch (RFC 7386)
merges an object key by key but replaces an array *wholesale*, so `enabled`,
`default` and `stats` are served perfectly well by `PATCH` while `exits` and
`interfaces` cannot be — a patch meaning to edit one exit would take the others'
traffic with it.

The deeper reason is that without item routes the *edit* happens in the client.
Every caller would load the document, splice the list itself, and send the whole
thing back: two requests where the lock covers only the second, and one rule —
`Config.Rename`'s cascade, `Config.Upsert` keeping an exit's slot — reimplemented
once per client. Renaming is the case that proves it. An exit's name is
referenced by `default` and by every assignment, so a rename is a change in three
places; a client that splices the array changes one and leaves the other two
naming an exit that is no longer there.

### Dry run, and the one interruption

Two query parameters, and they are independent.

- **`?dry_run=true`** — plan and answer, write nothing. The response is a plan,
  the same shape `POST /plan` returns, so one question has one answer whichever
  route it was asked down. This is what `olr --dry-run` uses.
- **`?confirm=true`** — go ahead with a change that would move traffic that is
  currently flowing. Without it, such a change is **not applied**: the daemon
  answers `409` with the plan, and the caller decides. This is design.md §5.1 and
  §5.3.3 resolved as *instant, except when it would disconnect you* — and it
  costs a second round trip only in the case that earns one.

`olr` sends `confirm=true` on every change. §5.1 gives the CLI no staged commit,
so a command that answered "this would be disruptive, run it again" would be one
by another name; `--dry-run` is how you look first. The WebUI does not, because
it has somebody to ask.

`POST /apply` is exempt. It re-programs intent already stored — and already
confirmed when it was stored — so there is no new decision to put to anyone.
Gating it would mean a box whose rules somebody flushed needs an extra flag to be
repaired, and that is the box that most needs repairing.

### 409 means two things

Both are refusals that wrote nothing, and the body tells them apart:

- `plan.blocked` is set — §6's refusal. Another program owns the routing table.
  Not a decision the operator can make here; they have to go and resolve it in
  that program's configuration.
- `plan.impact` is `disruptive` — the confirm gate above. Repeat with
  `confirm=true`.

A second status code was considered and rejected: a client has to read the plan
in either case to say anything useful on screen, so the code would not save it
any work.

---

## 9. Scope

| | | |
|---|---|---|
| **v1** | exits: `next_hop`, `interface`, `blocked` | `interface` is nearly free once `next_hop` exists, and it is how WireGuard and Tailscale arrive |
| | `Internet via` at **network** level | group and device tiers wait on §10 #6 |
| | nft classify + RPDB, documented mark/priority/table ranges | |
| | `net.ipv4.ip_forward`, written and read back | §3.8 — the module programs the forwarding path, so it owns whether the box forwards |
| | per-exit health probe, `block` on failure | dns:§1.2 depends on it |
| | named counters, the `ipv4_addr . mark` set | |
| | foreign `ip rule` detection and refusal | |
| | **egress NAT on `dial`'s uplink**, with an off switch | §3.9 — the other half of what a LAN needs, and what stops the next hop being typed twice |
| | **port forwards, and hairpin NAT** | `docs/port-forwarding.md`, moved here from the deleted `firewall` module |
| **v2** | `local_socket` (TPROXY) | wants dns:§2.1's return-path answer settled first |
| | IPv6 forwarding | §3.8 — needs `accept_ra=2` on the uplink; `dial.Uplink` now exists, so this waits only on the work |
| | per-interface `conf.<dev>.forwarding` in place of the global key | §3.8 — the narrower write, once `link`'s networks say which interfaces traffic enters and leaves by |
| | group and device tiers of the ladder | |
| | conntrack-derived per-flow detail | |
| **Later** | multi-WAN failover policy beyond `block` / `direct` | hysteresis and probe design are their own scope |
| | an advanced source+destination rule list | the only thing the ladder cannot express |
| **Never** | our own proxy engine, or wrapping one | §10 |
| | filtering, zones and rules | not this module's and not olr's for now — `docs/port-forwarding.md` §0 |
| | routing by domain name, by any mechanism | §4 — it is the proxy's job, and doing it too is two rule lists that disagree |

---

## 10. Considered and rejected

- **Wrapping a third-party proxy router as a managed backend.** Structurally
  legal — a separate unit, rendered config, supervised like dnsmasq. It fails on
  packaging and cadence: these tools are not in Debian, so we would be
  distributing a proxy binary and its CVEs, and they make breaking config changes
  across minor versions, which is fine for a human with a migration guide and
  expensive for a renderer that must survive `apt upgrade`. The operator runs it;
  we route to it. If it is ever revisited, the candidate is whichever one has the
  most stable config format.
- **A userspace TUN router as the base layer, with no nftables at all.** Such a
  tool is more featureful at routing, and that is not the question. Everything
  through a userspace TUN costs: inbound port forwards stop working (a TUN is
  outbound-only), a client's own IPsec breaks, ICMP becomes emulated so the tools
  people debug with lie, and every byte crosses a userspace stack with no
  offloads. mDNS was *not* a valid
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
- **Routing by the proxy's fake-IP range.** The mechanism §4 used to be built
  on, and the reason it is recorded rather than merely dropped is that it was
  genuinely elegant: a fake IP is a destination prefix, the RPDB routes prefixes
  natively, and it needed no set population, no dnstap, and no DNS interception
  beyond the `:53` hijack we do anyway. What it cost was a permanent structural
  coupling — the resolver's upstream had to be derived from the routing decision
  for every device, forever, or a device silently reached an exit its own
  setting said it did not use. Paying that to duplicate a feature the operator's
  proxy already has, better, was the wrong trade. §4 has the full argument.
- **Populating nftables sets from the resolver** (dnstap, dnsmasq's `nftset`, or
  our own relay, which already parses every answer and would be the natural
  place). The remaining way to route by name once fake-IP is gone, and rejected
  on its own merits rather than by the old "§4 makes it unnecessary": the set
  update races the client's first packet. A rule that works on the second
  connection and not the first is worse than no rule, because it presents as
  flakiness rather than as a missing feature, and the operator debugs their
  network instead of reading a scope table.
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
3. **Who owns the device inventory** (§10 #6). Gates the group and device tiers of
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
7. **Whether egress NAT should be per-network rather than one rule for all of
   them** (§3.9). One network translated and another not is a coherent thing to
   want — a lab subnet the modem has a static route for, beside a guest network
   that should look like the router. Today the switch is module-wide, which is
   the smaller thing that covers the case somebody actually reported. If a
   second person asks, the field moves onto the network rather than growing a
   list beside it.
8. **What `Enabled` should be called**, now that it governs two tables and one
   of the module's four concerns escapes it (§0). The honest name is no longer
   "enabled"; renaming it is a stored-document change, and the answer so far is
   that a clear sentence in §0 and on the switch is cheaper than the migration.
   Revisit if a third concern lands.
