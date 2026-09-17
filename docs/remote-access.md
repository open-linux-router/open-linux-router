# `remote` module design

Status: **built for WireGuard, unproven on hardware.** `internal/remote` is the
module, the tunnel is kernel WireGuard driven through `wg`, and there is no
WebUI page yet. What has not happened is a phone connecting: §11.1 records
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
| **Remote access** | only the operator, with a credential | the whole network | **this module** |
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

Modelling them as a network (`design.md` §4.4's group) is what makes the rest
of olr apply to them without any of it being written twice:

- they appear in `devices` with the names their operator gave them;
- `dns` answers those names, so `phone.home.example.com` means something;
- `gateway`'s `Internet via` applies to the dial-in network exactly as it does
  to `iot` — which is how a peer's traffic can be sent out through a chosen
  exit rather than the box's normal path.

Each of those is a feature nobody has to build twice. As "remote clients" every
one of them would be a second integration.

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

**What the group buys is a name other modules can key off, and that half is not
built yet.** Batch 1 creates the interface and addresses it; registering the
segment as a group so `devices` and `dns` see it is deferred to the batch that
needs it. The deferral is honest rather than convenient: nothing consumes the
registration today, and inventing the registration path before a consumer exists
is how `dhcp` came to be keyed on kernel interface names for a year
(`design.md` §9).

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

### 6.1 `Endpoint` is the one thing olr cannot derive

It is the public name or address a client dials, and nothing on the box knows
it: the uplink address may be behind a carrier NAT, and the name that tracks it
lives at a DNS provider. So it is a required field when the module is enabled,
and the validator refuses rather than rendering a configuration that cannot
connect.

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

## 8. Failure modes

The honest ones first, because two of them are gaps in olr rather than in this
module and an operator will meet them on day one.

| | What happens | What olr says |
|---|---|---|
| **No egress NAT for `routes: everything`** | A full-tunnel peer's traffic reaches the box with a source address from the dial-in network, and olr renders no masquerade for it. On a box whose upstream does not route the dial-in subnet back, the peer has a tunnel and no internet | A warning at write time naming this exactly. `firewall` builds `olr_nat` for port forwards only (docs/firewall.md); general egress NAT is not olr's yet |
| **IP forwarding off** | A peer reaches the router and nothing behind it | Reported in `status`, with the sysctl named. Not written: `net.ipv4.ip_forward` is machine-wide shared state and `design.md` §3.4 says we do not squat it. In practice a box that already routes a LAN has it on |
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
| **v2** | Shadowsocks and SOCKS5, as two more parallel objects | §10 — the second instance is what earns any shared shape, not the first |
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

2. **The dial-in segment is not registered as a group yet** (§3.1), so a peer
   does not appear in `devices` and `dns` does not answer its name. Those are
   the three integrations §3 uses to justify the module existing at all, and
   none of them is built. This is the first thing to do after hardware proves
   the tunnel.

3. **Egress NAT belongs to somebody** (§8, row 1). `routes: everything` cannot
   work on the common topology without it, and this module must not grow its own
   masquerade — that would be a second owner of a decision `firewall` is going
   to make. The warning is a placeholder for a real answer.

4. **The CLI spells no protocol, and the stored configuration does.**
   `olr remote set --endpoint …` writes `remote.wireguard.endpoint`. The nesting
   is right where it is expensive to change and absent where it is cheap, but it
   means that the day Shadowsocks lands, `set` splits into `set wireguard` and
   `set shadowsocks`, and `enable`/`disable` grow an object. `docs/cli.md` §12
   says pre-1.0 CLI changes are cheap, which is the licence being used here
   rather than an oversight.

5. **Peer removal is `disruptive` and the CLI confirms it automatically.** Every
   `olr` command sends `confirm=true` (gateway:§8a), so `olr remote rm peer
   phone` revokes access with no second question; the WebUI will ask. That is
   the established contract and it reads more dangerous here than elsewhere,
   because the thing being taken away is somebody's way in. Confirm it is still
   the wanted shape before a UI is built on it.
