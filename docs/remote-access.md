# Remote access

OLR manages WireGuard device access and a separate SOCKS5 server. The shared
endpoint is the public hostname or address clients use. Each object has its own
configuration, status, plan and apply routes. WireGuard programs kernel state;
SOCKS5 renders a credential-bearing 3proxy configuration and supervises its unit.

## 2. The object: a peer

A peer is **a device that may dial in**. One is a phone, one is a laptop, one is
a work machine the operator wants to keep separate.

The operator gives two things:

| Field | Example | |
|---|---|---|
| `name` | `phone` | what this device is called, here and later in `devices` |
| `routes` | `home` | what the device sends through the tunnel |

Everything else is derived and never asked:

- the key pair — generated on the box, §4
- the address on the dial-in network — allocated, §3
- `AllowedIPs` on the server side — the peer's own address and nothing else
- the whole client configuration — rendered and handed back once, §6

So adding a peer is **one name**, and that is the claim this module makes.
`olr remote add peer phone` prints a configuration file that works.

### 2.1 What a peer is not

It is not an account, and there is no password. WireGuard authenticates by key
pair: a peer *is* a public key, and the name is olr's label for it. Two
consequences worth stating because they surprise people who expect a VPN to
have logins:

- **Revoking access is removing the peer.** There is nothing to disable; the
  key stops being accepted the moment the peer is gone, and any session it had
  dies with it.
- **A peer is a device, not a person.** An operator with a phone and a laptop
  has two peers. Sharing one client configuration between two devices appears
  to work and then breaks intermittently, because both ends of a WireGuard
  session assume one endpoint per key — so the validator cannot catch it and
  the documentation has to say it instead.

---

## 3. The dial-in segment is a network

The phone that dials in is **a device on a network at home**. That is a design
decision rather than a description, and the alternative was live: the dial-in
clients could have been "remote clients", a list belonging to this module and
visible nowhere else.

Modelling them as a network is what lets the rest of olr apply to them without
being written twice. **This section used to list three integrations that fell
out of it for free. Then somebody went and read the three modules, and only one
of them is free** — the corrected version is below, because a justification that
overstates its own payoff is worse than a smaller true one.

| | What it actually costs | |
|---|---|---|
| `gateway`'s `Internet via` | **nothing — it already works** | assignments are keyed by kernel interface (`internal/gateway/config.go`), so `olr gateway set via wg0 <exit>` sends a peer's traffic out a chosen exit today. gateway:§2.5 will re-key these to network names eventually; until it does, the absence of a network is not felt |
| `dns` answering `phone.home.example.com` | **one command, the same one every other device costs** | `olr dns add host phone --address 10.6.0.2` |
| a peer appearing in `devices` | **blocked, and not on this module** | §3.3 |
| a peer appearing on the overview's network map | **its own box above the router, keyed by name** | §3.4 |

The second row is the one that looked like a gap and is not. A local name in
`dns` is *stored intent*, never derived from another module's addresses:
`internal/dns/dhcp.go` argues the case at length for DHCP reservations, and
every word of it applies here. A name that came from a peer would be correct
exactly once; rendered bytes are what drift is measured against
(`design.md` §5.4), so a peer edit would leave `dns` drifted until somebody
applied it — and re-applying `dns` from inside a `remote` change would break the
promise underneath every mutating route in olr, that one request is planned and
the plan decides whether it lands.

So a dial-in device is named exactly the way a device on the LAN is named, and
what this module owes is not a mechanism but a **pointer**: `olr remote add
peer` prints the `olr dns add host` line with the address already filled in.
That is `design.md` §5.6's rule — automatic behaviour is declared, never
inferred — applied to the smallest possible case.

### 3.3 A peer is not a device, and `devices` is why

`devices` is keyed by MAC. Not incidentally — `design.md` §4.4 says so in as
many words, `Merge` in `internal/devices/presence.go` drops any sighting whose
MAC will not parse, and the vendor lookup, the icon detection and the fixed
address all hang off it.

**A WireGuard peer has no MAC.** It is a public key at the end of a tunnel;
there is no layer 2. So putting one in the device list needs one of three
things, and two of them are worse than the gap:

- **Synthesise a MAC per peer.** A fabricated hardware address in the
  operator's own device list, with a vendor lookup that would confidently
  report something. This is the shape of lie the whole `Origin` field in
  `internal/devices/list.go` exists to prevent.
- **Re-key `devices` on something wider than a MAC.** Defensible, possibly
  right eventually, and a change to a foundation module that four others
  reference. Not something a new module gets to force on its way in.
- **Leave it.** A peer is listed by `olr remote show peers`, with its address,
  its last handshake and where it was last seen from — which is more than the
  device list would show, and in the one vocabulary that is true of it.

The third is what is built, and §11 #2 keeps the question open rather than
closed.

### 3.1 Registered, not managed — who writes the address

This is the first time in olr that **two modules touch one interface**, and it
needed resolving rather than assuming.

`link` owns networks, and applying one *writes the router's address to the
kernel* (`internal/link/apply.go`, `DesiredFor`). But `wg0` does not exist until
this module creates it, and a WireGuard address is not like an Ethernet address
in the way that matters:

> **Assigning the interface's address and assigning a peer's address are one
> act.** Both are fixed at the moment a client configuration is generated, and
> both are written into a file the operator copies to a phone. A design where
> `link` owns one and this module owns the other splits a single decision across
> two owners for no gain.

So: **this module owns the interface and its address.** `link` is not asked to
create it, is not asked to address it, and — this is the part that must not be
skipped — `wg0` is **never added to `link.Adopted`**. Adoption is the operator
consenting to hand over *their* NIC (`design.md` §7); `wg0` is olr's own, and
there is nothing for anyone to consent to.

**And the network is not registered with `link` at all, which is a reversal of
what this section first said.** The plan was to register it once a consumer
existed. Going to find that consumer is what produced the table above: `gateway`
does not need one, `dns` must not have one, and `devices` cannot use one. What
is left is a registration with no reader — and one active cost, because `dhcp`
keys off `link`'s networks, so a registered dial-in network would show up on the
DHCP page offering to serve addresses on a network whose addresses are not
served at all (§3.2).

So `remote` owns the interface, its address and the segment, and `link` is not
told. The day something needs to name this network, the thing to weigh is
whether `link` grows a notion of a network it does not address — not whether to
paper over it here.

### 3.2 The asymmetry that has to be said out loud

A dial-in network looks like every other network and behaves unlike one in a
single respect, and an operator will meet it:

> **These addresses are not served. They are baked into each client
> configuration when it is generated.**

There is no DHCP here and there never will be — the address is inside the
tunnel, and the tunnel does not exist until the client already knows its own
address. So an operator who goes to the DHCP page looking for the dial-in
network will not find it, and an operator who changes the subnet has invalidated
every client configuration they have already handed out.

That second consequence is why changing the subnet is classified `disruptive`
(§7.4) even though nothing is disconnected at the instant it is applied. The
damage is to files that already left the box.

---

### 3.4 On the map, as a box of their own

§3.3 leaves a peer nowhere in `devices`, and a network map with nothing on it
for a phone that is connected right now is a map that is wrong about the
router's own state. So the overview draws each peer itself, and this section is
the two decisions that took.

**Above the router, beside the ways out.** Everything under the router hangs
off something — a group by its column, a device by its row — and a card of its
own down there would need every group's line to run through it or squeeze past
it. Above the router there is exactly one box per line, each joining the router
directly, which is what a peer is: a device on the far side of this box. The
band therefore holds both kinds of thing that are outside it — where traffic
leaves (observed next hops) and where it arrives (peers) — and the two are told
apart by their cards, not by their position. Gateway policy exits stay off the
map entirely: an exit this box chooses among is not a thing outside it.

**Four states, not two.** `connected` (a handshake inside
`internal/remote/view.go`'s three-minute window), `away` (last seen at a time),
`never` (no handshake ever — almost always a configuration that was not
imported, or a port that does not reach the box), and `unknown` (the kernel
could not be read, which is an absence of an answer and never a "no"). They are
the same four `PeerBadge` draws on the tunnel's own page, for the same reason:
"never connected" is the one state the operator can still fix, and folding it
into "offline" hides the only failure this module reliably produces.

A peer is not a fault. The one colour the map spends is red, on a way out that
has stopped answering; a sleeping laptop is not that, so its card is drawn in
the same neutral as everything else and says when it last called.

`RemoteStatus.peers` is the only input. A box with the tunnel switched off gets
no box, no empty container and no "WireGuard is off" note — the map is about
what is on the network, and `remote`'s page is where a module that is off is
described.

---

## 4. Keys

A WireGuard key pair is 32 bytes of X25519. olr generates both halves itself,
with `crypto/ecdh` from the standard library — not by running `wg genkey`, and
not with a new dependency.

That is worth one sentence of justification because shelling out would have been
the obvious thing: key generation is the one operation in this module that has
to work on a machine with no `wg` and no Linux, because that is where the tests
run. Doing it in the standard library makes the whole of key handling pure,
testable anywhere, and independent of whether the backend is installed.

The private key is clamped exactly as `wg genkey` clamps it, so a key generated
here and a key generated there are indistinguishable.

### 4.1 Where the keys live, and what never leaves

Two key pairs exist per peer relationship and they are treated differently:

| Key | Stored | Leaves the box |
|---|---|---|
| the box's private key | in the configuration document, redacted everywhere | never |
| the box's public key | derived on demand | in every client configuration |
| a peer's public key | in the configuration document | no |
| a peer's private key | **nowhere** | once, in the client configuration |

The last row is the one that carries a cost, and it is taken deliberately. olr
generates the peer's key pair so that adding a peer is one command, hands the
complete client configuration back in the response to that command, and keeps
only the public half. There is no route that returns a client configuration a
second time, because there is nothing left to build one from.

> **Losing a client configuration means removing the peer and adding it again.**

Storing the peer's private key would make the config re-showable and is what
most WireGuard front-ends do. It is refused here for the reason
`ingress:`§4.1 gives about the DNS provider token, one step further along: a
credential that is never stored cannot be disclosed by a backup, a screenshot,
or a `GET` somebody forgot to redact. The convenience lost is one re-issue on a
lost phone, which is a thing that should arguably be a re-issue anyway.

An operator who would rather generate the key on the device itself can:
`olr remote add peer laptop --public-key <key>` stores the public half and
**declines to print a client configuration at all**, because it cannot write one
without a private key it does not have. The tunnel settings are printed instead,
for the operator to paste into a configuration they complete themselves.

---

## 5. The backend: `wg`, and never `wg-quick`

olr ships no WireGuard. The data path is in the kernel; what has to be installed
is `wireguard-tools`, which is in every distribution's repository and is
declared the way every other backend is (`core.Dependency`, `Inert: true` — the
package installs two binaries and starts nothing).

### 5.1 `wg-quick` would trip olr's own refusal

`wg-quick` is the obvious way to bring a WireGuard interface up and it is the
wrong one here, for a reason found by reading olr's own code rather than by
taste.

`wg-quick up` with `AllowedIPs = 0.0.0.0/0` installs its own policy routing: an
`ip rule` at priority 32765 selecting table 51820, which carries a default
route. `gateway`'s `observeRules` (`internal/gateway/kernel_linux.go`) flags any
foreign `ip rule` selecting a table that carries a default route, because that
is the definition of a second owner of "where does traffic go" — and gateway:§6
then **refuses the whole apply**.

So using `wg-quick` inside olr would mean olr breaking olr: the tunnel comes up
and `olr gateway` stops working, with a refusal message about somebody else's
routing daemon that is in fact us.

The collision is not a reason to avoid WireGuard. It is a reason to avoid
`wg-quick`, whose entire job is the part olr already does:

| `wg-quick` does | olr does |
|---|---|
| create the interface | netlink, `internal/remote/kernel_linux.go` |
| set the address | netlink, same file |
| bring the link up | netlink, same file |
| load the peer configuration | `wg setconf` |
| **install `ip rule`s and routes** | **nothing — that is `gateway`'s** |

The last row is the whole point. A tunnel olr created installs no competing
rule, so `Internet via` keeps working, and the one useful thing `wg-quick` would
have brought is the one thing that must not happen.

### 5.2 What olr executes, and the rule it bends

`wg setconf <interface> <file>` is the only subprocess this module runs. That
needs stating against `design.md` §3.6, which says olrd executes no
subprocesses, and which already carries two exceptions (`caddy validate`, and
the package manager as a transient unit).

This is a third, and it is the mild kind: `wg` reads one file we just wrote and
writes kernel state through its own netlink socket. It touches no filesystem
path, needs nothing the unit does not already have — every olr unit runs as root
with no sandbox (`design.md` §3.5) — and is bounded.

The alternative, `wgctrl`, would configure the peers over netlink from inside
olrd and delete the exec. It was not taken for batch 1 on dependency budget:
`go.mod` has six direct dependencies and that restraint has already decided
several arguments in this codebase. Everything `wg` is used for sits behind
`Kernel` (`kernel.go`), which is the seam `design.md` §10 requires anyway, so
swapping it later is a change to one file.

**There is no configuration file.** The rendered configuration is handed to
`wg setconf` on **standard input** — `wg setconf wg0 /dev/stdin` — so the box's
private key never reaches a filesystem at all. This is the same mechanism
`wg-quick` uses (it passes a process substitution), and it is worth the small
unfamiliarity: the document already holds the one copy of the key that
`design.md` §3.4 says the operator's data lives in, and a second copy at rest,
even for the milliseconds between writing a temporary file and deleting it,
would be a second thing to protect with no reader.

So this module writes no file, which is a departure from `design.md` §7's
"generated files live under `rendered/`". The generated artefact here is kernel
state, not something a daemon re-reads — exactly the situation `gateway` and
`gateway` is already in.

---

## 6. What a client configuration says

The file handed back when a peer is created is the product of this module as
much as the tunnel is. Every line of it is derived:

```ini
[Interface]
PrivateKey = <generated, shown once>
Address    = 10.6.0.2/32
DNS        = 10.6.0.1

[Peer]
PublicKey           = <the box's>
Endpoint            = home.example.net:51820
AllowedIPs          = 10.6.0.0/24, 192.168.1.0/24
PersistentKeepalive = 25
```

### 6.1 `Endpoint` is the one thing olr cannot derive, and it belongs to the box

It is the public name or address a client dials, and nothing here knows it: the
uplink address may be behind a carrier NAT, and the name that tracks it lives at
a DNS provider. So it is required as soon as anything is switched on, and the
validator refuses rather than rendering a configuration that cannot connect.

**It sits at the module level rather than inside either object**, because both
need exactly this value and a field typed twice is a field that can disagree
with itself — design.md §4.1's rule occurring inside one module rather than
across two. An operator who moves house changes it once.

The consequence is that it carries **no port**: each object listens on its own,
so a port here could only ever be right for one of them. An object whose public
port differs from the one it listens on says so with its own `public_port`, and
the validator refuses a port on the shared field rather than guessing which
object it was meant for.

An operator who is already using `olr dial` has the answer — it is the name they
keep current there — and the refusal says so. Reading it from `dial`
automatically was considered and declined: `dial` holds a *list* of records and
nothing says which one is this box's front door, so olr would be guessing at a
value whose wrongness presents as "the VPN just doesn't connect".

### 6.2 `AllowedIPs` means two different things and we name only one

On the **server** side, a peer's `AllowedIPs` is a filter: which source
addresses that key may use. olr derives it — the peer's own address, a `/32`,
and nothing else — and does not offer it as a field. Anything wider is a
cryptokey-routing decision with no operator-facing question behind it.

On the **client** side, the same word means the opposite thing: what the device
sends *into* the tunnel. That is a real choice with a real trade, so it is a
field, and it is spelled in the operator's words rather than WireGuard's:

| `routes` | The device sends home | |
|---|---|---|
| `home` (default) | the home networks and the dial-in network | the NAS, the printer, the router's own UI; everything else goes out the coffee shop's Wi-Fi as normal |
| `everything` | all of it | the device appears to be at home for every purpose, including its public IP address |

`home` is the default for a reason the client-side/router-side symmetry usually
hides: the client is a phone *out in the world*, and full-tunnel drags its video
calls across the operator's home upload. The "configure it once on the router"
instinct that is right for devices at home is wrong here, because olr is not in
the path of anything the phone does until the phone decides it should be.

**Which is also why changing it later is not something olr can do.** The value
lives in the file on the device and nowhere else — the tunnel does not know it
and cannot be told — so `olr remote set` on an existing peer stores the new
intent, produces no kernel change whatsoever, and answers with the one line the
operator has to edit on the device. Saying nothing there would be the worst of
the three options: the operator would believe it had been done.

The prefixes behind `home` are read from `link`'s networks per request, never
copied (`design.md` §4.1). A box with no networks configured yet gets a warning
saying the configuration will reach the router and nothing behind it.

### 6.3 `DNS` points at the router

So that a dial-in device resolves local names the way a device at home does:
`nas.home.example.com` has to answer, or "reach the whole network" is only true
for people who memorised addresses. The address is the box's own on the dial-in
network, which is inside `AllowedIPs` in both modes, so it works for split
tunnels too.

`dns` answers on that address by construction rather than by an operator
remembering to pin it: it subscribes to the tunnel's address (design.md §4.1's
`remote → dns` arrow) and binds it leniently, so a tunnel that comes up after the
relay does is still answered on. A tunnel that is on while `dns` is off is the
one combination olr cannot make work; `olr dns status` reports the served set so
"not configured" and "not served" stay distinguishable.

### 6.4 `PersistentKeepalive = 25`

Rendered always. Without it a peer behind NAT is reachable only while it is
sending, so the operator's phone is contactable from home for a minute after it
last spoke and then silently is not. Twenty-five seconds is upstream's
recommendation and is below every NAT timeout worth worrying about.

---

## 7. Mechanism

Intent in the configuration document under `remote` (`design.md` §3.2 rule 1).
No rendered file that survives an apply, no backend to supervise — what this
module configures is the kernel, which makes it `gateway`-shaped and not
`ingress`-shaped.

### 7.1 There is no unit

This is the surprising half and it is worth being explicit, because every other
module with a backend has one. WireGuard's data path is in the kernel. There is
no daemon to start, nothing for systemd to supervise, and `wg-quick@wg0.service`
is the unit this module exists not to use (§5.1).

`design.md` §3.5's test decides it: *does it have to keep running while olrd is
stopped?* The tunnel does, and it does — kernel state outlives olrd without
anyone's help. So there is nothing to put in a unit.

What kernel state does not outlive is a **reboot**. So this module is applied at
olrd startup, exactly as `gateway` is, and for exactly the same
reason: the configuration would otherwise survive perfectly and be in force
nowhere.

### 7.2 Apply order

Bottom-up, and each intermediate state has to be one where a peer either
connects correctly or does not connect at all — never one where a handshake
succeeds and the traffic goes nowhere:

```
create the interface  →  address it  →  wg setconf  →  bring it up
```

Up is last on purpose. An interface that is up with no peers configured is a
socket accepting handshakes it will reject; the window is milliseconds and it
produces a client that reports a failure at the exact moment olr is making
things work, which is the sort of thing that gets reported as a bug in the
wrong place.

Tearing down is the reverse and ends by deleting the interface, because a
WireGuard interface with no configuration is indistinguishable, to anybody
reading `ip link`, from one that is broken.

### 7.3 The escape hatch

`remote.wireguard.raw_wireguard_conf`, per `design.md` §3.2 rule 5: appended
verbatim to what `wg setconf` is given. WireGuard's own configuration format has
few knobs olr does not model — a pre-shared key, an `FwMark` — and this is the
whole of the answer to all of them.

It is additive and cannot restate what olr renders, checked the same way
`ingress` checks its Caddyfile hatch: textually, erring toward allowing, with
the authoritative check being `wg setconf` refusing the file before anything
else happens.

### 7.4 Impact, in the operator's terms

| Impact | What it means here |
|---|---|
| `none` | nothing the kernel reads changes |
| `reload` | a peer is added, or its client-side routes change. No live tunnel is disturbed — `wg setconf` keeps the session state of every peer whose key is unchanged |
| `restart` | the listen port or the box's address moves. Live tunnels drop and re-establish by themselves, because a WireGuard client retries forever |
| `disruptive` | something stops working and does not come back on its own |

`disruptive` is reached by four things and each of them is a fact rather than a
guess:

- **removing a peer that has connected** — compared against the keys the kernel
  is actually holding, not against the stored list, so removing a peer nobody
  ever used is not dressed up as an outage;
- **changing the subnet or the box's key** — every client configuration already
  handed out becomes wrong, and no retry fixes a file on somebody's phone;
- **disabling the module** — the way in goes away;
- **changing the listen port while the endpoint is a bare address** — the same
  problem as the subnet: the file says the old port.

---

---


## 8. SOCKS5

`/api/remote/socks5` exposes config, status, plan, apply and credential-link
routes. The link is deliberately absent from MCP tools because it contains the
password. Config and plan responses redact secrets.

The default listener scope is `internet` (all interfaces), with authentication.
`tunnel` restricts the listener to the WireGuard address. SOCKS5 carries no
transport encryption; the UI reports the exposure warning from the backend.
Moving the listener or replacing credentials can invalidate existing links and
uses the normal disruptive-change confirmation gate.

## 9. Removed protocol migration

Shadowsocks is no longer supported. Its API, CLI, renderer, installer guidance,
service unit and launcher entry have been removed. Old stored sections are
ignored on read and dropped when remote configuration is next saved. They cannot
enable a listener or open a firewall port. Package upgrades stop and mask only
`olr-shadowsocks.service`; independently installed services are outside OLR ownership.
Old rendered credential files are not deleted automatically. Standalone-binary
upgrades must stop and disable the old OLR service before replacing the binary.
