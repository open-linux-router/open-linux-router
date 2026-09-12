# `ddns` design — publishing a changing address

Status: **built**, as the first and so far only contents of `internal/dial`.
This was written before the code so the expensive decisions were arguable while
they were still cheap; §11 records what building it changed and what is still
missing. Bare section references are to this document; references to `design.md`
name it, and `ingress:` means `docs/ingress.md`.

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

A credential is not a list, and it cannot be resolved by asking a binary. The
obvious move from there is a shared object — one owner, both modules
referencing it, the way design.md §4.4 does it for group and device.

**That move was considered and refused**, and the refusal is design.md §3.4's
*everything is data* rule. A credential is not a special kind of field; it is a
field. A shared one would mean either a secrets sidecar, which turns backup into
two things that can be restored separately, or a reference by name, which adds a
broken-link failure that a literal value cannot have. Both are real costs and
neither buys anything on a single-admin box.

So **DDNS holds its own token**, as ordinary data, in `dial`'s section of the
config document. What is left of the problem is not architectural:

- **Do not make somebody type it twice.** If `ingress` already holds a token for
  the same provider, the setup path offers it rather than presenting an empty
  field.
- **Rotation is the failure that matters.** Two copies, one updated: DDNS breaks
  within minutes and visibly, certificate renewal breaks in thirty days and
  silently, and the silent one is the one that takes every published service's
  HTTPS with it. `status` notices when two modules hold different tokens for the
  same provider and says so. That is a check, not a schema.

Both copies carry the obligations ingress:§4.1 states: redacted on every
surface, never written into a file we would show somebody.

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

1. ~~**Is the DNS provider credential a shared object?**~~ **Closed — no.** (§5)
   design.md §3.4 says everything is data and there is no secrets store, so DDNS
   holds its own token. What survives is a `status` check for two modules
   holding different tokens for one provider, which is where the rotation
   failure shows up.
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
4. ~~**Does `dial` exist before this does?**~~ **Closed — it does now.** The
   second option was taken: `internal/dial` is the smallest piece of the module
   that owns an uplink address, and it contains this feature and nothing else.
   It invents no uplink object, because the reference topology (dns:§1) has no
   WAN uplink at all and a `dial.uplink` field would be empty on the deployment
   we lead with — the same refusal `link` makes about a primary key, for the
   same reason. The cost is that the module's name runs ahead of its contents,
   which is stated in its package comment rather than hidden.

---

## 11. What building it changed, and what is still missing

### 11.1 Where the code differs from what is written above

- **A record carries two credential fields, not one.** §5 says "the credential"
  throughout, because Cloudflare's is one value. Alibaba Cloud and Tencent
  Cloud — two of the four providers v1 ships — authenticate with a key *pair*,
  so `Record` has `provider_key_id` beside `provider_token`. Nothing else in §5
  changes: both halves are ordinary data in `dial`'s section, and the secret
  half is redacted everywhere.
- **The callback URL is redacted too.** It does not look like a credential and
  for this provider it is one: DuckDNS, Dynu and most DynDNS v2 endpoints put
  the token in the query string. It costs an operator the ability to read back
  what they typed, which is what they already cannot do with a token.
- **A name carries a `zone` field.** Every provider addresses a record as
  zone-plus-the-labels-below-it. The split is derived with the public suffix
  list, as ddns-go does; the field is the override for the zones that list
  cannot know about. Getting it wrong produces "no such zone", which reads like
  a credential problem and is not one.
- **More than one record of the same name and type is refused.** Upstream
  updates every match, which silently collapses a round-robin somebody else set
  up. §8 says this is not a DNS control panel, and this is where that line
  falls.

### 11.2 Known gaps

Stated here rather than left to be discovered, per the rule that we document
what we do not cover instead of implying coverage we do not have.

- **Not verified end to end.** Each provider is tested against an `httptest`
  server asserting the exact request it builds — including both signature
  recipes — and the schedule, the backoff and the CGNAT verdict are unit-tested
  with an injected clock. The whole path has been exercised against a local
  reflector and a local callback endpoint. **No real provider account has been
  used**, so a wrong field name or a changed API at Cloudflare, Alibaba Cloud or
  Tencent Cloud would show up first for whoever tries it.
- **The reflector is bound by interface, not by exit.** §3.3 asks for the
  question to be asked through the exit whose address is published.
  `gateway.DialThrough` does exactly that with `SO_MARK`, and `dial` cannot call
  it: `dial` is foundation, `gateway` is a service module, and design.md §4.1
  forbids the arrow. v1 uses `SO_BINDTODEVICE`, which is what §3.3 specifies
  anyway; per-exit binding waits for a fact subscription in the legal direction.
- **The rotation check does not exist.** §5 and §9 #1 leave a `status` check for
  two modules holding different tokens for one provider. It has a home now that
  `dial` exists, and it is not built — so an operator who rotates a Cloudflare
  token in `ingress` and not here still learns about it when DDNS breaks.
- **Nothing offers the token `ingress` already has.** §5's "do not make somebody
  type it twice" is unimplemented; the setup path presents an empty field.
- **AAAA is not published**, by §9 #2 rather than by omission. The record type
  is a constant, so nothing in the schema or the CLI offers a choice that the
  validator would then refuse.
- **No default reflector**, by §9 #3, which is still open. The field is required
  and the error says why rather than leaving the absence looking like an
  oversight.
- **There is no `enable`/`disable`.** Pausing means removing the record, and
  removing a record leaves the name at the provider pointing at whatever was
  last published. Worth revisiting if anybody wants to stop publishing for a
  week without retyping a credential.

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
