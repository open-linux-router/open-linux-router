# `system` module design — claiming a box, and the first five minutes

Status: **designed, not built.** Section references are to `design.md` unless
prefixed `ingress:` or `dns:`, which mean `docs/ingress.md` and `docs/dns.md`.

This document decides what happens between `apt install` and a working router,
and it exists because that stretch is where every olr release since v0.1.3 has
been found broken — not in a module, but in the path a new box takes before it
has configured anything. v0.1.4 could not find dnsmasq. v0.1.5 wrote a unit
naming a binary it had not installed. v0.1.6 served a JSON authentication error
where the web UI should have been. Each was the next wall after the last, and
none of them was in a feature.

The remaining wall is that the UI does not exist until you run a command nobody
tells you about, and that is the one this document removes.

It also settles a contradiction we are currently shipping. §6.2 says the TCP
listener "is always authenticated" and that an unauthenticated one "refuses to
bind anything but loopback". As of v0.1.6 neither sentence is true: the listener
is unauthenticated by default, on any address. That was a deliberate product
decision and this document keeps it, but §6.2 has to stop describing a system
that is not there. §10 is amended in §10 below.

---

## 1. The shape of a first install

Both paths converge on **one command, then a URL**. That is the whole goal, and
everything below is in service of making it safe rather than making it shorter.

### 1.1 From the package

```
$ sudo apt install ./olr_0.2.0_amd64.deb
...
olrd is running on http://192.168.1.91:8080

Nothing else on this machine has changed — no DHCP server, no resolver, no
firewall rule. Open that address to finish setting up.
```

### 1.2 From the tarball

```
$ tar xzf olr-0.2.0-linux-amd64.tar.gz
$ sudo ./olr enable
  wrote /usr/local/bin/olr  (copied from /home/you/olr)
  wrote /lib/systemd/system/olrd.service
  ...
enabled olrd.service
started olrd.service

olrd is running on http://192.168.1.91:8080

Nothing else on this machine has changed. Open that address to finish setting up.
```

### 1.3 Then, in the browser

One screen. Not a wizard, not a tour, not an account:

> **This router has not been set up yet.**
>
> Anyone on your network can reach this page right now. Choose how you want to
> get in from here on:
>
> - **No password** — anyone on this network can configure this router.
>   Right for a home network you control.
> - **Set a password** — [________]
>
> [ Continue ]

After that, the dashboard. The whole first run is: install, open a link, make
one choice.

### 1.4 What changes to produce that

- `olrd` listens on `0.0.0.0:8080` by default instead of only on its control
  socket. §9 keeps the port.
- `postinstall.sh` and `olr enable` print reachable URLs instead of printing a
  command to run.
- `olr listen` stops being a required step. It stays, because changing the
  address or closing the listener are both real operations — it just stops
  being the thing standing between an install and a UI.

---

## 2. The object: a claim

> **A box is claimed when an administrator has recorded how they want to reach
> it. Until then it will not accept configuration from the network.**

The state lives in the document, like every other piece of intent (§3.2):

```json
"system": {
  "access": {
    "claimed_at": "2026-09-12T18:04:11Z",
    "password": { "hash": "$argon2id$..." }
  }
}
```

and `"password": null` is the recorded choice of no password — **not** the same
as the key being absent. That distinction is the whole design. An absent
`access` section means nobody has decided; a null password means somebody
decided, in a UI that told them what they were choosing, and we can stop
worrying on their behalf.

This is what resolves the tension v0.1.6 created. The objection to shipping an
unauthenticated admin API was never really "no password" — plenty of home
networks want exactly that. It was "no password, because the question was never
put to anybody." A recorded choice is not a weaker version of authentication; it
is a different thing, and it is the thing §7's *nothing happens until you say
so* actually asks for.

---

## 3. The invariant that makes listening-on-install safe

> **An unclaimed box cannot be configured over the network.**

While no `access` section exists, the TCP listener serves exactly two things:

| | Unclaimed | Claimed |
|---|---|---|
| The SPA and its assets | yes | yes |
| `POST /api/system/access/claim` | yes, subject to §4 | gone (409) |
| Every other `/api` route | **409 `unclaimed`** | per the recorded choice |
| The unix socket | unaffected, always full access | unaffected |

This is the load-bearing part of the document, and it is what lets the listener
be open from the first second of an install. Without it, "listen by default" and
"no password by default" compose into an unauthenticated router admin API
appearing on a LAN with no human action — which is a different and much worse
posture than either decision alone. With it, the window between `apt install`
and a human choice is not an exposure at all: there is nothing to reach through
it.

It also means the invariant has to hold at the **API** layer and not in the UI.
A scanner does not load the SPA; it posts to `/api/dhcp/config`. The refusal
belongs beside the router table, next to where authentication is applied
(`internal/daemon`, `authenticateAPI`), and the SPA's onboarding screen is
merely the pleasant face of a rule enforced elsewhere.

The unix socket is deliberately untouched, for the reason §6.2 already gives:
its access control is its file mode and group, which a local caller has already
satisfied. Somebody with root on the box does not need to claim it in a browser
to use `olr`, and making them would be theatre. `olr claim` (§8) exists for the
operator who never opens one.

### 3.1 Why not a time-boxed window instead

The obvious alternative is to allow anything for the first N minutes after
install and then lock down. Rejected: it converts a security property into a
race against the operator's attention, and its failure mode is a box that can
no longer be set up by the person who owns it — *"I locked myself out of my own
router"* is worse for a household than the risk it removes, and it is the kind
of failure that arrives at the least convenient moment (§5.5's concern in a new
costume).

Unclaimed is therefore an indefinite, safe, loud state: nothing can be
configured through it, `olr status` reports it, and olrd says so in the journal
at every start.

---

## 4. Who may claim

Claiming is the one privileged act available without a credential, so it is the
one place that needs a rule of its own.

**Allowed:**

- The unix socket, always. Root on the box outranks anything here.
- An HTTP source in a private, link-local or loopback range — RFC1918,
  `fc00::/7`, `169.254.0.0/16`, `fe80::/10`, `127.0.0.0/8`, `::1`.

**Refused**, with an explanation and no state change:

- Anything else, which in practice means the public internet.

The case this is for is concrete and not hypothetical: somebody installs olr on
a VPS, or on a box with a routable address, and the first thing to find the open
port is a scanner rather than its owner. Trust-on-first-use is fine on a home
LAN and indefensible on a public address, and the source range is the cheapest
honest way to tell those apart.

The page still renders for a public client — refusing to serve HTML would only
make the box look broken — and says:

> This router can only be set up from its own network. Reach it from a machine
> on the LAN, or run `sudo olr claim` on the box over SSH.

**What this does not protect against, stated rather than hidden:** a hostile
device already on your LAN, claiming the box in the minutes before you do. For
that, `sudo olr claim --code` prints a one-time code the browser must present,
which turns first-run into proof-of-shell. It is not the default, because
requiring it always would recreate exactly the "go read a file over SSH and
paste it" friction that v0.1.6 deleted — and on the networks most olr boxes live
on, it buys nothing. Capability without complexity, per the README.

---

## 5. Why "no password" is offered at all

Because it is what most of these boxes want, and because a product that refuses
to offer it gets it anyway — via a password of `router`, written on a sticky
note, reused from something that matters.

The alternative designs and why they lose:

| | Why not |
|---|---|
| Always require a password | The home case is a LAN the operator already trusts; the router's own web page is not more sensitive than the switch it sits next to. Mandatory credentials here produce weak ones. |
| Generate one and show it once | Now it has to be stored somewhere by the operator, and the thing they will store it in is a text file next to their SSH key. |
| `admin`/`admin`, force a change | A default credential that exists on every box for the seconds before it is changed, and forever on the boxes where it is not. |
| Token in a file, as v0.1.5 had | It could not be entered — the page that asks for it was behind it — and once fixed it is still a shared secret with no rotation story. |

What makes offering it defensible is §2: it is a *recorded* choice, made on a
screen that states the consequence in the same sentence as the option. And it
is reversible — `olr access set --password` later, without reinstalling
anything.

---

## 6. After the claim

| State | TCP listener | `olr` over the socket |
|---|---|---|
| Unclaimed | SPA + claim only (§3) | full |
| Claimed, no password | full, no credential | full |
| Claimed, password | login required | full |

A claimed-with-password box gets a session cookie, not a bearer token in
`localStorage`. The existing `AuthGate` is close to the right shape already and
its prompt becomes a login form; what changes underneath is where the credential
comes from and how long it lives.

**The UI states the posture permanently, quietly.** A box claimed with no
password shows it — a line in the header or the footer, not a nag — because
"nobody has a password on this" is a fact an operator should be able to see six
months later without reading a config file. That is §5.6's *automatic behaviour
must be declared* applied to a posture rather than an action.

---

## 7. Surfaces

Per §6 *one schema, four surfaces*, this is a module like any other, with one
top-level verb borrowed from `link`'s precedent.

```sh
olr claim                       # interactive: asks, once, like the UI does
olr claim --no-password         # record the choice non-interactively
olr claim --password            # prompt for one (never as an argument)
olr claim --code                # print a one-time code for the browser (§4)

olr access show                 # what the posture is
olr access set --password       # change it later
olr access set --no-password    # remove it, deliberately
```

`olr claim` is top-level and `olr access` is the module, which looks
inconsistent and is not: `adopt`/`release` set the precedent that **consent is a
top-level verb** while the configuration it unlocks lives in a module. Claiming
a box is the same act as adopting an interface, one level up.

`--password` never takes a value on the command line (R2 in `cli.md` is about
identity being positional; this is the narrower rule that credentials are never
argv, where they land in shell history and `ps`).

---

## 8. Migration

**From v0.1.6.** No box has an `access` section, so every upgraded box is
unclaimed and its TCP API stops answering until somebody opens the UI and makes
one choice. That is a breaking change and worth naming as one. It is accepted
because the fleet is days old, the cost is a single screen, and the alternative
— treating "no access section" as "claimed with no password" — would silently
grandfather in exactly the state this document exists to eliminate, on every box
that ever ran v0.1.6.

**From `--auth` and the token file.** A box started with `--auth` has already
had a credential decision made for it, so it is treated as claimed with a
password, the token continuing to work until the operator sets a real one.
`--auth` then joins `--no-auth` as accepted-and-ignored: both describe a model
this replaces, and `OLRD_ARGS` is hand-edited, so neither may become a
start-time error (the v0.1.6 reasoning, unchanged).

**`/etc/open-linux-router/api-token`** is no longer generated. An existing one is
left on disk rather than deleted — removing a file an operator may have copied
into a script is not ours to do — and `olr access show` says it is no longer
consulted.

---

## 9. Port and transport

**`:8080` stays.** `ingress:§6` already commits to it twice over: as the API
listener, and as the *permanent recovery path* — "when Caddy is misconfigured,
its certificate has expired, or `:443` is held by something else,
`http://<router-ip>:8080` is the only remaining way in to fix it." A port that
is the documented way out of a broken ingress is not one to renumber for
tidiness, and it is already in `README.md`, `docs/install.md` and every operator's
notes.

The cost is real and should be stated: `:8080` is the most contended port on
Linux, and an install onto a box already running something there will fail to
bind. That is now a diagnosable failure rather than a mysterious one — the
preflight added in v0.1.4 names the holding process, its pid and its systemd
unit — and `olr listen <addr>:<port>` moves it. Refusing to serve the UI is not
an option the recovery-path argument leaves open.

**Plain HTTP, and the caveat.** A password typed into this page crosses the LAN
in cleartext. Self-signed TLS is the usual answer and it is worse for this
audience: a browser interstitial on first run, on the one screen where the
operator is deciding whether to trust the thing. Home Assistant makes the same
trade for the same reason. The path out is `ingress` publishing the UI through
Caddy with a real certificate (`ingress:§6` already allows it as an ordinary
entry), which is opt-in and needs nothing here. Until then: a password on this
listener protects against a curious housemate, not against somebody who can see
your packets, and the UI should not imply otherwise.

---

## 10. What this amends elsewhere

**§6.2** must stop saying the TCP listener is always authenticated and that an
unauthenticated one binds only loopback. The replacement invariant is §3's:
*TCP cannot configure the box until the box is claimed; what it requires after
that is the operator's recorded choice.* The paragraph's underlying instinct —
that an open admin API on a router's LAN is not a convenience — is preserved
exactly, by making the open state useless rather than by making it loopback-only.

**§7** says "Install alone changes nothing." It needs one clause, because
installing now serves a page: *install serves olr's own UI on olr's own port,
and changes nothing else — no interface, no other daemon, no shared port, no
route.* The promise that install cannot break your SSH is untouched; opening
`:8080` was never able to.

**§10 #1** is partly answered. `auth` does land in `system`, as its `access`
slice, and this document builds only that slice.

**`ingress:§6`** describes first boot as "`olr listen 0.0.0.0:8080` plus a
token, per `docs/install.md`". Both halves stop being true; the recovery-path
argument that paragraph exists for gets stronger, not weaker, since the listener
is now always there.

**`docs/install.md`** loses its step 2 entirely — the UI is already open — and
gains the one-choice screen.

---

## 11. Scope

**This is not a user model.** One box, one optional password, no accounts, no
roles, no sessions beyond a cookie, no SSO, no API keys per client. §10 #1 folds
"who may log into olr" into `system`, and this is the smallest slice of that
which makes a first install correct. It is deliberately shaped so a later user
model *replaces* it rather than having to stay compatible with it: there is no
username to migrate, no role to reinterpret, and the recorded choice degrades
cleanly into "the box's default policy" once real identities exist.

**Not included, deliberately:** password reset from the UI (it is `sudo olr
access set --password` on the box, which is the honest recovery path for a
device you have physical or SSH access to), rate limiting on the login form
(worth doing, belongs with the login form, not with this decision), and
remembering which browsers have logged in.

---

## 12. Open decisions

1. **The claim screen's default.** Which of the two options is preselected, or
   whether neither is. Preselecting "no password" makes the fast path one click
   and makes the choice easier to make without reading; preselecting the
   password makes the safe path the default and adds friction to the common
   case. Leaning toward neither preselected, both one click.

2. **`olr claim` interactive or not.** §7 gives it an interactive form, which is
   the only interactive prompt in the whole CLI. The alternative is to require
   `--password` or `--no-password` explicitly and keep the CLI promptless, at
   the cost of making the command less obvious to type.

3. **Whether `--code` (§4) ships in the first version** or waits until somebody
   asks. It is small, but it is a second path through first-run, and second
   paths are where first-run bugs live.

4. **Argon2id would be a new dependency, and not a small one in context.**
   `golang.org/x/crypto` is not in `go.mod` today — not even indirectly — so
   Argon2id means adding a module to a direct-dependency list the README calls
   "a deliberate constraint, not an accident", for one function.

   The stdlib alternative is `crypto/pbkdf2`, in since Go 1.24 and so available
   on our 1.27 floor. PBKDF2 is the weaker KDF against an attacker with the
   hash, and that comparison is doing less work here than it looks: this hash
   lives in a root-owned file on a single box, and anyone who can read it can
   also run `olr access set --password`, or edit `olr.json`, or restart olrd.
   Guessing the password is not how that attacker proceeds.

   Leaning stdlib PBKDF2 with a high iteration count, and recording the reason
   so the next person does not read it as an oversight. Argon2id becomes
   correct the moment this hash is ever readable by something that is not
   already root — a multi-user model, a backup that leaves the box, a config
   export — and that is the trigger to revisit, not a version number.
