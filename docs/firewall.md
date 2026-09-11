# `firewall` module design — port forwarding

Status: **built, and named for more than it does.** `internal/firewall` owns one
object: a **port forward**. There are no zones, no rule lists and no filtering
policy, which is most of what `design.md` §4 gives this module eventually.
Section references are to `design.md` unless prefixed `gateway:`, which means
`docs/gateway.md`.

The name is the one `design.md` §4 already reserves for this territory —
"nftables `olr_filter`, `olr_nat` — zones, rules, NAT, forwards". Calling the
module `nat` today would mean renaming it the week filtering lands, and the
repository has a precedent for living with a name that is ahead of its contents:
`internal/link`'s package comment opens by saying it is not the module its name
promises and owns exactly one field. This is the same bargain. What the module
is *for* is stated in its own package comment, and the scope table in §8 is the
contract.

One sentence is the whole feature:

> **Something on the internet connects to this router on a port, and the
> connection is delivered to one device on your network instead.**

---

## 1. The object: a forward

### 1.1 Definition

> **A forward is a standing instruction to redirect connections arriving from
> outside, addressed to this router on one port, to an address and port inside.**

Four properties and a name, and each answers a question the operator already
has:

| Field | Operator's question | Example |
|---|---|---|
| `name` | what do I call this | `web` |
| `in` | where does it arrive | `wan0` |
| `protocol` | `tcp`, `udp` or `both` | `tcp` |
| `port` | on which port | `8080`, or `30000-30010` |
| `to` | and goes where | `192.168.1.10:80` |
| `hairpin` | does it work from inside too | on by default (§4) |

`in` is an interface rather than "the internet", and that is not a placeholder
for a better word. A box can have two uplinks, and a forward that silently
applied to both would be a policy nobody wrote. Naming the interface makes the
scope of the rule exactly as wide as the operator said it was — and it is the
same staging argument `gateway:`§2.5 makes: the field becomes a network name
when `link` grows groups, and the stored document does not change shape.

### 1.2 The destination is an address, not a device

`to` stores `192.168.1.10:80`. It does **not** store a reference to a device in
the `devices` inventory, and that is a decision rather than an omission.

A forward is a kernel rule that has to exist the moment a packet arrives, at
boot, before any lease has been handed out and while the device is switched off.
A device reference would have to be resolved to an address at apply time, so the
rule would either be absent until the device appeared — which is a port that
works some mornings — or be programmed against a remembered address, which is a
device reference that has quietly become an address anyway, with one more place
for the two to disagree.

What the operator loses is the case where the device's address changes and the
forward silently points at whatever took it over. The answer to that is a fixed
lease, which `dhcp` already has, and the surfaces say so: the WebUI's dialog
offers a device picker that fills the address in and **warns when that device has
no fixed address**, and `olr firewall add forward` accepts the address the
operator can see in `olr devices show`. The stored value is an address either
way. The picker is a convenience for typing it, not a second model.

### 1.3 Port ranges may not be shifted

A single port may be remapped freely — `8080` outside to `80` inside is the
motivating case and the most common one. A **range** may only be forwarded to
the same range.

This is a kernel property surfaced rather than a policy we chose. Netfilter's
NAT core keeps the original port when it already falls inside the requested
range (`l4proto_in_range` in `get_unique_tuple`), which is what makes
`30000-30010 → 192.168.1.10:30000-30010` an exact identity mapping. Ask it to
shift a range instead — `30000-30010` outside to `40000-40010` inside — and the
original port is *not* in range, so netfilter falls through to picking a free
port arbitrarily. Connections still arrive; they arrive on unpredictable ports.

That is the worst available failure: it works, and it delivers to the wrong
place, and nothing in the configuration looks wrong. So validation refuses it
with the reason, rather than programming a rule whose behaviour we would then
have to explain. An operator who genuinely wants a shifted range writes one
forward per port, which is honest about what the kernel is being asked to do.

---

## 2. The operator model

There is no ladder here and no inheritance. A forward is a flat, independent
thing: it either matches a packet or it does not, and two forwards cannot
disagree about one packet because validation refuses the overlap (§5.1) rather
than resolving it.

That is a smaller model than `gateway`'s deliberately. *"Where does my phone go?"*
needs a ladder because every device has an answer whether or not anybody set one.
*"What reaches my NAS from outside?"* has the answer **nothing** until somebody
writes it down, and a list of what somebody wrote down is a complete account of
it. Adding structure to that would be modelling for its own sake.

### 2.1 Everything else stays shut, and we do not say otherwise

olr has no filtering policy. It does not maintain a default-deny stance on the
forward hook, and this module does not create one. What keeps the rest of the
network unreachable from outside is that the addresses inside are private and
nothing routes to them — plus whatever `forward`-hook policy the box already has
from somewhere else (§6).

This matters because it bounds what a forward *means*. Creating one does not
"open a hole in the firewall", because there is no firewall of ours for it to be
a hole in. It installs a NAT translation. If something else on the box is
filtering forwarded traffic, that filter still applies and the forward may not
reach — which is exactly §6, and it is reported rather than worked around.

---

## 3. Mechanism

### 3.1 The ruleset

```
table inet olr_nat {
  counter fwd1

  chain prerouting {                # type nat hook prerouting priority dstnat
    iifname "wan0" meta nfproto ipv4 fib daddr type local \
        tcp dport 8080 counter name "fwd1" dnat ip to 192.168.1.10:80
    iifname "lan0" meta nfproto ipv4 fib daddr type local \
        tcp dport 8080 dnat ip to 192.168.1.10:80
  }

  chain postrouting {               # type nat hook postrouting priority srcnat
    ip saddr 192.168.1.0/24 ip daddr 192.168.1.10 tcp dport 80 masquerade
  }
}
```

Four details in there are load-bearing, and each is a mistake somebody makes by
hand.

### 3.2 `fib daddr type local`, not the WAN address

The obvious rule matches the router's public address. That address comes from
DHCP or PPPoE and changes, so the obvious rule is one whose correctness expires
without warning — and re-rendering it on every address change would make the
firewall table churn every lease renewal.

`fib daddr type local` asks the kernel the question we actually mean: *is this
packet addressed to this box?* It is true for whatever address the uplink
currently holds, for a second address added later, and for the loopback — and it
is false for traffic merely passing through, which is how it also keeps a forward
from capturing transit traffic that happens to use the same port.

### 3.3 `meta nfproto ipv4` on every rule

The table is `inet`, which sees both families. §7 explains why there is no IPv6
forwarding, but the table is `inet` anyway so that the day filtering arrives it
does not need a second table.

That makes the family guard mandatory rather than tidy. Without it, an IPv4
payload offset applied to an IPv6 packet reads whatever is at that offset in the
v6 header. `internal/gateway/kernel_linux.go`'s `sourceExprs` carries the same
guard and says the same thing; this is the second module to need it and it will
not be the last.

### 3.4 Named counters, one per forward

Anonymous inline counters are zeroed when the chain is re-rendered, and this
module re-renders its whole table on every apply (§3.6). So adding a second
forward would silently reset the first one's numbers — `gateway:`§3.3's lesson,
which cost a bug there and is being paid forward here.

*Has this forward ever been hit?* is the first question anybody asks when a port
does not work, and it separates "the packet never arrived" from "the packet
arrived and something further in dropped it". Those have completely different
next steps, so the counter is worth an object.

The counter is named from the forward's **slot** — a small integer allocated on
save and kept for the forward's lifetime — rather than from its name. Same
reasoning as `gateway:`§3.2's slots: renaming a forward should not reset the
number somebody is watching, and inserting a forward that sorts earlier should
not renumber the others. It is also the only spelling that is certainly a legal
nftables identifier, which an operator-chosen name is not.

### 3.5 The hairpin pair

Two rules, and they are one feature. §4 is the whole argument.

### 3.6 The table is replaced wholesale, in one transaction

`olr_nat` is deleted and rebuilt on every apply, in a single netlink batch, which
is what `internal/gateway/kernel_linux.go` does with `olr_route` and for the same
reasons: the batch is atomic so no half-built ruleset is ever visible, and a
hand-edit inside our table is corrected on the next apply.

`nft flush ruleset` remains banned outright (§3.4). We delete one table, by name,
and nothing else on the box is touched.

---

## 4. Hairpin NAT, and why it is on by default

The complaint is universal and never phrased as a bug report: *"I set up the port
forward and it works from my phone on mobile data, but not from my laptop at
home."*

A client on the LAN looks up the operator's dynamic-DNS name, gets the router's
public address, and connects. The DNAT rule as written matches `iifname "wan0"`,
so it does not fire, and the connection goes to the router itself and is refused.

Adding the LAN-side DNAT is half the fix and produces a subtler failure. The
client at `192.168.1.20` now has its packet rewritten to `192.168.1.10`, which is
on its own segment — so the server replies **directly** to `192.168.1.20` from
`192.168.1.10`, bypassing the router entirely. The client receives a reply from
an address it never sent to and drops it. The connection hangs rather than
failing, which is worse.

The masquerade is the other half: rewriting the client's source address to the
router's makes the server reply through the router, where conntrack can undo both
translations.

**It is on by default because off-by-default is a setting nobody discovers.** The
failure it prevents does not present as a missing feature — it presents as the
port forward being broken, tested from the one machine the operator has to hand.

### 4.1 What it costs, stated

The server sees **the router's address** as the source of every hairpinned
connection, not the real client's. A web server's access log fills with one
address; per-client rules on the server stop discriminating; fail2ban on the
server can lock out the router and with it the whole house.

That is a real cost and it is why `--no-hairpin` exists. It is the right choice
for an operator whose internal clients reach the service by its internal address
anyway — which is the better answer in general, and is what split-horizon DNS is
for. olr cannot make that choice for them because it does not know what their
DNS says, so it makes the choice that fails visibly rather than the one that
fails at 2am in someone else's log file.

The warning is on the surface, not only in this document: validation emits it for
every forward with hairpin on.

---

## 5. Failure modes

Each of these presents as *"the port forward does not work"*, which is why they
are checks and reported facts rather than troubleshooting notes.

### 5.1 The ones validation refuses

| | Why |
|---|---|
| Two forwards overlapping on `(in, protocol, port)` | genuinely ambiguous; first-match would be a precedence model nobody can see (§2.3 of `gateway`, one level down) |
| `to` is an address on this box | a forward to ourselves is a loop, and the operator meant a local service |
| `to` is unspecified, loopback or multicast | not a destination a device can hold |
| `in` has not been adopted | §7 — we do not program rules on an interface nobody handed us |
| A shifted port range | §1.3; the kernel would deliver to arbitrary ports |
| An IPv6 address in `to` | §7 |

### 5.2 The ones the plan reports, because they are somebody else's state

**Another table filtering the forward hook.** ufw, firewalld, Docker and a
distribution's own `/etc/nftables.conf` all install chains on the `forward` hook,
and several default to `policy drop`. In nftables a `drop` is final: our `accept`
in our table cannot override a `drop` in theirs, because every table sees every
packet and the most restrictive verdict wins.

So this is **not something we can fix**, and pretending otherwise would be worse
than saying nothing. The plan detects it and says so:

> *nftables chain `filter/FORWARD` in table `inet firewalld` has policy drop, so
> something else on this box is filtering forwarded traffic and this forward may
> not reach 192.168.1.10.*

The same move `gateway:`§6 makes with foreign `ip rule` entries, with one
deliberate difference: **gateway refuses and this one does not.** Two owners of
the routing table is a correctness problem that silently misroutes; a foreign
forward-chain policy is a thing that might be exactly what the operator
configured on purpose, and might already have an accept rule for this traffic
that we cannot evaluate. Refusing would block a legitimate setup on a guess. So:
report, apply, and let the counter (§3.4) settle the question.

**A local service already listening on the port.** Forwarding `wan0:22` while
`sshd` listens on `:22` is the one that costs the operator their session: DNAT
runs in prerouting, before the local delivery decision, so the rule takes effect
and the next SSH connection from outside goes to the other machine. The operator
is holding the connection it breaks.

`core.TCPPortInUse` and `core.UDPPortInUse` already exist for `dhcp`'s preflight
and answer exactly this, so the plan warns with the port named. It is a warning
and not a refusal, because "forward SSH to the NAS and administer this box from
the LAN" is a legitimate thing to want — but it is never a thing to do by
accident.

### 5.3 The one that is ours and is a bug we had to fix

See §6. It is not a firewall failure mode; it is a `gateway` one that only port
forwarding makes visible.

---

## 6. The return path, and the `gateway` bug this exposed

**On any box that has an exit configured, port forwarding silently did not work
before this module landed.** The fix is in `gateway`, not here, and it is worth
recording in both documents because neither module is wrong on its own.

Follow a forwarded connection on a box where `lan0` is assigned to the exit
"Clash". `olr_route`'s classify chain runs in prerouting at mangle priority, and
`gateway:`§3.5 restricts it to forwarded traffic with `fib daddr type != local`:

| Packet | What classify sees | Result |
|---|---|---|
| Inbound `SYN`, before DNAT | destination is still the router's own address, so `fib daddr type != local` is false | mark 0 — correct |
| The server's `SYN/ACK` back | source `192.168.1.10` matches `lan0`'s source rule; destination is the public internet | **marked for Clash** — wrong |

The reply looks exactly like an ordinary LAN machine opening an outbound
connection, because conntrack does not restore the original source address until
postrouting. So the policy route sends it into the proxy, and the connection dies
half-open.

The fix is one guard added to `gateway`'s `sourceExprs`:

```
ct status & 0x20 (IPS_DST_NAT) == 0
```

> **A connection whose destination we rewrote is not one we choose an exit for.**

The semantics are general, which is what makes this a fix rather than a back door
for one module: the reply leg of any DNATed connection belongs to whoever
originated the translation, and its path is decided by the conntrack entry, not
by our source rules.

**The declared cost.** Traffic DNATed by *somebody else's* table — a Docker
published port, a hand-written rule — also loses its exit assignment. That is a
behaviour change for a setup that mixed the two, and it is the correct direction
to be wrong in: an unassigned connection takes the box's normal path and works,
while the alternative breaks it. `gateway:`§3.5 carries the same note.

---

## 7. IPv6 is deliberately absent

There is no `to` that may be an IPv6 address, no v6 form of a forward, and no
switch that mentions v6. This is the most likely thing in the document to be read
as an oversight, so the reasoning is here rather than only in §9.

IPv6 has no NAT and needs none: a device inside already has a globally routable
address, so "forwarding" a v6 port is not a translation at all. It is **a
filtering decision** — permitting inbound connections to an address that is
already reachable in principle.

olr has no filtering policy (§2.1). We do not maintain a default-deny stance on
the forward hook, so there is nothing for a v6 "forward" to open. Worse, an
`accept` in our table cannot override a `drop` in somebody else's (§5.2), so on
the boxes where inbound v6 *is* blocked — which is the only case where the
feature would mean anything — ours would not work.

> **A switch that appears on four surfaces and reliably does nothing is worse
> than a missing feature.** The operator configures it, tests it, finds the port
> shut, and debugs their network instead of reading a scope table.

So it waits for the filtering half of this module, where it is one rule in a
policy we own and can reason about. Until then `to` takes an IPv4 address and
validation says why.

---

## 8. Scope

| | | |
|---|---|---|
| **v1** | DNAT from one interface to one address | the object |
| | port remapping, and identity port ranges | §1.3 |
| | hairpin NAT, on by default, with a warning | §4 |
| | per-forward named counters | §3.4 |
| | foreign forward-policy detection, reported | §5.2 |
| | local-port conflict warning | §5.2 |
| **v2** | a source allow-list (`--from`) | belongs to filtering, not to NAT — see §9 |
| | IPv6 inbound permits | needs the filtering half to exist (§7) |
| | per-forward enable/disable without deleting | |
| **Later** | zones and a filter policy — the rest of §4's brief | |
| | the nftables reader `design.md` §4.2 gives this module for the whole box | which also closes `gateway`'s observation gap |
| **Never** | UPnP / NAT-PMP / PCP | §9 |

---

## 9. Considered and rejected

- **UPnP, NAT-PMP and PCP.** The protocols by which an application on the LAN
  opens a port without asking anybody. They are genuinely useful — games and
  consoles want them — and they are rejected because the object they create is
  not the object this module has. A forward here is *something the operator
  wrote down*, visible in a list, surviving a reboot, removable by the person who
  made it. A UPnP mapping is a hole punched by whatever software asked, with a
  lease, attributable to no person, and the security history is dire: malware
  inside the network opening its own inbound path is the textbook case. If it
  ever lands it needs its own object, its own list, its own expiry, and a
  per-device consent model — which is a feature, not a flag on this one.

- **A source allow-list (`--from 203.0.113.0/24`) in v1.** The most-requested
  neighbouring feature and the most tempting to add, because one `ip saddr` match
  in the DNAT rule appears to buy it. It is rejected on a boundary rather than on
  merit: restricting *who may connect* is filtering, and putting the first
  filtering rule in the NAT chain would mean the second one has nowhere to go.
  It would also be a filter that fails open in a way nobody expects — a source not
  in the list is not DNATed, which means it reaches **the router's own port**
  rather than being refused, so adding an allow-list would quietly expose a local
  service to the addresses it was meant to exclude. That is not a rule to ship
  ahead of the chain that would handle it properly.

- **Modelling the destination as a device reference.** §1.2.

- **A `dmz` or "forward everything to this host" object.** One rule, universally
  regretted, and it is expressible as a range forward if somebody insists. It is
  not worth a first-class object that teaches operators a worse habit than
  naming the three ports they actually need.

- **Matching the WAN address explicitly instead of `fib daddr type local`.**
  §3.2. It is what every hand-written example does, and it is why those examples
  break on the next lease renewal.

- **Refusing to apply when another table filters the forward hook**, as
  `gateway:`§6 refuses foreign `ip rule` entries. §5.2 has the argument: the two
  situations look alike and are not. Routing has one owner or it misroutes;
  filtering composes, and the foreign policy may already permit exactly this
  traffic.

- **Putting these rules in `olr_route`.** They have different lifetimes and
  different hooks, and folding them together would mean disabling routing policy
  took the port forwards with it. `design.md` §4.2's rule is one table per
  concern; `olr_nat` is this module's, as §4's brief already names it.

---

## 10. Open

1. **Whether `in` should accept "any uplink" once `dial` lands.** Today it names
   one interface. A box with a failover WAN wants both, and the honest spelling
   of that is a group, not a wildcard — so it waits on `link`'s groups rather
   than getting a special value now.

2. **Whether the local-port conflict check should be a refusal for the port the
   request arrived on.** Forwarding the port you are currently connected over is
   the lockout case `gateway:`§5.1 classifies as `disruptive`; here it is only a
   warning, because the connection is not moved, it is the *next* one that goes
   elsewhere. Arguably that is still worth the confirm gate.

3. **How a forward should read when the target device is offline.** The rule is
   correct and the connection is refused, which is right, but the screen could
   say so — it needs `devices` presence, and the join belongs on the status
   endpoint rather than in the rule.
