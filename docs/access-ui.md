# Access UI

Access shows two square-tile grids: **Add a way out** and **Add a way in**.
There is no System default row or purpose filter. A technology can appear in both
grids; dialogs only expose configuration actions owned by that direction.
Hover/focus explains functions and observed state; click exposes the same details
on touch devices. Unintegrated directions link to external documentation.

Tiles use bundled project artwork from `web/public/access-logos` and the existing
Shadowsocks mark. Protocols without an established logo use text rather than
invented symbols. See the logo directory README for provenance.

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
| In | Shadowsocks | remote.shadowsocks and remote endpoint | `/access/shadowsocks` |
| In | SOCKS5 server | remote SOCKS5 | `/access/socks-server` |
| In | HTTPS service | ingress | `/access/ingress` |
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
workflows. The directory references each technology once, with separate actions
where it has multiple configuration owners.

Former Gateway URLs and Advanced addresses redirect while preserving query
parameters and fragments. `/access/remote` retains its combined view for existing
bookmarks and shared endpoint settings. Directory dialogs link to individual
views of the same configuration, with independent status summaries.

The landing page reads existing configuration and status endpoints. It never
applies network changes. A failed observation is shown as unavailable rather than
off, and saved intent with a stopped service is shown as needing attention. The
outbound SOCKS5 summary describes the local TUN service, not the reachability of
the external proxy. Protocol settings and configuration apply paths remain on
their respective pages.

## Validation

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
