# `firewall` module — a default stance at the boundary

Status: **built.** Section references are to `design.md` unless prefixed.

One sentence is the whole feature:

> **Nothing outside your networks may start a connection to this router, or to
> anything behind it, unless something here asked for it.**

## 1. Why it exists now

Until IPv6, the networks behind an olr box were unreachable from outside by
accident: their addresses were private, and the egress masquerade gave nothing
outside a way in. That is not a firewall, and it stops being true the moment a
device has a global IPv6 address — every printer, NAS and camera on the network
becomes directly addressable from the internet. The router itself was never
covered: the web UI listens on `0.0.0.0:8080` and answers on the uplink too.

So filtering stopped being optional. What it did not need to become is
complicated.

## 2. The object: one switch

```json
"firewall": { "enabled": true }
```

That is the stored configuration, all of it. **Off by default** — installing olr
changes nothing until somebody says so (§7) — and an ordinary switch in both
directions. `olr firewall enable|disable|show|status`, `/api/firewall/…`, and a
page at Gateway → Firewall.

There is no rule list, and that is the design rather than a gap. A home router's
firewall is strong because of its default, not because of how many rules it
has — and every exception a house network needs is already written down
somewhere else in olr. So the module does not ask for them a second time.

### 2.1 Inside and outside

**Inside** is every member interface of a `link` network, plus the remote-access
WireGuard interface when it is enabled — a dialled-in phone is meant to be on
the network. **Outside is everything else**, including any interface that
appears later: a second NIC, a PPPoE session, a USB modem. Defined by what is
trusted rather than by what is not, so a misclassification fails closed.

Enabling with no networks at all is refused: nothing would be inside, and the
ruleset would drop the operator's own connection.

### 2.2 Openings are derived, never typed

What stays reachable from outside comes from the objects that need it:

| Opening | From |
|---|---|
| udp WireGuard port | `remote.wireguard`, when enabled |
| tcp (+udp) Shadowsocks port | `remote.shadowsocks`, when enabled |
| tcp SOCKS5 port | `remote.socks5`, only with `listen: internet` |
| tcp 80, tcp 443, udp 443 | `ingress`, when enabled (443/udp is HTTP/3) |
| IP protocol 41, **from the broker only** | `dial`'s IPv6 tunnel (`uplink.ipv6`), when set |
| a port forward's traffic | conntrack's DNAT status — the forward *is* the permission |

Removing the object closes its port on the next apply. There is no rule anybody
can forget to take out. olrd re-applies the firewall whenever `dial`, `link`,
`remote` or `ingress` announces a change (`internal/daemon/firewall.go`).

The tunnel's opening is the one that is not a port. 6in4 is its own IP
protocol, and it is limited to the broker's address because protocol 41 from
anywhere would let anybody inject IPv6 into the tunnel. It is needed even though
conntrack lets replies in: the tracking entry for protocol 41 lapses after ten
minutes of silence, and the first inbound IPv6 connection to a quiet tunnel
would be dropped. The tunnel device itself is outside, like every interface that
appears later, so IPv6 arriving through it gets the same stance as IPv4 arriving
on the uplink.

## 3. The ruleset

One table, `inet olr_filter`, so IPv4 and IPv6 are one policy. Two chains:

```
input   (to this router)            forward (through it)
  established,related  accept         established,related  accept
  from lo              accept         from <inside>         accept
  from <inside>        accept         to not <inside>       accept
  icmp, icmpv6         accept         port forwards (dnat)  accept
  dhcp, dhcpv6 client  accept         icmpv6 errors, echo   accept
  each opening         accept         drop (counted)
  drop (counted)
```

The choices that are not obvious:

- **ICMPv6 is not optional.** Neighbour discovery and router advertisements are
  ICMPv6, and dropping "packet too big" breaks path MTU discovery in the way that
  reads as "ping works, pages hang". The accepted types are RFC 4890's.
- **The forward chain only guards traffic headed into our networks** (`to not
  <inside> accept`). Forwarding between interfaces olr does not own — Docker,
  libvirt — is not this module's to judge, so a box running containers keeps
  working. The input chain has no such carve-out: the router itself is
  protected from every non-inside interface, which includes a container bridge
  reaching a service on the host.
- **Explicit counted drop under an `accept` policy**, not `policy drop`. The
  verdict is identical; the counters are what the status screen reports.
- **Other tables cannot loosen this.** In nftables a `drop` in any base chain is
  final and an `accept` is not, so rules added by hand, by wg-quick or by Docker
  can narrow what this table allows but never widen it.
- **Atomic.** Programmed over netlink in-process like `olr_nat` (no `nft`
  binary): delete-and-recreate in one batch, so there is never a half-built
  ruleset. Each rule carries its canonical line as its nft comment, which is
  also the diff basis for drift.

## 4. Lockout

Switching the firewall on while connected from outside — the olr box sits
behind an existing home router, and the operator is on that router's network —
blocks their next connection to the UI. The established one survives; the page
stops working a moment later and reads as the box having died.

design.md §5.5's guard, which would revert an unconfirmed change, is not built.
What stands in for it: the plan names the interface the request arrived on, and
when that interface is outside it carries a warning and `impact: disruptive`.
The UI stops and asks; the API returns 409 without `confirm=true`. The CLI over
the unix socket is never affected and never warned.

A private-source exception ("allow the UI from RFC 1918 addresses on the
uplink") was considered and left out to keep the stance simple. It is one rule
if it turns out to be wanted.

## 5. Limits, stated

- **Boot window.** The table is programmed when olrd starts, first of all the
  restores. Between the kernel bringing interfaces up and olrd starting, the
  box is unfiltered.
- **No per-device IPv6 openings.** Letting the internet reach one device's port
  over IPv6 is a filtering permission rather than a translation, and there is no
  object for it yet (docs/port-forwarding.md §7).
- **Drift detection reads comments**, with the gap `olr_nat` documents: a rule
  whose expressions were hand-edited under an intact comment is not reported. It
  is corrected on the next apply regardless.
- **Unverified on a real box** at the time of writing. The kernel half is
  typechecked for Linux and has not yet run against a live nftables.
