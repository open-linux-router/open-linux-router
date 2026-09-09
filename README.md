# open-linux-router

Router software for personal, homelab and small-org networks, installed as a
package on an ordinary Linux box rather than flashed as a system image.

olr does not reimplement DHCP, DNS or packet filtering. It renders configuration
for the daemons a router is already made of — dnsmasq, unbound, nftables —
drives them through systemd, and puts one schema behind a CLI, a REST API, an
MCP server and a web UI. The box stays a normal Linux machine: nothing outside
olr's declared scope is touched, and `apt remove` gives it back.

**Status: early.** DHCP, DNS and routing are built; interface adoption is
minimal (see below); firewall, NAT and Wi-Fi are not written yet. If you want a
finished router today, this is not one — but a box serving DHCP for a household
works, and that is the path documented in
[docs/install.md](docs/install.md).

---

## Install

Debian 13 (trixie) and Ubuntu are the tested targets; anything with systemd and
nftables should work.

```sh
sudo apt install ./olr_<version>_<arch>.deb
```

apt resolves `dnsmasq-base`, `unbound` and `nftables` before any of olr's code
runs. For distributions the `.deb` does not cover there is a tarball with an
`install.sh` that checks the same things by hand.

Installing starts `olrd`, the control plane, **and changes nothing else**. No
DHCP server appears, no resolver takes over port 53, no interface is
reconfigured. That is deliberate: installing a router's control plane on a box
you reach over SSH must not be able to disconnect you from it.

## Open the web UI

`olrd` serves its control socket at `/run/olr/olrd.sock` — which is what the
`olr` command talks to — and does not listen on the network until told to:

```sh
sudo olr daemon listen 0.0.0.0:8080
```

Then browse to `http://<this box>:8080`. It will ask for a token, which `olrd`
generated on first start:

```sh
sudo cat /etc/open-linux-router/api-token
```

To close it again, `sudo olr daemon listen --off`. For a box you would rather
not expose at all, `olr daemon listen 127.0.0.1:8080` and reach it over an SSH
tunnel — loopback needs no token.

## Or stay on the command line

Everything the UI does goes through the same HTTP API, so the CLI is not a
lesser surface:

```sh
olr link show interfaces        # what this machine has
sudo olr adopt enp1s0           # hand one to olr
olr dhcp add pool enp1s0 --range 192.168.1.100-192.168.1.200
olr dhcp enable
olr dhcp show leases            # who took an address
```

`olr adopt` is the step people miss. olr refuses to serve anything on an
interface nobody handed it, so every module will reject an interface until it
has been adopted. Adopting by itself does nothing to the machine — it sets no
address and starts no service. It grants permission.

## Serving DHCP for an existing network

The most useful thing this can do today: leave your existing router in place
doing the routing, and move DHCP onto a Linux box so you get a real device list,
fixed addresses that are easy to edit, and a config file you can read.

[docs/install.md](docs/install.md) walks through it, including the two things
that go wrong — pointing the gateway and DNS at the right box, and the overlap
while both DHCP servers are running.

## Documentation

| | |
|---|---|
| [docs/install.md](docs/install.md) | Getting a box serving DHCP for a real network |
| [docs/cli.md](docs/cli.md) | `olr` command conventions, enforced by tests |
| [docs/dns.md](docs/dns.md) | What the DNS module does and refuses to do |
| [docs/gateway.md](docs/gateway.md) | Exits, and which networks use them |
| [docs/mcp.md](docs/mcp.md) | The agent surface |
| [design.md](design.md) | Architecture, and the decisions behind it |

## Building

```sh
make all      # SPA + binaries
make check    # vet + tests
make package  # .deb for amd64 and arm64
```

Go builds without Node installed — a binary built that way serves an
explanatory page instead of the UI. `make web` needs Node 24.

## Licence

MIT.
