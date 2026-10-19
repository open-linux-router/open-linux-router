# Access project artwork

Bundled locally so router pages do not request third-party images at runtime.
Brand marks identify their respective projects; they do not imply endorsement.

Artwork comes from [theSVG](https://thesvg.org) (MIT; `public/icons/<slug>/` in
GLINCKER/thesvg) whenever the library carries the project, so the launcher draws
from the same library as the OS marks and selectable service icons.

- WireGuard, Tailscale, NetBird, ZeroTier, Nebula: `<slug>/default.svg`, in the
  library's own colours. WireGuard's and Tailscale's files are byte-identical to
  the Simple Icons artwork the launcher used before — theSVG republishes it.
- Cloudflare: `cloudflare/color.svg` rather than `default.svg`. The default
  carries a white fill that disappears on the launcher's white tile.
- Headscale: `https://raw.githubusercontent.com/juanfont/headscale/main/docs/assets/logo/headscale3-dots.svg`
  (BSD-3-Clause project). theSVG has no entry for it.
- SSH keeps OpenSSH's own artwork: `https://www.openssh.com/images/openssh.gif`.
  theSVG's `ssh` entry is a dotted ring, not a shell mark.

Trademarks remain with their owners. Protocols without a mark — SOCKS5, HTTP,
HTTPS, Gateway, 6in4, NAT, frp, rathole — use existing Lucide SVG icons with
their protocol names below. Lucide is ISC licensed and supplied by the existing
`lucide-react` dependency; no launcher artwork is generated or drawn locally.
