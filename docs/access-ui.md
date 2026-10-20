# Access UI

Access shows three square-tile grids: **Networking**, **Add a way out** and
**Add a way in**. There is no System default row or purpose filter. A technology
can appear in both directional grids; each tile is a link to the configuration
owner for that direction, so there is no intermediate dialog. Hover/focus
explains functions and observed state, and the tile's accessible name carries
that state for touch and screen readers. A technology OLR does not configure
links to the project's own documentation in a new tab.

Tiles use bundled project artwork from `web/public/access-logos`, taken from
[theSVG](https://thesvg.org) wherever the library carries the project, so the
launcher matches the marks used elsewhere in the interface. Headscale is the one
mark outside the library, from its own repository. The rest are protocols rather
than projects: SOCKS5, Gateway, NAT, frp and SSH wear a capital initial on a
tinted tile, which keeps two entries sharing an initial (SSH and SOCKS5) apart by
hue instead of by borrowed branding. See the logo directory README for
provenance.

## Connection model

`web/src/features/access/catalog.ts` defines direction, kind, name, purpose and
canonical configuration URL. A way is a connection capability, not a promise
that all connections can be selected in every context. Protocol does not decide
direction: SOCKS5 appears in both directions with different configuration owners.

| Direction | Kind | Configuration owner | Canonical URL |
| --- | --- | --- | --- |
| Out | Gateway or existing VPN interface | gateway exits | `/access/gateway` |
| Out | External SOCKS5 | socksout | `/access/socks5` |
| Out | Tunnel Broker / HE | dial.tunnelbroker | `/access/tunnelbroker` |
| In | WireGuard | remote.wireguard and remote endpoint | `/access/wireguard` |
| In | SOCKS5 server | remote SOCKS5 | `/access/socks-server` |
| In | Port forwarding | gateway forwards | `/access/forwards` |

System default is a built-in way out, displayed separately with observed IPv4
and IPv6 routes. It cannot be added or deleted. No default-for-networks setting
is introduced. Networks, groups and devices retain their existing inheritance
and route selection; Access owns discovery and connection configuration.
Blocking is a routing policy rather than an addable connection.

Each connection keeps one configuration owner and its current lifecycle. A proxy
or HE tunnel's generated interface is not another connection to create. HE keeps
its routed prefixes, activation and LAN assignment; its source routing still
precedes gateway policy, and native IPv6 retains its route. No generic selector,
automatic prefix migration, new failover or dependency chaining is introduced.
Tunnel/proxy establishment depends on an underlying path; future configurable
dependencies must reject cycles and keep transport traffic out of its own tunnel.

Ways in express either joining the network, borrowing its internet access or
publishing a service. Those intents share a directory, not permissions or a
universal configuration form. Credentials and exposure remain in their own
workflows. The directory references each technology once, with a separate tile
per direction where it has multiple configuration owners. The HTTPS ingress view
(`/access/ingress`), which publishes a service by domain name, is reached from a
device's service action rather than from the directory.

Former Gateway URLs and Advanced addresses redirect while preserving query
parameters and fragments. There is no combined remote access page: the old
`/access/remote` address redirects to `/access/wireguard`, and WireGuard and
SOCKS5 each keep their own view. Directory tiles link to individual views of the
same configuration, with independent status summaries.

The landing page reads existing configuration and status endpoints. It never
applies network changes. A failed observation is shown as unavailable rather than
off, and saved intent with a stopped service is shown as needing attention. The
outbound SOCKS5 summary describes the local TUN service, not the reachability of
the external proxy. Protocol settings and configuration apply paths remain on
their respective pages.

## Validation

Direct-navigation revision: removing Nebula, HTTP and HTTPS leaves five
Networking tiles, four ways out and five ways in. Node 24 lint, production build,
all 42 UI tests and the interface-rate checks pass. A headless browser against a
scratch daemon verifies every tile is an anchor, that configuration tiles route
in place (Gateway, SOCKS5 and NAT open `/access/gateway`, `/access/socks5` and
`/access/forwards`), that no dialog element exists, that unintegrated tiles keep
`target="_blank"` with `rel="noopener noreferrer"`, and that nothing overflows at
375 pixels. Screenshots at 1440 and 375 pixels in light and dark themes:
`/tmp/access-{light,dark}-{1440,375}.png`. No live router settings changed.

Protocol removal: Go 1.27 Linux vet and all tests pass. Node 24 lint (existing
warnings only), all 29 UI tests, interface-rate checks and production build pass.
Browser checks verify removed technologies are absent, retained technologies
and SOCKS5 configuration links remain, bundled logos load and no page overflows
at 375, 768 and 1440 pixels in light and dark themes. Screenshots are in
`/tmp/olr-protocol-removal-review`. No live router configuration was changed.

Direction/logo revision: Node 24 lint, production build, all UI tests and Linux
Go 1.27 vet/tests pass. Browser checks at 375, 768 and 1440 pixels in both themes
verify the two headings, removed default/filter controls, square tiles, loaded
local logos, hover/focus explanations, keyboard dialogs and configuration links.
Screenshots: `/tmp/olr-access-logos-review`. No live router settings changed.

Previous directory revision:

Technology-directory checks cover all 24 tiles at 375, 768 and 1440 pixels in
light and dark themes, square proportions, horizontal overflow, hover, keyboard
activation, detail dialogs, purpose filtering, both SOCKS5 actions and external
documentation links. Screenshots are retained in
`/tmp/olr-access-directory-review`. Node 24 lint, production build and all 25 UI
tests plus interface-rate checks pass; lint retains existing unrelated warnings.
Linux Go vet and tests pass with Go 1.27. These checks use synthetic responses
and do not change live router settings.

Earlier connection/status refactor validation:

UI lint, production build and all UI tests pass with Node 24, including ten
Access status regression tests. Linux Go vet and tests pass with Go 1.27 (the
commands behind `make check` were run directly because make was unavailable).
Browser checks for the tile refactor use synthetic API responses at 375, 768
and 1440 pixel widths in light/dark themes, keyboard focus, overflow checks,
individual configuration pages, the two Gateway redirects with query and hash
preservation, and read-failure state. Screenshots are captured for review.
No live router settings were changed.

## Illustration

`web/src/assets/access-connections.webp` is a decorative, transparent 256 × 256
WebP (8,690 bytes), displayed at 64–96 CSS pixels with empty alternative text.
It was generated using the built-in MyLifeDB `generate_image` tool with
`gpt-image-2`, medium quality and a square size request, then resized with Pillow
and encoded at quality 88. Its alpha channel is preserved. It is a page
illustration, not a replacement for the application's logo.

Final generation prompt:

```text
Use case: stylized-concept
Asset type: compact decorative illustration for Open Linux Router's Access settings page, displayed at 96px beside a short introduction, not a hero banner or replacement brand logo.
Primary request: a refined physical sculpture suggesting private connections into and out of a home network: two interlocking open rounded loops, one satin porcelain silver and one restrained system blue, with two tiny connection nodes.
Scene/backdrop: genuinely transparent background.
Style/medium: premium minimalist 3D product illustration, precise smooth curves, matte ceramic, soft studio lighting, subtle contact shadows, crisp silhouette.
Composition/framing: centered small balanced sculpture occupying 80 percent of square frame, readable at small scale. Quiet and functional, suitable on both white and charcoal UI backgrounds.
Color palette: porcelain gray and muted medium blue matching a neutral system-style control panel.
Constraints: transparent alpha; no text, no letters, no watermark, no gradients in backdrop, no frame, no glow, no additional objects or wires.
```
