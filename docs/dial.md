# `dial` module design — the box's own way out

Status: **part built.** `internal/dial` is the module. The static uplink and
dynamic DNS are built; the DHCP-client, PPPoE and LTE forms, IPv6 and prefix
delegation, and multi-WAN are owed. Bare section references are to this
document; references to `design.md` name it, and `gateway:` means
`docs/gateway.md`.

`docs/ddns.md` is the other half of this module and was written first, as its
own design. This document is about the module: what it owns, and why the uplink
is an object rather than a field on something else.

---

## 1. The object: an uplink

> **An uplink is how this box itself reaches the internet.**

Not how the networks behind it do — that boundary is `gateway`'s, and §5 is
about why the distinction is the whole design rather than a technicality.

One object, at most one of them, and it may be absent. Absent is the reference
topology (dns:§1): olr beside the modem, serving DHCP and DNS for a network
somebody else routes, with the default route put in the main table by the
distribution. That arrangement is supported, is what `docs/install.md` walks
through, and is the one this product leads with.

### 1.1 What it owns, stated plainly

When an uplink is configured, olr owns three pieces of kernel state, and — when
it is static — two pieces of the host's own configuration:

| | Owned |
|---|---|
| The interface's IPv4 address | yes — written, and restored after a reboot |
| The interface's admin state | yes — brought up, never down |
| The default route in the main table | yes — `RouteReplace`d, and restored after a reboot |
| The distribution's DHCP client, for IPv4 on that interface | yes, when static — stopped there (§4) |
| `/etc/resolv.conf` | yes, when static and `dns` is set — §4 |
| Any *other* address on that interface | **no** — reported, removable by name, never removed by an apply |
| Source NAT for the networks behind it | **no** — `gateway` does it, off this interface (§5) |

The third row is the one that needed an argument, because design.md §3.4 says
*"don't squat shared state: `/etc/resolv.conf`, the main route table, and
sysctls are touched only when a module explicitly owns that concern"*. That is a
condition rather than a ban, and the precedent is `gateway` turning on IP
forwarding (gateway:§3.8). This module explicitly owns that concern; §3 is what
it took to say so honestly.

### 1.2 Why it is an object and not a field on a network

`link` already models an interface that this box has an address on. It would
have been one field cheaper to add a gateway to that.

It would also have been wrong, and the wrongness has a name: a `link` network is
**one this box serves** (design.md §4.4). Every module downstream keys off
one — `dhcp` serves a range on it, `dns` derives `allow_from` from it,
`gateway` masquerades out of it and knows a forward's destination is inside it,
`wifi` will attach a radio to it — and design.md §5.6 makes *"we never serve DHCP on a WAN
interface"* a structural exception that "follows from role rather than from
observation". A WAN interface inside a network would be one every one of
those modules has to special-case, on the strength of a field none of them can
see.

There is a second reason that has nothing to do with taste. `internal/link`'s
`PlanAddrs` claims **complete IPv4 ownership** of a network member: any address it
did not put there is removed on the next apply. It bounded that claim with "WAN
interfaces are `dial`'s and are never members" — and until this object existed
that sentence was vacuous, because `dial` had nowhere for one to live. So the
only place an operator could put their modem-facing NIC was a network, and
`link` then stripped the address the ISP had given it. An operator with three
NICs reported exactly this and concluded that olr could not be brought to a
working state. They were right.

The two cannot both own one interface's addressing, and the validator refuses
the overlap rather than resolving it. The refusal names the network and says to
remove it first, because that is a migration and not a typo.

### 1.3 Why singular

Multi-WAN is real and is not here. It needs a selection policy — gateway:§2.1's
ladder — plus metric and failover semantics that exist nowhere in this tree yet,
and inventing the storage for them before the behaviour is the retrofit
design.md §9 warns about.

The field is a pointer to one object, so the day those land it becomes a list
without the meaning of an already-stored document changing. `gateway`'s `Exits`
is the shape to copy.

---

## 2. Static only, and what that costs

The uplink ships in one form: an interface, an address with its mask, and a
gateway. `dial`'s job description in design.md §4 is "WAN: DHCP client, PPPoE,
static, LTE; IPv6 PD", and three of those four are still owed.

That is a scope decision rather than an oversight. The three missing forms share
a shape the static one does not have: each brings a **backend to supervise**
(`dhcpcd`, `pppd`, `ModemManager`), an **on-demand dependency** to install, and
an **address discovered rather than typed** — which means a read path, a unit,
and a status that can say "dialling". Roughly twice the work, and none of it is
what unblocks the report above.

What the static form costs an operator today: a box whose ISP hands out an
address by DHCP has to keep letting the distribution do that, and must not set
an uplink for that interface. `Uplink.IPv4` being optional is the seam those
forms will arrive through — an uplink with no IPv4 block already means "olr owns
this interface and writes no address", which is exactly what a DHCP-client
uplink stores.

`Uplink.IPv6` is absent for the reason `link.Network` has no IPv6 field: there is
no v6 write path behind it. A field that three generated surfaces offer and the
writer cannot honour is worse than its absence.

---

## 3. Mechanism

Three operations, in this order, and the order is the argument.

1. **Add the address.** Additive — see §3.1.
2. **Bring the interface up.** After the address, so the link comes up already
   carrying one.
3. **Replace the default route.** Last, because it is the only one of the three
   that can be true and useless: a route out of an interface with no address is
   a route nothing can use.
4. **Retire the previous uplink's address**, when the uplink moved to another
   interface or another address. After everything that replaces it, and only
   when all of that landed — until then the old address may be the only way
   this box reaches anything. See §3.3.

`RouteReplace`, never delete-then-add. Replacing the default route is the one
operation that must not leave a window with no route at all, and `replace` is
atomic. `internal/link` solves the same problem the other way — add before
remove — because an interface can hold two addresses at once; the main table
holds one default route, so that trick does not apply here.

Written through netlink, like `link` and `gateway`. No `ip` subprocess
(design.md §3.6), and no file rendered — the uplink is the second module after
`link` with no unit to drive.

### 3.1 Foreign addresses are reported, never removed

This writer deliberately does **not** take `PlanAddrs`' ownership claim.

The uplink is the single interface a distribution's DHCP client is most likely
to also be acting on, and stripping what it put there is the failure this whole
object exists to stop. So an address on the uplink that olr did not write is
surfaced — in the plan, in `olr dial show uplink`, and on the Networks page —
and left exactly where it is.

The asymmetry is the safe one, and it is the same one
`link.Desired.AddOnly` argues for: too many addresses is a state an operator can
see and fix from a shell they can still reach, too few is one that takes the
shell away.

"From a shell" turned out to be the problem. The commonest foreign address on
an uplink was olr's own — a network removed before networks took their
addresses with them — and an operator working from the web UI had no way to
remove it. So the Networks page offers to remove each one by name, through
`link`'s `DELETE /api/link/interfaces/{name}/addresses/{address}`. An apply
still never removes one; a person naming it does.

### 3.3 Moving the uplink takes the old address with it

Changing the uplink to another interface, or to another address on the same
one, retires the address olr wrote for the previous uplink. Handing the uplink
back does not, and neither does changing to an uplink with no static address:
§2's argument about `rm uplink` holds for both — olr never recorded what the
box had before, so taking the address away could only leave it with less.

Moving is different because the replacement is already in place when the old
address comes off, and an address olr wrote and no longer claims is exactly the
leftover nobody can tell from somebody else's. On the box that forced this it
was a second interface on the same subnet, answering for an address nothing
routed to.

The plan shows the removal on the interface it happens on, as disruptive, and
names the address when the request arrived over it.

The status also says whether the gateway answers. A default route via a gateway
on the wrong segment matches the config exactly and carries nothing; that box
showed green until the kernel's neighbour table was read, and now reads
"192.168.1.1 is not answering on ens19. It answers on ens18".

### 3.2 Restored at startup, because the kernel forgets

An address and a route are kernel state, and a reboot takes both. olrd puts them
back on start, alongside `link`'s addresses and ahead of everything written
against them.

`link` was missing from that list once and the consequence was three modules
visibly broken with nothing in olr saying why. The consequence here would be
worse: what the box comes back without is how anybody reaches it to notice.

### 3.3 Removing an uplink tears nothing down

`olr dial rm uplink` stops olr *owning* the way out. It does not take the
address off the interface and does not delete the default route.

That is deliberate and it is not symmetry with `link`, which does remove a
network's address. olr never recorded what the main table held before it claimed
it — `Applier.Restore` refuses to save state for the same reason — so a teardown
could not put the previous route back, only leave the box with none. The usual
reason to remove an uplink is to hand the interface to something else, and
taking the route away first is the wrong opening move.

What stops is the restore after a reboot, and the plan says so in those words.

---

## 4. Taking the interface from the distribution, and `/etc/resolv.conf`

olr writes addresses and routes straight into the kernel and never goes through
ifupdown, dhcpcd, systemd-networkd or NetworkManager. Until this section
existed, whatever the distribution had been told about the uplink's interface
kept running beside it. The box that forced the change had `iface ens18 inet
dhcp` in `/etc/network/interfaces`, so ifupdown started dhcpcd on the uplink at
every boot; dhcpcd never got a lease, gave ens18 a 169.254 address and a default
route through it, and wrote an empty `/etc/resolv.conf`. The router reached the
internet by address and resolved nothing.

A static uplink replaces the DHCP client that would otherwise have supplied the
interface's address, the default route **and the resolvers**. So once it is
static, olr takes all three, through `internal/host`:

- **IPv4 on the interface.** The distribution's DHCP client stops doing IPv4
  there and keeps doing IPv6, which olr does not configure. For dhcpcd — what
  Debian 13's ifupdown runs, and Raspberry Pi OS — that is an `interface X` /
  `ipv6only` block in `/etc/dhcpcd.conf`, between olr's markers, and a
  `SIGHUP` — dhcpcd's `--rebind` — to the running dhcpcd so it re-reads now
  rather than at the next boot. Never `SIGALRM`, which is its `--release`: it
  de-configures the interface, IPv6 included, and exits. The first version
  sent that, and it did. Anything it had put on the interface goes; the plan says so before the
  change is confirmed.
- **The box's resolvers**, from `dns` — usually the modem. Where
  `/etc/resolv.conf` links into systemd-resolved, resolved stays the box's
  resolver (design.md §3.4 never stops an OS component) and gets them through
  `/etc/systemd/resolved.conf.d/20-olr-resolvers.conf`. Where it is a plain
  file, olr writes it — keeping the operator's `search` and `options` lines —
  after keeping the original to put back, and tells dhcpcd `nohook
  resolv.conf` so no lease rewrites it.

Given back by removing exactly what olr added: handing the uplink back, or
clearing `dns`, removes olr's blocks, tells dhcpcd again, and restores the
original `resolv.conf`. olrd converges all of this at every start, so a box
upgraded into it, or one whose `dhcpcd.conf` a package upgrade rewrote, is put
right without anybody saving anything.

The same takeover applies to every **network member with a subnet** — the other
place olr writes an IPv4 address — which is what finally retires docs/install.md's
rule that a member must never be an interface the distribution addresses by DHCP.

**Not taken over yet**: NetworkManager, systemd-networkd, dhclient (Debian 12's
ifupdown), and a `resolv.conf` maintained through resolvconf. Each is detected
and reported on the uplink card or the interface list, with the step that would
stop it, and nothing is changed. Each one's switch is a different file told to
re-read a different way, and shipping them untested would mean editing the
configuration a box boots with on the strength of a reading of its manual.

The resolvers are this box's own, not olr's resolver's upstream:
`dns.Upstream` still defaults to `ModeRecurse` and resolves from the root. The
§4.1 arrow `dial → dns (upstream resolvers)` is still unbuilt.

---

## 5. What an uplink does *not* do, and the trap that follows

**Setting the uplink gets this box onto the internet. It does not, by itself,
get the networks behind it there** — but the remaining step is not the
operator's, and that is a deliberate change from how this first shipped.

A packet from a LAN client that leaves through the default route leaves with its
LAN source address still on it, and the modem has no route back for that prefix.
The box reaches the internet and nothing behind it does, and the symptom reads
like DNS, or like a firewall, and is neither.

What closes it is **egress NAT, owned by `gateway`** (gateway:§3.9): one
masquerade rule on the uplink interface, scoped to the subnets `link` declares,
written whenever `dial` has an uplink. `gateway` reads the interface from here
through a narrow view, which is design.md §4.1's `dial → gateway` arrow walked
for the first time.

So the whole of the two-NIC setup is:

```sh
sudo olr dial set uplink --interface enp2s0 \
  --address 192.168.2.9/24 --gateway 192.168.2.1
sudo olr net add lan --member enp1s0 --subnet 172.16.1.0/24 --router 172.16.1.1
```

### 5.1 What this replaced, and why it is worth recording

For one release the answer was *"add a `gateway` exit pointing at the same next
hop, and assign your networks to it"*, because SNAT existed only on an exit. It
worked, and it was wrong in two ways worth keeping written down:

- **The operator typed the modem's address twice**, once here and once in an
  exit, with nothing keeping the two in step.
- **It made an exit mean two things.** An exit is for choosing a *different* way
  out (gateway:§1.1); making the default way out also require one meant the
  object could not be explained by its own definition.

`dial` still cannot see `gateway`'s configuration — the arrow points the other
way, and inverting it would be a cycle — so it could never have checked whether
the work had been done. The reminder was a plan note for exactly that reason,
shown once at the moment of the change. **That note is gone**, along with the
step it was reminding anybody about.

The masquerade is `gateway`'s and not `dial`'s because `gateway` owns the
nftables tables at this boundary (design.md §4.2: each module writes its own
table). Putting it here would have meant a second module writing NAT rules, and
`dial` opening a table for one line.

---

## 6. Surfaces

| | |
|---|---|
| `GET /api/dial/uplink` | intent beside fact — see §6.1 |
| `PUT /api/dial/uplink` | set it; disruptive when it takes over a route |
| `DELETE /api/dial/uplink` | stop owning it (§3.3) |
| `olr dial show uplink` | the same two halves, as text |
| `olr dial set uplink` | `--interface`, `--address`, `--gateway`, `--dns` |
| `olr dial rm uplink` | |
| Networks page, third section | beside Interfaces and Networks |

The uplink is on the **Networks** page rather than under Gateway, which is where
the reporter looked first. Gateway's blurb — "how each network reaches the
internet" — reads exactly like where this would be, so that page now says which
of the two it is and links here. The uplink sits on Networks because everything
you do with an interface is there: hand it over, then make it either a network
this router serves or the one way out.

### 6.1 Intent beside fact, never collapsed

Every read of the uplink returns both halves: the address and gateway olr was
*told*, and the addresses and default route the kernel actually *has* — the
latter read per request and never stored (design.md §4.5).

They are never reduced to one verdict, for the same reason `docs/ddns.md` §6
keeps its three questions apart. The failure worth catching is the one where the
configuration looks perfect and the default route leaves by a different
interface, and a single "ok" boolean is exactly what hides it.

The three states that must not read alike:

- no default route at all — this box cannot reach the internet;
- a route going where olr asked — as set;
- a route going somewhere else — configured and not in force, which is drift
  (design.md §5.4) and is fixed by applying again.

---

## 7. Failure modes

**Changing the uplink can lock the operator out**, in two ways `link`'s address
changes have only one of. Moving the address drops anything connected to it;
replacing the default route drops anything reaching this box from *outside* the
house, because the request changing the route arrives over the route being
changed. Both are classified `disruptive`, both stop at a confirmation in the
UI, and both carry a warning naming what is about to move.

design.md §5.5's lockout guard — the dead-man's switch that reverts a change
nobody confirms — **is not built**. Until it is, the plan and that confirmation
are the entire safety net. This is the same position `internal/link` is in and
is recorded in both places.

**A gateway outside the address's subnet is refused**, not warned about. An
off-link next hop needs a route to itself first and olr writes none, so the
kernel would refuse with `ENETUNREACH` halfway through an apply — a much worse
place to learn that the mask is wrong than the form.

**A private uplink address is a warning, never a refusal.** Double NAT is a
working setup; the port-forwarding half of `gateway` makes the same call about
the same fact (`docs/port-forwarding.md` §5.4). What
it costs is inbound: nothing on the internet can open a connection to this box
unless the device in front forwards it.

**The network address as the uplink address is refused.** `192.168.2.0/24` is
the network; `192.168.2.9/24` is an address on it. The field is deliberately not
masked on the way in — which is the opposite of `link.GroupIPv4.Subnet`, one
module away and looking almost identical — because masking here would quietly
turn the box's own address into something netlink accepts and nothing can reach.

---

## 8. Still open

1. **The DHCP-client, PPPoE and LTE forms.** §2 has what they cost.
2. **IPv6, and prefix delegation.** design.md §4.3 wants the delegated prefix to
   reach `dhcp`; nothing here reads or stores one.
3. **Multi-WAN**, and with it failover — which needs a health signal, and
   `gateway` already has one for exits (gateway:§5.5). Whether they are one
   mechanism is the interesting question and is not answered.
4. ~~Who owns `/etc/resolv.conf`.~~ **Closed.** The uplink, when it is static
   and names resolvers (§4).
5. ~~Whether the `gateway` exit should be offered alongside the uplink.~~
   **Closed.** The question only existed because source NAT lived on an exit;
   gateway:§3.9 moved it onto the uplink interface itself, so there is no second
   object to offer and no §5 trap to close with a checkbox. §5.1 records what
   the old answer cost.
6. **NetworkManager, systemd-networkd and dhclient** on an interface olr has
   taken — detected and reported, not yet taken over (§4).
