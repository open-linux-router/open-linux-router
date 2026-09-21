# `remote` module design

Status: **two of the three objects built, unproven on hardware.**
`internal/remote` holds WireGuard — kernel tunnel, driven through `wg` — and
Shadowsocks — a supervised `ssserver` with a rendered configuration. SOCKS5 is
not built. There is a WebUI page for the tunnel and none for the proxy. What has not happened is a phone connecting: §11.1 records
exactly how far this has been driven and where that stops. Bare section
references are to this document; references to `design.md` name it, and
`gateway:` / `ingress:` name those.

olr has had the outbound half of the router for a while — `gateway` decides
where traffic leaving a network goes. The inbound half was two thirds built:
`firewall` forwards a port, `ingress` publishes a service over HTTPS, `dial`
keeps a public name pointing here. **What was missing is the operator
themselves getting back in.** Not one service — the network. From a phone, on
somebody else's Wi-Fi.

Five things were decided before this was written and most of the document is
their consequences: the scope is exactly **two intents** and this is one of
them; remote access ships as **three parallel objects** rather than one
abstraction; the dial-in segment **is a network**; olr **creates the interface
itself and never uses `wg-quick`**; and the tunnel has **no daemon and no unit**,
which makes this module shaped like `gateway` rather than like `ingress`.

---

## 1. The two intents, and why only one of them is new

An operator wants one of two things from outside their house, and they are not
variations of each other:

| Intent | Who gets in | What they reach | Status |
|---|---|---|---|
| **Remote access** | only the operator, with a credential | the whole network, or just the way out | **this module** |
| **Public publishing** | anybody | one service | `ingress` + `firewall`, built |

These take opposite values on both dimensions, so the common part is empty:
no shared field, no shared mechanism, no shared object. That is the inverse of
why `gateway`'s exit legitimately unifies three `Via` forms — those differ in
*how*, and agree on *what*. Unifying these two would produce a form whose every
field is greyed out for one of its two modes.

Two further intents were considered and **cut**: private publishing (one
service, credentialed) and site-to-site (two networks joined permanently).
Private publishing is remote access with extra steps once you are already
inside; site-to-site is a different object shape with a different lifecycle and
a different failure story, and nothing about this module forecloses it.

---

## 1a. Two objects, and the difference an operator has to understand first

Remote access ships as parallel objects rather than one thing with a backend
setting, and the reason is not implementation tidiness — it is that they do
different jobs, and an operator choosing between them is choosing between those
jobs:

| | **Tunnel** (WireGuard) | **Proxy** (Shadowsocks) |
|---|---|---|
| The device becomes | a device on a network at home | a device somewhere else, borrowing this box's way out |
| It can reach | the NAS, the printer, this router's UI, the internet | exactly what the internet can reach |
| Identity | one key pair per device | **one password for every device** |
| Revoking one device | remove its key; nobody else notices | impossible — change the password for everybody |
| A client configuration | issued once per device, never re-showable | one link, reproducible whenever asked |
| Underneath | kernel state, no daemon | a rendered file and a systemd unit |

They are not substitutes and an operator may well want both: one to get *in*,
one to borrow a route out from a network that is filtering or watching. What
they have in common is a single field — the address clients dial — and that is
the only thing the module level owns.

**The parallelism goes all the way down.** They do not share a plan, an apply,
an HTTP surface or a page section. Folding them together would produce a plan
type whose `changes` field is a list of files half the time and a list of kernel
lines the other half, to describe two mechanisms that never interact. §7.5 is
what that decision cost and bought.

---

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
| `gateway`'s `Internet via` | **nothing — it already works** | assignments are keyed by kernel interface (`internal/gateway/config.go`), so `olr gateway set via wg0 <exit>` sends a peer's traffic out a chosen exit today. gateway:§2.5 will re-key these to network names eventually; until it does, the absence of a group is not felt |
| `dns` answering `phone.home.example.com` | **one command, the same one every other device costs** | `olr dns add host phone --address 10.6.0.2` |
| a peer appearing in `devices` | **blocked, and not on this module** | §3.3 |

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

**And the group is not registered with `link` at all, which is a reversal of
what this section first said.** The plan was to register it once a consumer
existed. Going to find that consumer is what produced the table above: `gateway`
does not need one, `dns` must not have one, and `devices` cannot use one. What
is left is a registration with no reader — and one active cost, because `dhcp`
keys off `link`'s groups, so a registered dial-in network would show up on the
DHCP page offering to serve addresses on a network whose addresses are not
served at all (§3.2).

So `remote` owns the interface, its address and the segment, and `link` is not
told. The day something needs to name this network, the thing to weigh is
whether `link` grows a notion of a group it does not address — not whether to
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
`firewall` are already in.

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
olrd startup, exactly as `gateway` and `firewall` are, and for exactly the same
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

## 7.5 The proxy: Shadowsocks

The second object, and the one that reaches nothing. A client gets this box's
way out and no sight of the network at all — which is the whole point when the
problem is a hostile network rather than a NAS you cannot reach.

### 7.5.1 One password, and therefore no device list

There is one secret for every client, because that is what Shadowsocks is.
Three things follow and all of them are visible to an operator:

- **There is no client list**, and there must not be one. A list the server
  cannot tell apart would look exactly like the tunnel's device list and behave
  nothing like it: removing an entry would revoke nobody. That is precisely the
  "looks unified, behaves differently" trap, and the honest answer is to have no
  list rather than a decorative one.
- **The link is reproducible.** `olr remote show link` prints it whenever it is
  asked, because olr stores the password. The tunnel's equivalent cannot exist —
  a peer's private key is generated, returned once and forgotten — and the
  asymmetry is not an inconsistency, it is the same fact seen twice.
- **Revoking is changing the password for everybody.** olr classifies that as
  disruptive and says who it affects, which is everyone.

Per-device keys *are* possible — SIP022 has a multi-user extension — and were
considered and deferred (§10). They would buy the tunnel's revocation story at
the cost of client support olr cannot verify, and the failure that produces is
a device that will not connect for a reason nothing on screen explains.

### 7.5.2 The cipher decides what a password *is*, and that is a trap

This is the `wg-quick` of this object: a sharp edge that olr must absorb once so
nobody else meets it.

For a `2022-blake3-…` method the password field is **not a password**. It is a
base64-encoded pre-shared key of a length the cipher fixes — 16 bytes for
`aes-128-gcm`, 32 for the other two — and a server handed anything else refuses
to start with a complaint about base64 that never mentions the cipher. The older
`aes-256-gcm` and `chacha20-ietf-poly1305` take an ordinary passphrase.

So an operator who picks a stronger-sounding cipher and keeps their password
gets a proxy that will not run, for a reason nothing connects to what they
changed. olr's answer:

- **the password is generated, never typed** — there is nothing a human could
  choose that would be better than random bytes of the right length;
- **changing the cipher regenerates it**, because keeping it would be keeping a
  value the server will reject; and
- **that change is classified disruptive**, because every link already handed
  out stops working. Regenerating quietly would be the worst of the three
  options.

### 7.5.3 UDP is on, against upstream's default

`ssserver` defaults to `tcp_only`. The symptom of that is not "UDP does not
work" — it is *"the web works and some apps mysteriously do not"*, because name
resolution and anything over QUIC fall back slowly or not at all. olr renders
`tcp_and_udp` so nobody has to learn it, and keeps a field so an operator who
wants the narrower thing can say so (design.md §5.6 — declared, never inferred).

### 7.5.4 The backend, and the part that is the operator's

`ssserver` from shadowsocks-rust, supervised as `olr-shadowsocks.service`,
reading a configuration olr renders at mode `0600` — internal/ingress's shape,
because this one is a daemon.

**No distribution packages it.** Debian carries shadowsocks-libev, which
upstream has declared bug-fix-only; the Rust implementation is the shadowsocks
project's own and its stated direction, and it ships as a release binary and
nothing else. So unlike `wireguard-tools` there is no `core.Dependency` here —
one would produce "install shadowsocks-rust with your package manager", which
names a package that exists nowhere — and the blocker carries a download and an
`install` line instead.

That has a cost worth stating rather than burying: **security updates for this
one are the operator's.** A router nobody touches for two years is exactly where
that matters. It was raised as a reason to prefer a backend with an apt channel
and the answer was that a sustained upgrade channel is not the deciding
constraint here; recorded so the trade is visible rather than rediscovered.

### 7.5.5 What it does *not* need

The thing the tunnel's full-tunnel mode cannot do without (§8, row 1) — egress
address translation — this object does not need at all. The proxy terminates a
client's connection on this box and opens its own, so traffic leaves with the
router's own source address and there is nothing to masquerade. On the common
deployment, where olr sits behind something else that does the NAT, **the proxy
works today and `routes: everything` does not.**

---

## 8. Failure modes

The honest ones first, because two of them are gaps in olr rather than in this
module and an operator will meet them on day one.

| | What happens | What olr says |
|---|---|---|
| **No egress NAT for `routes: everything`** | A full-tunnel peer's traffic reaches the box with a source address from the dial-in network, and olr renders no masquerade for it. On a box whose upstream does not route the dial-in subnet back, the peer has a tunnel and no internet | A warning at write time naming this exactly. `firewall` builds `olr_nat` for port forwards only (docs/firewall.md); general egress NAT is not olr's yet |
| **IP forwarding off** | A peer reaches the router and nothing behind it | Written, by `gateway`, whenever that module is enabled (gateway:§3.8) — it is the one machine-wide sysctl olr sets, and the box forwards for a peer for the same reason it forwards for a LAN. It is read back on every plan, so a third party resetting it shows up as drift. A box with `gateway` switched off is still the operator's to turn on |
| **LAN devices' default route is not this box** | The peer's packets arrive at the NAS and the reply goes somewhere else. The tunnel is up, `wg show` looks perfect, and one direction is missing | Not detectable from here; §9 lists a route check as v2 |
| The UDP port is not reachable from outside | Handshakes never arrive. `wg show` shows a peer that has never handshaked | `status` reports the last handshake per peer, which is the only honest signal — "never" is different from "a while ago" |
| Endpoint name stops resolving to this box | Existing sessions survive; new ones cannot start | Not this module's to detect; it is `dial`'s, and that is where it is visible |
| Two devices sharing one configuration | Intermittent, looks like a flaky network | Cannot be detected — one key, one endpoint at a time. §2.1 |
| `wireguard-tools` missing | Nothing can be applied | A blocker on this module's page with the install command for the distribution actually running (`core.Dependency`) |
| The kernel has no WireGuard | Creating the interface fails | The netlink error, with the module name the box is missing |

The first two share a shape worth naming: **the tunnel comes up and works, and
the thing behind it does not.** That is the worst debugging experience this
module can produce, which is why both are reported rather than left to be
discovered — even though neither is something olr will fix on the operator's
behalf.

---

## 9. Scope

| | | |
|---|---|---|
| **v1** | WireGuard: a peer is a name, keys generated here, one client configuration returned once | §2, §4 |
| | the dial-in network's subnet and the box's address on it | §3 |
| | `routes: home \| everything`, home prefixes read from `link` | §6.2 |
| | `DNS` and `PersistentKeepalive` rendered, never asked | §6.3, §6.4 |
| | last handshake and transfer per peer in `status` | §8 — the only honest liveness signal |
| | applied at olrd startup, like `gateway` and `firewall` | §7.1 |
| | `raw_wireguard_conf` escape hatch | §7.3 |
| **v1** | Shadowsocks: one port, one cipher, one generated password, one reproducible link | §7.5 |
| | UDP carried by default, against upstream's | §7.5.3 |
| **v2** | SOCKS5, as the third parallel object | the second instance settled the shape; the third should need no new argument |
| | per-device keys for the proxy, through SIP022's multi-user extension | §7.5.1 — buys real revocation, costs client support that cannot be verified from here |
| | a WebUI section for the proxy | the page covers the tunnel only |
| | registering the dial-in segment as a group, so `devices` and `dns` see peers | §3.1 |
| | a QR code for the client configuration | the phone case, and the reason `add peer` returns the file rather than a path |
| | pre-shared keys per peer | escape hatch until then |
| | a reachability check on the home prefixes | §8 row 3 |
| **Never** | a peer that is not a device — user accounts, roles, per-peer firewall policy | that is `firewall`'s object and a different conversation |
| | storing a peer's private key so the configuration can be shown twice | §4.1 |
| | `wg-quick`, in any form | §5.1 |
| | site-to-site | §1 |

---

## 10. Considered and rejected

- **One "remote access" object with a backend choice.** The three protocols
  share no field: WireGuard has peers and public keys, Shadowsocks has a port
  and a cipher, SOCKS5 has a listen scope and credentials. An abstraction over
  an empty intersection is a form where every field is conditional. Three
  objects, one page.
- **`wg-quick`.** §5.1. It would install the routing that makes `olr gateway`
  refuse to apply — olr breaking olr, with an error message blaming a third
  party.
- **`wgctrl` instead of the `wg` binary.** Cleaner and deletes the one exec, at
  the cost of a seventh direct dependency. Deferred rather than rejected: the
  seam is in place and the swap is one file (§5.2).
- **Letting `link` own the interface and its address.** It owns every other
  interface's address, so this looked like consistency. It splits one act —
  allocating the peer's address and the interface's address happen together, at
  generation time — across two modules, and requires `link` to create a device
  it knows nothing about. §3.1.
- **Adopting `wg0`.** Adoption is consent to hand over the operator's hardware.
  There is nobody to ask about an interface olr made.
- **Naming the module `vpn`**, which is the name `design.md` §4 reserved. Two of
  the three objects this module will hold are not VPNs — a SOCKS proxy borrows
  an exit and gives no access to the network at all — so the reserved name
  describes a third of the module and mis-describes the rest. `remote` is what
  the operator is doing.
- **A systemd unit for the tunnel.** §7.1. There is no process.
- **A persistent rendered configuration file.** §5.2. It would be a second copy
  of the box's private key at rest, read by nothing.
- **Asking the operator for the peer's `AllowedIPs`.** §6.2. The word means two
  different things on the two sides of a tunnel, and only one of them is a
  question.

---

## 11. Open

1. **Nothing has dialled in.** Verified so far: the module builds, the pure
   halves — key generation, validation, rendering, planning — are unit-tested
   off Linux, and `kernel_other.go` keeps them testable there. What no one has
   done is create `wg0` on a real box, import the printed configuration on a
   phone, and reach a device. Until that happens every claim in §6 is a claim
   about a file, not about a tunnel.

   Three specific things to watch on first contact, in the order they would
   bite:

   - **`wg setconf wg0 /dev/stdin` has not been run** (§5.2). It is the shape
     `wg-quick` uses and `wg` opens the path with `fopen`, so a pipe is a
     legitimate thing to find there — but this is the one line in the module
     whose failure would be total, and it has not been exercised.
   - **The rendered configuration carries `#` comments** — the peer's name
     above each `[Peer]` block. wireguard-tools strips them, and a real `wg`
     has not yet confirmed it.
   - **After `wg0` is up, `olr gateway plan` must still be empty.** This is the
     one §5.1 predicts. If it refuses with gateway:§6's foreign-rule message,
     something is installing routing that olr did not — and the whole argument
     for creating the interface ourselves is wrong.

2. **A peer is not in `devices`, and the question is `devices`', not this
   module's** (§3.3). The list is keyed by MAC and a peer has none. Re-keying it
   — on "a thing on a network", with a MAC as one kind of identity rather than
   the identity — is the change that would let a peer, a container and a
   statically-routed subnet all appear where an operator looks for them. It is
   a foundation-module decision and wants its own argument; the open question is
   whether it is worth making, not how to work around it here.

   What was *closed* by going and looking: the group registration this section
   used to ask for is not wanted (§3.1), and `dns` naming is one command rather
   than a mechanism (§3).

3. **Egress NAT belongs to somebody** (§8, row 1). `routes: everything` cannot
   work on the common topology without it, and this module must not grow its own
   masquerade — that would be a second owner of a decision `firewall` is going
   to make. The warning is a placeholder for a real answer.

4. ~~**The CLI spells no protocol.**~~ **Closed by the second object arriving.**
   `set` split into `set wireguard` and `set shadowsocks`, `enable`/`disable`
   grew a required object, and the protocol went in the *object* position rather
   than a fifth one — so docs/cli.md's four positions are intact. The endpoint
   stayed unqualified because it belongs to the box (§6.1), and `add peer` /
   `rm peer` stayed unqualified because only the tunnel has devices.

5. **Peer removal is `disruptive` and the CLI confirms it automatically.** Every
   `olr` command sends `confirm=true` (gateway:§8a), so `olr remote rm peer
   phone` revokes access with no second question; the WebUI will ask. That is
   the established contract and it reads more dangerous here than elsewhere,
   because the thing being taken away is somebody's way in. Confirm it is still
   the wanted shape before a UI is built on it.
