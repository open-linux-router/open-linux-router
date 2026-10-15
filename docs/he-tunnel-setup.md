# Guided HE IPv6 setup

Gateway interfaces → **Set up HE IPv6** opens `/gateway/he-setup`.
The browser saves the current step and non-secret tunnel draft locally. Public
address checks and verification results are deliberately not persisted as proof
of current connectivity.

The guide reuses Overview's ip.sb public-address query and five-minute cache
(retained for thirty minutes when inactive), with a timestamp and manual refresh.
The lookup follows the router's default route; a different observed uplink route
is flagged before using the result as HE's endpoint. RFC 1918 private uplink
addresses and CGNAT shared addresses are classified immediately, independently
of the external lookup. Private addressing does not prove the upstream WAN is public.
The guide explains upstream
protocol 41 forwarding or DMZ, measures the built-in HE endpoints, links to HE
creation, previews and applies the tunnel, then guides LAN setup and client tests.
The 36-entry catalog is HE's creation-page list supplied on October 15, 2026;
Warsaw has no endpoint because it was full. Capacity remains subject to HE's page.

`GET /api/dial/he/servers` returns the catalog. Public address discovery uses
`GET /api/system/public-addresses`; no separate reflector service is needed.
`POST /api/dial/he/check` requires `target` and sends eight ICMP probes bound to the uplink for catalog IPv4
endpoints, or to `olr-6in4` for the configured HE IPv6 peer and two fixed public
IPv6 targets. Requests have a ten-second deadline and one check runs at a time.
Arbitrary targets are rejected. Checks do not change network configuration.

Recommendations rank responding servers by observed loss, average latency, then
RTT standard deviation. No reply means unknown, not unavailable. Measurements
never select or switch a server. IPv4 latency is only a preliminary comparison;
the configured tunnel must separately pass IPv6 round-trip checks.

Tunnel application uses the existing plan and revision-checked configuration
patch, changing only uplink IPv6. It can replace an existing IPv6 default route.
The guide reports partial application; automatic rollback is not provided.
LAN changes have separate previews for network addressing, SLAAC/DNS with the
tunnel MTU, and Gateway IPv6 forwarding. Each is a revision-checked patch;
completed actions are not rolled back if a later action fails. Enabling DHCP or
Gateway also activates their existing saved configuration, stated before apply.
The existing settings pages remain linked for advanced changes. The guide
can inspect saved settings but does not claim they prove runtime or client
connectivity. HE account creation, upstream configuration, client HTTPS/MTU tests
and endpoint updates after public IPv4 changes remain explicit manual steps.
