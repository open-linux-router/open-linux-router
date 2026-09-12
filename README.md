# open-linux-router

Router software for personal, homelab and small-org networks — installed as a
package on an ordinary Linux box, not flashed as a system image.

`apt install`, and the Debian machine you already have becomes a router with a
web UI, a CLI, a REST API and an MCP server, all speaking to the same
configuration. It stays a normal Linux machine the whole time.

**Status: early.** DHCP, DNS, devices and the gateway are built. Interface
handling is deliberately minimal so far — olr adopts the NICs you give it, but
does not yet create bridges, VLANs or addresses. The firewall module holds
**port forwarding and nothing else** — no zones, no rules, no filtering policy —
so olr decides what is redirected *into* your network and not what is allowed
through this box. Wi-Fi is not written at all. If you want a finished router
today, this is not one — but a box serving DHCP and DNS for a household works,
and that is the path documented in [docs/install.md](docs/install.md).

---

## What it believes

**A package, not an operating system.** No custom ISO, no reformatting, no
image to flash. You keep your distro, your packages, your SSH keys — and
`apt remove` gives the box back.

**Drive the daemons; don't reimplement them.** dnsmasq, unbound and nftables
have decades of correctness in them that nobody should rewrite. olr renders
their configuration, supervises them through systemd, and puts one schema in
front. It is a control plane, not a network stack.

**Nothing happens until you say so.** Installing olr starts a control plane and
changes *nothing else* — no DHCP server appears, no resolver takes port 53, no
interface is touched. You hand olr an interface explicitly (`olr adopt`) before
any module will serve on it. Installing a router's control plane on a box you
reach over SSH must never be able to disconnect you from it.

**Hide complexity, never capability.** The default surface speaks your
vocabulary — networks, devices, fixed addresses — not the daemon's. But every
module also has a declared escape hatch that passes raw backend config through,
versioned and single-source like everything else. We make the common 95%
pleasant. We do not hide Linux.

**One schema, four surfaces.** Each module's config is a Go struct; the JSON
Schema derived from it generates the CLI flags, the REST body, the web form and
the MCP tool definition. The web UI is not privileged — it is one client of the
same HTTP API the CLI and your agent use.

**Never store a fact twice.** olr keeps your *intent*. The kernel keeps the
interfaces, dnsmasq keeps the leases, nftables keeps the rules. Nothing is
cached, so nothing can drift.

**Linux is the extension surface.** Need something we don't ship? You don't
write an olr plugin — you `apt install` it, drop in a systemd unit, or add your
own nftables table. The distro is a better plugin system than any API we could
design, and our job is to stay out of its way.

## What's underneath

Five modules ship today. Each one owns a slice of configuration and hands the
actual work to something that already does it well:

| Module | What you configure | What actually does the work |
|---|---|---|
| **`link`** | Which interfaces olr is allowed to touch | Kernel **netlink**, read live per request. Drives nothing — adoption is consent, not configuration. |
| **`dhcp`** | Pools, fixed addresses, options, leases | **dnsmasq**, in a unit of its own (`olr-dhcp.service`) reading a config olr renders. Never the distro's instance. |
| **`dns`** | Upstreams, local names, blocking policies | **unbound** recursing on loopback, behind a small relay of ours (`olr-dnsd.service`) that owns `:53`, applies policy on the fast path, and observes on a tee. |
| **`devices`** | Device names, categories, the inventory | No daemon. dnsmasq's lease database joined with the kernel's **ARP table**, so the statically-addressed printer shows up too. |
| **`gateway`** | Exits, and which network uses which | **nftables** and the kernel's **policy routing database**, programmed directly over netlink — no rule files, no `nft` shell-outs. |
| **`firewall`** | Port forwards, and nothing else yet | **nftables**, one `olr_nat` table over netlink. Named for what §4 gives it eventually; today it has no zones, rules or filtering policy. |

All of it is one binary. `olr` is the command you type, the control plane
systemd runs, and the DNS relay behind port 53 — separate units and separate
sandboxes, one executable. Under it sits the Go standard library plus a short
list of direct dependencies: [`google/nftables`](https://github.com/google/nftables)
and [`vishvananda/netlink`](https://github.com/vishvananda/netlink) for the
kernel, [`coreos/go-systemd`](https://github.com/coreos/go-systemd) for
supervision over D-Bus, [`spf13/cobra`](https://github.com/spf13/cobra) for the
CLI, and [`invopop/jsonschema`](https://github.com/invopop/jsonschema) for the
schema reflection everything else is generated from. Keeping that list short is
a deliberate constraint, not an accident. There is no database and no message
bus — configuration is one JSON file. `olrd` spawns no subprocesses at all,
which is what makes its systemd sandbox nearly free.

Not written yet: firewall *filtering* — zones and rules, the other half of the
firewall module — Wi-Fi (hostapd), VPN (WireGuard), QoS (tc), WAN dialling
(pppd/dhcpcd).

## Getting started

Debian 13 (trixie) and Ubuntu are the tested targets; anything with systemd and
nftables should work.

```sh
sudo apt install ./olr_<version>_<arch>.deb
```

apt resolves `dnsmasq-base`, `unbound` and `nftables` before any of olr's code
runs. This starts the control plane and touches nothing else on the machine.

For anything the `.deb` doesn't cover, the tarball holds a single binary that
installs itself. There is no package manager involved here to resolve the
backends, so install them first — on Debian and Ubuntu:

```sh
sudo apt install dnsmasq-base unbound nftables
tar xzf olr-<version>-linux-<arch>.tar.gz
sudo ./olr enable
```

`dnsmasq-base`, not `dnsmasq`: the full package also ships a system dnsmasq
service that binds `:53` as soon as it's installed, and olr runs its own
instance rather than taking somebody else's daemon over. If you already have
the full package, `olr enable` will say so and tell you how to stand it down.

`olr enable` writes the systemd units, puts the binary in `/usr/local/bin`,
corrects the units' paths if your distribution doesn't keep dnsmasq where
Debian does, and starts the service. It prints every file it writes, and
`--dry-run` shows the list without touching anything. `olr disable` undoes the
boot entry and leaves the files in place.

Hand it an interface, then give that interface a job:

```sh
olr link show interfaces                                          # what this box has
sudo olr adopt enp1s0                                             # grant permission
sudo olr dhcp add pool enp1s0 --range 192.168.1.100-192.168.1.200
sudo olr dhcp enable
olr dhcp show leases                                              # who took an address
```

`olr adopt` is the step people miss. Every module refuses to serve on an
interface nobody handed it. Adopting sets no address and starts no service — it
only grants permission.

For the web UI, tell olr to listen. By default it answers only on its control
socket at `/run/olr/olrd.sock`, which is what the `olr` command talks to:

```sh
sudo olr listen 0.0.0.0:8080
```

Then browse to `http://<this box>:8080` and you're in — **there is no password.**
Opening the listener is the decision; having made it, anyone who can reach that
address can configure this router. That's the right default on a home network
and the wrong one on a network you don't control, so `sudo olr listen --off`
closes it again, and `127.0.0.1:8080` plus an SSH tunnel keeps it to people who
can already log into the box.

To require a token instead, add `--auth` to `OLRD_ARGS` in
`/etc/open-linux-router/olrd.env`. olrd generates one on first start, you read
it with `sudo cat /etc/open-linux-router/api-token`, and the UI asks for it.
A real login — users, not a shared secret — belongs to the unbuilt `system`
module.

**The most useful thing this can do today:** leave your existing router in place
doing the routing, and move DHCP onto a Linux box — so you get a real device
list, fixed addresses that are easy to edit, and a config file you can read.
[docs/install.md](docs/install.md) walks through it, including the two things
that go wrong.

---

| Docs | |
|---|---|
| [docs/install.md](docs/install.md) | Getting a box serving DHCP for a real network |
| [docs/cli.md](docs/cli.md) | `olr` command conventions, enforced by tests |
| [docs/dns.md](docs/dns.md) | What the DNS module does, and refuses to do |
| [docs/gateway.md](docs/gateway.md) | Exits, and which networks use them |
| [docs/firewall.md](docs/firewall.md) | Port forwarding, and why there is no filtering yet |
| [docs/mcp.md](docs/mcp.md) | The agent surface |
| [design.md](design.md) | Architecture, and the decisions behind it |

Building: `make all` (SPA + binary), `make check` (vet + tests), `make package`
(`.deb` for amd64 and arm64). Go builds without Node — a binary built that way
serves an explanatory page instead of the UI. `make web` needs Node 24.

MIT licensed.
