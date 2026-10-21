
// Local copies of project artwork, taken from theSVG (thesvg.org) wherever the
// library carries the project; provenance is in public/access-logos/README.md.
// frp uses its official website's mark. Unbranded features can use the approved
// app artwork below; other entries retain the launcher's monogram fallback.
export const ACCESS_LOGOS: Record<string, string> = {
  wireguard: '/access-logos/wireguard.svg',
  tailscale: '/access-logos/tailscale.svg',
  headscale: '/access-logos/headscale.svg',
  netbird: '/access-logos/netbird.svg',
  zerotier: '/access-logos/zerotier.svg',
  cloudflare: '/access-logos/cloudflare.svg',
  tunnelbroker: '/access-logos/tunnelbroker.svg',
  frp: '/access-logos/frp.svg',
}

// Original feature artwork brings its own backing, unlike upstream logos.
export const ACCESS_ARTWORK: Record<string, string> = {
  socks: '/feature-icons/socks5.webp',
}
