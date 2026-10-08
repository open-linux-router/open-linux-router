# Routed IPTV multicast

OLR can proxy IPv4 IGMP subscriptions from named LAN networks to a separate,
operator-provided IPTV upstream interface. It does not create VLAN interfaces,
bridge Ethernet, relay the provider's DHCP, perform STB authentication, or
proxy IPv6 MLD. The STB remains a normal client of the LAN. This works only
when the provider accepts routed multicast from a proxy and the STB can obtain
its service information without sharing the modem's L2 segment.

Install `igmpproxy` on the router and ensure its distribution unit is not
running; OLR runs a separate `olr-iptv.service` with a dedicated rendered file.
The package intentionally does not depend on the distribution's daemon package:
installing it may start a second, unconfigured service. Create the IPTV
interface (or tagged VLAN) with your host network manager, `olr adopt <iface>`,
and ensure it has an IPv4 address and a route toward the provider multicast
source. OLR does not assign addresses on the IPTV upstream. IPv4 forwarding must be
enabled (normally by the OLR gateway module).

```sh
olr iptv set --upstream iptv0 --network lan --dry-run
olr iptv set --upstream iptv0 --network lan
olr iptv status
olr iptv disable
```

If the multicast source addresses are outside the upstream interface's
connected prefix, repeat `--source 10.0.0.0/8` (with the actual provider
prefix). The source list is passed as `altnet` to igmpproxy. OLR admits
only IPv4 multicast from the chosen upstream to selected LAN interfaces
when its firewall is enabled. No viewer means no joined stream, but a
switch/AP without IGMP snooping may flood an active stream across its ports
or wireless airtime. Test AP latency and throughput while watching TV.

Diagnostics: `olr iptv status`, `journalctl -u olr-iptv`, and `ip mroute`.
Check for downstream IGMP membership, an upstream multicast source, then a
kernel multicast route. A working proxy does not guarantee provider-specific
DHCP, portal, or authentication succeeds. Dynamic changes to the underlying
interface or LAN membership may require `olr iptv set` again to restart the
proxy. Do not bridge the IPTV interface into the household LAN.
