# Serving DHCP for a network you already have

The shortest useful thing olr can do: leave the router you already own doing the
routing, and move **DHCP** onto a Linux box. You get a device list worth reading,
fixed addresses that are easy to edit, and a configuration file you can open.
Nothing about how traffic reaches the internet changes.

This is written for that arrangement specifically, because it is the one with a
trap in it (§3, below). If the box *is* your gateway, everything here still
applies except the parts about pointing clients at the old router.

---

## What you need

- A Linux box on the network, with systemd and a **static address**. olr does
  not configure interface addresses yet — that is the unbuilt half of the `link`
  module — so set it with whatever your distribution uses (`systemd-networkd`,
  NetworkManager, `/etc/network/interfaces`). Give it an address outside the
  range you are about to hand out.
- Access to your existing router's settings, to turn its DHCP server off.
- Ten minutes, and preferably not while anybody is on a call.

Throughout, the example network is `192.168.1.0/24`, the existing router is
`192.168.1.1`, and the olr box is `192.168.1.2` on interface `enp1s0`.

---

## 1. Install

```sh
sudo apt install ./olr_<version>_<arch>.deb
```

This starts `olrd` and changes nothing else — no DHCP server, no resolver, no
firewall rule. Check it came up:

```sh
olr status
```

## 2. Open the web UI

```sh
sudo olr listen 0.0.0.0:8080
sudo cat /etc/open-linux-router/api-token
```

Browse to `http://192.168.1.2:8080` and paste the token when asked. The rest of
this document gives both the UI and the CLI; they are the same API.

## 3. Hand the interface to olr

**Web UI:** DHCP → Interfaces → switch on `enp1s0`.

**CLI:**

```sh
olr link show interfaces
sudo olr adopt enp1s0
```

olr refuses to serve anything on an interface nobody gave it, so without this
every range you try to add is rejected. Adopting sets no address and starts no
service; it grants permission.

## 4. Add the address range — and read this part twice

**Web UI:** DHCP → Address ranges → Add. Choosing the interface fills in a range
inside its subnet.

**CLI:**

```sh
sudo olr dhcp add pool enp1s0 \
  --range 192.168.1.100-192.168.1.200 \
  --gateway 192.168.1.1 \
  --dns 192.168.1.1
```

`--gateway` and `--dns` are the trap. Left unset they both mean *this router*,
which is right when the olr box is your gateway and **wrong here** — your
existing router still carries the traffic and answers the names. Set to blank in
this arrangement, every device would be handed a default route to a machine that
is not routing and a resolver that is not answering, and the symptom is "the new
router broke the internet".

Point them at whatever does that job today. Usually that is the old router,
`192.168.1.1`. If you would rather hand out a public resolver, `--dns 1.1.1.1`
is equally fine — what matters is that it is something that answers.

Then turn the server on:

```sh
sudo olr dhcp enable
olr dhcp status
```

## 5. Turn off the old DHCP server

Now, and not before. Find it in your existing router's admin page — usually
LAN → DHCP Server → off.

**Two DHCP servers on one network is a race, and olr cannot see the other one.**
The preflight check only looks at UDP/67 on *this* box; a second server in a
plastic box across the room is invisible to it. Whichever answers a client first
wins, so leaving both on gives you a network that works and is confusing, which
is worse than one that plainly does not.

## 6. Watch devices arrive

Devices do not move over immediately. They keep the address the old server gave
them until that lease expires — often 12 or 24 hours — and only then ask again.
To hurry a device along, turn its Wi-Fi off and on.

**Web UI:** the Overview page. **CLI:**

```sh
olr dhcp show leases
```

## 7. Pin the things that should not move

Printers, NAS boxes, anything you reach by address:

**Web UI:** Overview → click the device → give it a fixed address.

**CLI:**

```sh
sudo olr dhcp add reservation aa:bb:cc:dd:ee:ff --ip 192.168.1.50
```

Reservations are applied by reloading dnsmasq rather than restarting it, so
adding one does not interrupt anybody.

---

## What the device list can and cannot see

It is a join of three sources, and knowing which is which saves an argument with
the screen:

- **DHCP leases.** Everything that asked this box for an address. Complete for
  anything using DHCP, and blind to anything that does not.
- **The kernel's neighbour table** (`/proc/net/arp`), which catches the
  statically-addressed printer that has never taken a lease. It only knows
  devices *this box* has recently exchanged packets with — and in this
  arrangement traffic goes through your old router, not through here, so it will
  be sparse. Expect the list to be mostly lease-derived.
- **Your reservations**, so a device you have configured appears even when it is
  switched off.

Two known gaps, both stated rather than hidden: `/proc/net/arp` is IPv4-only, and
a DHCPv6 lease often carries no hardware address to join on — so a v6-only client
is invisible to both sources. A device that neither takes a lease nor talks to
this box has to be added by hand.

## Where things live

| | |
|---|---|
| `/etc/open-linux-router/olr.json` | everything you configured, one file |
| `/etc/open-linux-router/olrd.env` | whether the web UI listens, and on what |
| `/etc/open-linux-router/api-token` | the web UI's token |
| `/etc/open-linux-router/rendered/` | what olr generated for dnsmasq — never edit |
| `/var/lib/open-linux-router/dhcp/` | the lease database |
| `journalctl -u olrd -u olr-dhcp` | logs; olr keeps no log files of its own |

The document in `olr.json` is meant to be read. If you ever need to recover a
box, SSH in and open it — that is the point of intent being a plain file.

## Undoing it

```sh
sudo olr dhcp disable        # stop serving, keep the configuration
sudo olr release enp1s0      # take the interface back
sudo apt remove olr          # or remove it entirely
```

Turn your old router's DHCP server back on before or shortly after, or nothing
new joining the network will get an address.

## When something is wrong

**Nothing gets an address.** Check the range is inside the interface's subnet and
that the interface is up with a cable in it — `olr link show interfaces` says
both. Then `olr dhcp status`, then `journalctl -u olr-dhcp -n 50`.

**olr refuses to start the server.** Something already holds UDP/67 on this box,
and the refusal names it — process, pid and systemd unit — along with the
command that stands it down. olr runs its own dnsmasq instance and will not stop
a daemon it did not start. The usual culprit is your distribution's own dnsmasq:
`apt install dnsmasq` installs a service alongside the binary, where
`dnsmasq-base` is just the binary olr drives.

**A range is refused as "not adopted".** Step 3.

**Devices get an address but no internet.** Step 4 — the gateway is almost
certainly pointing at this box instead of your real router. `olr dhcp show pool
enp1s0` shows what is being advertised.

**The web UI cannot be reached.** `olr listen` with no listener set is off
by default; re-run step 2. If it is set, check nothing between you and the box is
filtering the port.
