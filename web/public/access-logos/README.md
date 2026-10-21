# Access project artwork

Bundled locally so router pages do not request third-party images at runtime.
Brand marks identify their respective projects; they do not imply endorsement.

Artwork comes from [theSVG](https://thesvg.org) (MIT; `public/icons/<slug>/` in
GLINCKER/thesvg) whenever the library carries the project, so the launcher draws
from the same library as the OS marks and selectable service icons.

- WireGuard, Tailscale, NetBird, ZeroTier: `<slug>/default.svg`, in the
  library's own colours. WireGuard's and Tailscale's files are byte-identical to
  the Simple Icons artwork the launcher used before — theSVG republishes it.
- Cloudflare: `cloudflare/color.svg` rather than `default.svg`. The default
  carries a white fill that disappears on the launcher's white tile.
- Tunnel Broker: `hurricane-electric-badge/default.svg`, Hurricane Electric's
  own badge, since the tunnel broker service is theirs.
- Headscale: `https://raw.githubusercontent.com/juanfont/headscale/main/docs/assets/logo/headscale3-dots.svg`
  (BSD-3-Clause project). theSVG has no entry for it.
- frp: the 196×196 inline SVG from `https://gofrp.org`'s navigation, the blue
  badge with the white *f*. The site's 16×16 and 32×32 favicons draw the same
  mark; its 180×180 and 192×192 PNG favicons still carry the older document
  icon the launcher used before.

Trademarks remain with their owners. SOCKS5 and Port forwarding use approved
original artwork documented in [../feature-icons/README.md](../feature-icons/README.md).
Gateway and SSH retain their initials on tinted tiles. theSVG's `ssh` entry is
a dotted ring rather than a shell mark, so it is not used.
