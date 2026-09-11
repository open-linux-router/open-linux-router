# `ddns` design — publishing a changing address

Status: **not built.** This is the selection and the shape, written before the
code so the expensive decisions are arguable while they are still cheap. Bare
section references are to this document; references to `design.md` name it, and
`ingress:` means `docs/ingress.md`.

Three things were decided before this was written and the rest is mostly their
consequences: the feature belongs to **`dial`**, the provider implementations
are **vendored from `ddns-go`** rather than imported, and the address we publish
is **discovered, not assumed**.

The last one is the whole difficulty. Everything else here is a periodic HTTP
request.

---

## 1. The object: a name that follows an address

> **A dynamic DNS record is a public name whose value this box is responsible
> for keeping current.**

That is deliberately not "a DDNS account" or "a provider". The operator's
sentence is *"I want `home.example.net` to keep pointing at me"*, and the
provider is an implementation detail of keeping that promise — the same
relationship `gateway`'s exits have to `Internet via` (gateway:§1.3).

What it is not:

| | Ours? | Why |
|---|---|---|
| A public A/AAAA record we keep current | yes | this document |
| The `_acme-challenge` TXT record | **no** | `ingress` already writes it through Caddy (ingress:§4) |
| A name on the LAN | no | `dns` answers the local suffix authoritatively (ingress:§3) |
| A port forward that makes the name useful | **no** | `firewall`'s object and `firewall`'s risk conversation — ingress:§9 already drew this line and it is drawn the same way here |

The last row is the one to hold. **A current DNS record does not expose
anything.** Publishing an address and accepting traffic at it are two decisions,
and collapsing them is how a router surprises somebody.

---

## 2. Why `dial`

design.md §4.1's rule decides this: the module that owns the fact owns the
feature. The value we publish is the public address of the uplink, which is
`dial`'s fact and nobody else's. Every other placement has to subscribe to it.

`dial` does not exist yet. That is a scheduling problem, not an argument — the
alternative placements are all worse for reasons that will not change when it
lands:

- **`dns`** is the resolver leg. It owns `:53` inbound, and its scope table says
  *never authoritative*. This is outbound publication into somebody else's zone;
  it shares no fact, no backend and no code path. The operator will look for it
  under DNS, which is a real cost and is paid the way design.md §4.2 pays it for
  RA under `dhcp`: with a pointer, not with the code.
- **`ingress`** is the tempting one, because it already holds a DNS provider
  credential for the same zone, and a proxy binary that can already write to
  that zone. §5 takes that seriously. But ingress:§9 explicitly refuses the
  outward direction, and ingress ships as its own optional package — DDNS
  underneath it would mean *no reverse proxy, no dynamic name*, which is a
  coupling no operator would predict.
- **`link`** stores adoption and nothing else, and its package comment says so
  precisely to keep the real module able to absorb it.

---

## 3. The address is discovered, not assumed

### 3.1 The reference topology does not have a WAN

dns:§1 has olr as a side router behind the modem. The public address is on the
*modem*; the address on our uplink interface is an RFC1918 address that would be
useless in a public A record. A DDNS that reads the interface is wrong for the
flagship deployment.

So there are two sources and the operator picks:

| | `interface` | `reflector` |
|---|---|---|
| Where the address comes from | the adopted uplink, read from the kernel | an HTTPS endpoint that echoes the source address |
| Correct when | olr terminates the WAN — PPPoE, DHCP from the ISP | olr is behind the modem, or behind anything |
| Cost | none | a recurring request to a third party |

**Neither may be inferred.** design.md §5.6 forbids exactly this: picking
`reflector` because the interface address "looks private" is automatic
behaviour that was never declared, and it silently turns a local-only box into
one that talks to a stranger every five minutes.

### 3.2 The reflector is also the only honest CGNAT answer

A reflector reports the address the internet sees. When that address is in
`100.64.0.0/10`, the operator is behind carrier-grade NAT and **the name will
resolve to an address nobody can reach**. That is worth saying out loud in
`status`, because the alternative is an operator debugging their port forward
for an afternoon.

This is the one diagnosis DDNS can offer that nothing else in the product can,
and it is a reason to prefer the reflector form even where the interface form
would work.

### 3.3 Which exit the question is asked through

With more than one exit, *"what is my address"* has no answer until you say
*"leaving how"*. The reflector request has to be bound to the exit whose address
is being published, or a multi-WAN box publishes whichever answer it happened to
get.

Binding is `net.Dialer.Control` with `SO_BINDTODEVICE`, or a `LocalAddr`. It
needs `golang.org/x/sys/unix`, which is already a dependency.

### 3.4 Our own resolver cannot verify the result

`dns` renders the local suffix as an unbound `local-zone ... static` zone, so
this box answers that whole zone itself and forwards none of it. A record we
just wrote into the *public* zone is invisible to anything asking our own
resolver: it gets an authoritative NXDOMAIN.

This is the same trap ingress:§4.2 documents for Caddy's propagation check, and
it is worth noting that `ddns-go` has a `-dns` flag for precisely this reason.
Any confirmation lookup we make must go to a resolver that is not ours, and the
default must not be the system resolver — which on this box is the one that
cannot answer.

---

## 4. The provider implementations are vendored

### 4.1 What was selected, and what was measured

Four candidates, built and measured rather than argued about
(`CGO_ENABLED=0`, `GOOS=linux`, against a 2.0 MB empty-`main` baseline):

| | vendors covered | binary | packages | module closure |
|---|---|---|---|---|
| `ddns-go/v6/dns` | ~30 | 5.5 MB | 213 | 14 |
| `libdns` + one provider | 1 per package | 4.1 MB | 149 | 3 |
| `lego` — whole provider registry | 223 | **79.0 MB** | 1436 | **688** |
| `lego` — one provider | 1 | 7.4 MB | 213 | 232 |

**`lego` is disqualified on function, not on size.** Its provider interface is
`Present(domain, token, keyAuth)` / `CleanUp(...)` — an ACME challenge writes a
TXT record and there is no argument anywhere in that signature for an IP address
or a record type. The real API clients live in `providers/dns/*/internal`, which
cannot be imported. 223 providers, none of which can set an A record.

`ddns-go` covers the most vendors at the smallest closure because every provider
in it is hand-written `net/http` + `encoding/json` with no vendor SDK. That is
also what makes it vendorable.

### 4.2 Why vendored and not imported

Importing it was tried, compiled and run. The API is drivable — a
hand-constructed config, no config file, no `RunOnce`, no package-global cache —
but three things do not survive contact:

```
Ipv4Addr=""  条目数=1
  home.example.net  UpdateStatus=""   == config.UpdatedFailed → false

2026/… Failed to get IPv4 from http://127.0.0.1:1
2026/… Exception: … connect: connection refused
2026/… Failed to get IPv4 address, will not update
```

1. **A failed update is not in the return value.** `UpdateStatus` is the empty
   string, indistinguishable from "nothing happened". The error reaches the
   caller only as text on the standard logger.
2. **The standard logger is `log`, not `slog`.** Capturing it means
   `log.SetOutput` — a process-wide side effect — and then parsing prose.
3. **`config` imports `os/exec`**, because a `Cmd` field can shell out for the
   address. design.md §3.6 forbids `olrd` starting subprocesses.

For this feature specifically, (1) is disqualifying. **Silent failure is the
worst failure mode DDNS has**: the record goes stale, everything looks healthy,
and the operator discovers it at the moment they are away from home and need it.

Vendoring the provider files fixes all three — the signature returns an error,
the logging is ours, and `os/exec` never enters the tree.

### 4.3 Running it as a backend was considered and does not work

It is the shape the rest of the product uses (dnsmasq, unbound, Caddy), and
`ddns-go` has the flags for it: `-c`, `-f`, `-noweb`, `-dns`. It fails on the
same axis as importing. Its status lives behind the web UI, which is
login-authenticated (`web/auth.go`, `web/login.go`); with `-noweb` there is no
machine-readable status at all, only the same `log.Printf` text in the journal.
The failure reporting does not improve by crossing a process boundary.

The second price is a binary Debian does not ship, and `ingress` has just
settled what that costs: it asks the operator to supply the proxy, and its own
commit says the cost "is only bearable if we say so at the moment it bites". For
a reverse proxy that is a defensible ask. For a request every five minutes it is
not — we would be sending somebody to a download page to keep a name current.

The distinction against dnsmasq and unbound is worth stating because it is the
whole reason the precedent does not apply: **those are infrastructure we could
not rewrite. This is a table of API shapes.** Thirty providers at roughly a
hundred and fifty lines each of HTTP calls is not a daemon, and supervising a
process to make one request every five minutes is machinery out of proportion to
the task.

### 4.4 What vendoring obliges us to

- **The MIT notice is retained on every vendored file.** There is currently not
  one third-party copyright header in this repository, so this is a new
  convention and should be introduced deliberately: a header naming the upstream
  file, its commit, and the license.
- **Upstream fixes are ours to track.** A provider changing its API is upstream's
  problem to notice and ours to pull. This is the real cost and it is the only
  one.
- **The list grows, it does not land at once.** Thirty providers is roughly four
  and a half thousand lines of somebody else's code, and a single commit of that
  is not reviewable. v1 vendors the ones we can actually exercise; the rest
  arrive with the operator who asks for them.
- **An unknown provider name is refused.** Upstream's dispatch ends in
  `default: dnsSelected = &Alidns{}` — a typo silently becomes Alibaba DNS. Our
  validator rejects a name that is not in the list, and there is no default.

---

## 5. The credential, which `ingress` already has one of

`ingress.Certificate.Token` is a DNS provider API token for a zone, already
redacted on every printing surface and already rendered into an environment file
rather than into the config Caddy reads (ingress:§4.1).

DDNS wants **the same token, for the same zone, from the same provider.** An
operator who has configured a wildcard certificate and then types their
Cloudflare token a second time to get a dynamic name is being asked to do the
same work twice, and the second copy is a second thing to rotate.

`ingress` has already had this argument once, about its provider *list*, and
settled it by deletion — `internal/ingress/providers.go` now records the
reasoning rather than the list:

> This file used to hold a hand-written list, and that list was a defect with a
> note attached: the legal set is whatever the proxy binary was linked with, so
> anything written down here was a second copy that could disagree with it. The
> copy is gone.

A credential is not a list, and it cannot be resolved by asking a binary. But
the same objection applies with more force, because a second copy of a *secret*
does not merely disagree — it has to be rotated twice and will not be. So the
provider credential should become a shared object owned by one module and
referenced by both, in the way design.md §4.4 does it for group and device. That
is a design.md-level decision rather than this document's, and it is §9 #1.

Until it is made, this document assumes DDNS holds its own credential and
carries the same two obligations ingress:§4.1 states: redacted on every surface,
never written into a file we would show somebody.

---

## 6. Mechanism

Ours, not upstream's. Upstream's `RunTimer` is `for { RunOnce(); sleep }`, which
is the right shape for a program with one job and the wrong one for a module
that has to answer `status` and participate in design.md §5's apply semantics.

- **A check is not an update.** The address is read on a schedule; the provider
  is called only when it changed. The cache is ours and it starts empty after a
  restart, which means the first check after a restart always publishes.
- **Backoff is on the provider, not on the address.** Providers rate-limit and
  some of them ban. A rejected update backs off; a failed address read does not
  need to, because it costs a stranger nothing.
- **`status` answers three separate questions** — when the address was last
  read, what it was, and whether the last publish attempted with it succeeded.
  Collapsing those into one "OK" is how §7's failure becomes invisible.

---

## 7. Failure modes

| | What the operator sees | What we must not do |
|---|---|---|
| Reflector unreachable | last checked, and that it failed | keep the old address and report success |
| Address read, provider rejected | the provider's own error text | retry in a tight loop |
| Credential wrong or revoked | `badauth`, named as such | treat it as a transient failure |
| Behind CGNAT | the name resolves to an unreachable address (§3.2) | report a healthy record |
| Record correct, nothing reachable | nothing — this is `firewall`'s question | imply that publishing exposed anything |

The first row is the one this whole design is arranged around. It is the
failure that upstream's return value cannot express (§4.2) and it is the reason
the code is vendored.

---

## 8. Scope

| | | |
|---|---|---|
| **v1** | one name per exit, A record | the operator's sentence in §1 |
| | `interface` and `reflector` address sources, chosen explicitly | §3.1 |
| | a vendored provider set we can exercise, growing by request | §4.4 |
| | CGNAT detection in `status` | §3.2 — the diagnosis nothing else offers |
| **v2** | AAAA | §9 #2 decides what it even means |
| | the credential shared with `ingress` | §9 #1 |
| **Never** | the port forward that makes the name useful | `firewall`'s object; §1 |
| | our own DNS provider API for a provider nobody asked for | the list grows by request, not by completeness |
| | managing the zone — other records, TTL policy, delegation | this is not a DNS control panel |

---

## 9. Open

1. **Is the DNS provider credential a shared object?** (§5) It is the only
   question here that affects another module, and the answer decides whether
   DDNS reuses `ingress`'s token or holds its own. Belongs in design.md §10.
2. **What does an AAAA mean on a router?** With a delegated prefix the
   interesting address is usually a *device's*, not the router's — the NAS, not
   the gateway — because there is no NAT to forward through. That turns one
   field into a record set and pulls in `devices`. v1 publishes the router's own
   address and says so, rather than growing into a zone editor by accident.
3. **Which reflector, and whose.** A default means sending every olr
   installation's address to one operator's endpoint on a timer. Shipping no
   default means the feature does not work out of the box. Neither is obviously
   right and the choice should be made deliberately rather than by whichever
   URL gets typed into the code first.
4. **Does `dial` exist before this does?** If DDNS is wanted before `dial`
   lands, the only honest options are to wait or to build the smallest piece of
   `dial` that owns an uplink address — not to put it somewhere else and move it
   later.

---

## 10. Considered and rejected

- **`lego`.** §4.1. Its provider interface cannot set an A record, and
  `ingress` had already rejected it for the certificate job (ingress:§10).
- **`libdns` + `certmagic`.** The pairing is real — certmagic's DNS solver takes
  a `libdns` provider, so one credential could serve both DDNS and ACME. It is
  moot here: `ingress` obtains certificates through the proxy binary, so the
  ACME half is already built at the backend layer, and embedding Caddy as a
  library is itself a rejected option (ingress:§10). What remains of the idea is
  the shared credential, which is §9 #1.
- **`ddns-go` as an imported module.** §4.2. A failed update is not visible in
  the return value.
- **`ddns-go` as a supervised backend.** §4.3. Same failure, across a process
  boundary, plus a binary the operator would have to go and fetch.
- **Writing the provider calls ourselves.** The long tail is reachable with one
  implementation of the DynDNS v2 protocol — `/nic/update?hostname=…&myip=…`
  with basic auth, which No-IP, Dynu, afraid.org and deSEC all speak. That is
  true and it is still not a reason to hand-write the thirty that do not,
  several of which are the ones our operators actually use. Vendoring proven
  implementations is not the same as writing them.
