# `ingress` module design

Status: **not built.** This document is the argument, not a record of code.
Section references are to `design.md` unless prefixed.

Five things were decided before this was written, and the rest of the document
is mostly their consequences: the backend is **Caddy**; it is **bundled, not
embedded, and not taken from the distro**; Caddy **owns :80 and :443**; the
first-boot path stays **IP + port**; and certificates are Caddy's own job rather
than a separate ACME client's.

One thing was *not* decided and is the largest question here: whether olr should
have this module at all. §2 is that argument, and it is a positioning question
(§2 of `design.md`), not a technical one.

---

## 1. The object: a published service

An operator has something running on their network — Home Assistant on a NUC,
Grafana in a container on the router, a NAS web UI — and wants to reach it at
`https://grafana.home.example.com` from a phone, without a port number and
without a certificate warning.

Today that costs four separate pieces of work: a DNS record, a wildcard
certificate, a reverse-proxy stanza, and a redirect from `:80`. Three of the
four are set up **once** and never thought about again. Only one of them varies
per service. So the object the operator creates has two fields:

| Field | Example | |
|---|---|---|
| `name` | `grafana` | the label under the module's domain |
| `upstream` | NUC : 3000 | where the request goes |

Everything else is derived and never asked:

- the certificate — one wildcard covers every name that will ever exist
- the DNS record — likewise, one wildcard
- the Caddyfile stanza
- the `:80` → `:443` redirect

That is the whole claim of this module. **Adding a service is one name and one
target**, because the expensive parts were paid once when the module was
enabled.

### 1.1 Module-level config, set once

`domain`, the DNS provider and its credential, and an ACME contact address.
Three fields, entered at enable time, never revisited.

### 1.2 The upstream is a device, not an address

Per §4.1 a module reads another module's facts through that module's API and
never keeps a copy. The upstream should therefore reference a **device**
(`devices` owns identity, §4.4) plus a port — not an IP address copied into our
config where it will rot.

This surfaces a constraint the operator has to be told about rather than
discover:

> **A published service needs a stable address.** A device whose address comes
> from a DHCP pool can move, and the rendered stanza then points at whatever
> took its place.

`dhcp` already owns fixed addresses. So selecting a device with no fixed address
must **offer to create one** as part of publishing, not render a stanza that
works today and silently proxies to a stranger's laptop next week. This is a
concrete instance of §5.6 — automatic behaviour declared, never inferred.

A raw `host:port` form stays available for targets that are not devices: a
container on the router itself, `127.0.0.1:3000`, something behind another
router.

---

## 2. Why this is in a router at all

It is worth being honest that this module is not networking. It is hosting.
Every other module in §4 manages how packets move; this one manages how an
application is reached. It is the first feature that makes olr look less like a
router and more like a home server platform, and that is a change to §2's
positioning, not an addition to it.

The case for:

- It is the **always-on box with the LAN address**. The reverse proxy has to
  live somewhere, and every other candidate is a box that might be off.
- The recipe is **identical for everyone** and annoying for everyone. Wildcard
  cert, DNS-01, split horizon or a public record, one stanza per service. This
  is exactly the shape of thing olr exists to collapse into a form.
- It is **separable**. Unlike `firewall` or `dial`, nothing else depends on it,
  it ships as its own package, and removing it removes nothing from the router.
  If the positioning bet is wrong, the cost of unwinding it is one package.

The case against is one sentence and it is not weak: **there is no natural
stopping point after this.** Once olr publishes services it will be asked to
run them, back them up, and watch them. The boundary has to be drawn in this
document or it will not be drawn at all. §9 draws it.

---

## 3. The name has to resolve, and that is not free

`https://grafana.home.example.com` requires a client on the LAN to receive the
router's private address for that name. Two ways, and v1 takes the cheap one.

### 3.1 A public wildcard record (v1)

`*.home.example.com A 192.168.1.2`, in the operator's real public DNS. Works
from every resolver, needs no split horizon, and needs **no code in olr at
all** — we document it and stay out of it.

What it costs, stated plainly:

- Anyone can query it and learn your internal addressing. This is topology
  disclosure, not access — the address is unroutable from outside — but it is
  disclosure.
- The name stops working the moment you leave the LAN. For internal-only
  services that is correct behaviour, and it will still surprise people.

A non-obvious reassurance worth writing down: **this does not interfere with
DNS-01.** ACME validates via a `TXT` record and does not care that the `A`
record points somewhere unroutable. The two uses of the zone are independent.

### 3.2 A local override in the relay (v2)

`dnsrelay` answers the configured suffix itself, and no public record exists.
Better on disclosure, and it works for operators who own no public domain.

It is also not ours to decide. `dns.md` §6 lists *authoritative service, zone
transfers* under **Never**, permanently. A wildcard override for one
operator-declared suffix is a long way from zone service — but it is the same
direction, and the `dns` module owns that call. It also adds an edge to §4.1's
DAG (`ingress → dns`) that does not otherwise exist.

Left open (§11). v1 does not need it.

---

## 4. Certificates

Three ways to get a browser-trusted certificate for an internal name. Two of
them are dead on arrival and it is worth recording why, so they are not
re-proposed.

| | Works for an internal-only name? | Cost |
|---|---|---|
| HTTP-01 | **No** — the name must be publicly reachable on `:80` | would mean exposing the router to get a cert for something that is not exposed |
| Local CA (Caddy's internal issuer, mkcert) | Yes | a root certificate installed on **every** device — fine for two laptops, fatal for phones, TVs, consoles and guests |
| **DNS-01, wildcard** | **Yes** | an API token for your DNS provider, stored on the router |

So DNS-01, and a wildcard. The wildcard is what makes §1's claim true: one
certificate covers every service that will ever be published, so adding a
service involves no certificate step whatsoever.

### 4.1 The token has no proper home, and we should say so

olr has no secrets store. The provider credential will land in a rendered file
— the Caddyfile or an environment file the unit reads — mode `0600`, owned by
root. That is the same trust boundary as every other secret on the box, and it
is not nothing: **anyone with root on the router can edit your DNS zone.**

Two mitigations that cost nothing and belong in the setup copy:

- **Scope the token to the single zone** if the provider supports it (Cloudflare
  does). A token that can write `home.example.com` and nothing else has a much
  smaller blast radius than an account key.
- Use a **dedicated zone or subdomain** delegated for this purpose, rather than
  the domain that also serves your mail.

This is a stopgap and the document should keep calling it one, rather than
implying a key-management story that does not exist.

---

## 5. The backend: Caddy, bundled

### 5.1 Bundled is not embedded

These are routinely conflated and their costs have nothing in common:

| | Embedded (Caddy as a Go library, inside `olrd`) | **Bundled** (our build, its own binary and unit) |
|---|---|---|
| `olrd` dependency count | hundreds of modules against a README that counts six as a feature | **unchanged** |
| `olrd` binary size | several times its current size | **unchanged** |
| §3.5 (backends run in their own unit) | violated | **respected** |
| A cert-renewal panic | takes DHCP and the API down with it | takes the proxy down |

Every objection to putting Caddy in the control plane is an objection to
embedding. None of them apply to shipping a separately built binary. So the
process model is the same one `dhcp` has with dnsmasq — render config, manage a
unit, signal a reload — and the only thing that differs is where the binary came
from.

### 5.2 Why our build and not a packaged one

No official Caddy package includes DNS providers — not Debian's, and not
Caddy's own apt repository. This is architectural rather than an oversight:
providers are compiled-in modules and Caddy has no runtime plugin loading, so
there is no official build that has them. `xcaddy` is the supported mechanism
and using it is not going off-road.

The alternative — a separate ACME client (lego, certbot) obtaining the wildcard
and Caddy reading the files via `tls <cert> <key>` — was seriously considered
and rejected in §10. It keeps a stock Caddy at the price of two backends, two
rendered configs, a certificate handoff with its own permissions and
reload-on-renew race, and a renewal status we would assemble ourselves out of
another tool's exit codes. That is three pieces of new plumbing bought with the
one piece we were trying to avoid.

### 5.3 What bundling actually costs

One thing, and it is ongoing rather than one-off:

> **Security updates become our obligation.** When Caddy patches a
> vulnerability, `apt` updates the distro's copy and does nothing for ours. Our
> users are patched when we cut a release.

This is the real price and it should be written into the project's commitments,
not discovered during an incident. The mitigation is mechanical — a CI job that
watches Caddy releases and opens a rebuild — but it is a standing job that did
not exist before this module.

### 5.4 Packaging shape

- **Its own package**, `open-linux-router-caddy`, with the main package
  declaring `Recommends:` rather than `Depends:`. The obligation in §5.3 is then
  visible and declinable rather than silently inherited by every install.
- **Installed to `/usr/lib/open-linux-router/caddy`**, supervised as
  `olr-caddy.service`, configured under `/etc/open-linux-router/`. Path, unit
  name and config path all differ from the distro package's, so a box that
  already has `caddy` installed has no conflict to resolve. This is not
  optional politeness; it is the difference between working and not on any box
  that has ever run a web server.
- **Compile in the whole `caddy-dns` provider set**, not a curated subset. A
  curated subset turns "my DNS provider isn't supported" into a feature request
  that requires a release. Binary size is the cheaper side of that trade.

---

## 6. Ports, and the one rule that must not be optimised away

Caddy owns `:443`, and `:80` for the redirect. Consequences:

**Enabling must preflight both ports** and refuse with a message that names the
process holding them. An existing nginx or Apache is the single most likely
install failure for this module, and "Caddy failed to start" is not an
acceptable way to learn it.

**Bind to LAN interfaces only.** `dns.md` §5 makes this point about the
resolver and it applies here with more force: a reverse proxy reachable from the
WAN is a way to publish internal services to the internet by accident. Exposure
to the internet is `firewall`'s decision, made explicitly, with a different
conversation attached — see §9.

**The `:8080` API listener is never turned off by this module.** Ever.

> When Caddy is misconfigured, its certificate has expired, or `:443` is held by
> something else, `http://<router-ip>:8080` is the only remaining way in to fix
> it.

This is `design.md` §5.5's lockout guard in a new costume, and worse than the
original because the breaking configuration is one the operator typed. First
boot is already IP + port (`olr listen 0.0.0.0:8080` plus a token, per
`docs/install.md`), so nothing needs to change — what needs to change is that
listener's *status*, from "a step during setup" to **a permanent recovery
path**. Written down here because the temptation to retire it once a nice domain
exists is obvious, and will recur.

The WebUI **may** be published through Caddy — `router.<domain>` →
`127.0.0.1:8080` — as an ordinary entry with no special case in core. That is a
nicety, it is opt-in, and it changes nothing above.

---

## 7. Mechanism

Intent in `/etc/open-linux-router/ingress.json` (§3.2 rule 1); a rendered
Caddyfile; a unit we supervise over D-Bus. Nothing novel — the point is that it
is the same shape as `dhcp` and `dns`.

### 7.1 Reload, not restart, and validate before either

Two ways this differs from dnsmasq and both matter:

- **A restart is visible to users.** Reloading dnsmasq costs a DHCP request that
  the client retries. Restarting the proxy drops connections in the middle of
  somebody's video call. `caddy reload` replaces config without dropping
  listeners and is the only acceptable apply path.
- **A bad config takes down every service at once**, not just the one being
  edited. So the render must be run through `caddy validate` *before* it is
  applied, and a failure must be reported as a rejected change rather than a
  dead proxy. This is the `plan`/`validate` step §3.2 already expects, with
  unusually high stakes.

### 7.2 We do not use Caddy's admin API

Caddy exposes an admin API that can mutate config without touching a file, and
it is tempting. It is rejected in §10: config that lives only in a running
process is not revisioned, not readable over SSH during an outage, and not
single-source. File plus reload keeps this module identical to every other one.

### 7.3 Escape hatch

`ingress.raw_caddyfile`, per §3.2 rule 5 — passed through verbatim, declared in
our config, rendered by us. Caddy can do vastly more than publish an internal
service, and the answer to all of it is this field plus the documentation of a
tool we did not write.

---

## 8. Failure modes

**This is the only module in olr with state that decays on a clock.** Everything
else is either applied or not; a certificate is applied and then, ninety days
later, stops being valid. That difference drives most of this section.

It also comes with a grace period that is a trap. Caddy renews well before
expiry, so the first failed renewal costs nothing and the tenth costs nothing —
there are weeks of successful serving between the thing breaking and the thing
being visible. By the time symptoms appear the cause is a month old.

| | What happens | What `status` must say |
|---|---|---|
| Renewal fails (token revoked, provider API down, zone moved) | Nothing, visibly. The existing certificate keeps serving until it expires, then every service breaks at once | Days remaining and last successful renewal, as an **active check** — not a log line nobody reads |
| Rendered config rejected | Caught by §7.1's validate; the change is refused | The refusal, with Caddy's own message |
| Upstream device down | 502 | Distinguish *configured but unreachable* from *not configured* |
| Upstream address moved | Stanza proxies to whoever took the address | Prevented at write time by §1.2, not detected after |
| `:80`/`:443` taken | Unit fails to start | Caught at preflight (§6), by the process name |

The first row is the one that will actually bite people, and it is why
certificate expiry deserves a place in the module's status output rather than
being left to Caddy's logs.

---

## 9. Scope

The boundary §2 said had to be drawn here.

| | | |
|---|---|---|
| **v1** | published services: name + upstream, one wildcard certificate via DNS-01 | the whole product claim of §1 |
| | `:80` → `:443` redirect, LAN-only bind, port preflight | §6 |
| | certificate expiry and renewal state in `status` | §8; the one clock-dependent thing we own |
| | `raw_caddyfile` escape hatch | §7.3 |
| | public wildcard `A` record, **documented not managed** | §3.1 |
| **v2** | local DNS override, so no public record is needed | §3.2, needs the `dns` module's decision |
| | publishing the WebUI itself | §6 |
| | per-service access control by group or device | the thing a router can do here that a standalone Caddy cannot — and the only entry on this list that justifies the module living in olr rather than in a README |
| **Never** | serving static files, PHP, a general web server | escape hatch, permanently |
| | **exposing a published service to the internet** | that is a port forward: `firewall`'s object, `firewall`'s risk conversation, and it must not become a side effect of publishing something internally |
| | running, updating or backing up the services themselves | §2's boundary. We make an application reachable. We do not manage applications |

---

## 10. Considered and rejected

- **Caddy as an embedded library.** §5.1. Every cost of embedding is real and
  none of them were costs of bundling; the objection was aimed at the wrong
  target.
- **A distro or official-repo Caddy, with `lego` obtaining the certificate.**
  §5.2. Preserves "we only wrap packages the distro ships" at the cost of two
  backends, two configs, a certificate handoff and a renewal status we would
  reconstruct from another tool. The separation of failure domains it buys is
  real but is not worth three new moving parts, and Caddy's certificate
  automation is the most mature part of Caddy.
- **`caddy add-package` / `caddy upgrade --plugin`.** Fetches a rebuilt binary
  from a third-party build service at runtime and replaces itself in place —
  which `apt` will then overwrite on the next upgrade. Unverified whether it
  even works unattended; rejected regardless, on both counts.
- **HTTP-01, and a local CA.** §4. One requires exposing the router, the other
  requires touching every device the operator owns.
- **Caddy's admin API instead of a rendered file.** §7.2.
- **Naming this module `proxy` or `gateway`.** Both words are taken and mean
  the opposite direction of travel — `gateway.md` uses them for egress and for
  the operator's mihomo/clash box. A third word was needed.

---

## 11. Open

1. **Should olr have this module at all** (§2). The positioning question, and
   the only one on this list that can cancel the others. It is not a technical
   decision and should not be settled by a technical argument.
2. **Local DNS override versus a public wildcard record** (§3.2). Needs the
   `dns` module's call on how far from "never authoritative" this sits, and adds
   an `ingress → dns` edge to the §4.1 DAG.
3. **Where the DNS provider token lives** (§4.1). A rendered `0600` file is the
   answer for now, and it is the first time olr has held a credential for a
   third-party account. If a secrets story is ever going to exist, this is the
   workload that starts it.
4. **The `devices` dependency** (§1.2). Referencing a device is right by §4.1
   but pulls `ingress → devices`, and forces the fixed-address interaction at
   write time. Confirm that the offer-to-create-one flow is acceptable rather
   than a refusal.
5. **Ordering.** `dial`, `firewall` and `system` are unbuilt — `internal/` has
   `link`, `dhcp`, `dns`, `devices` and `gateway`. Publishing services before
   the box has a firewall is hard to defend, and §9's "never expose to the
   internet" row is a promise that `firewall` is what actually keeps.
6. **Who watches Caddy releases, and how fast** (§5.3). The obligation is
   accepted in principle; the cadence is not defined, and an undefined cadence
   is how this becomes a stale bundled binary two years from now.
