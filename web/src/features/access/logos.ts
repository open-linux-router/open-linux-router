import shadowsocks from '@/assets/way-marks/shadowsocks.svg'

// Local copies of project artwork; provenance is in public/access-logos/README.md.
// Protocols without an established logo intentionally use their written name.
export const ACCESS_LOGOS: Record<string, string> = {
  wireguard: '/access-logos/wireguard.svg',
  tailscale: '/access-logos/tailscale.svg',
  headscale: '/access-logos/headscale.svg',
  netbird: '/access-logos/netbird.svg',
  zerotier: '/access-logos/zerotier.svg',
  nebula: '/access-logos/nebula.svg',
  openvpn: '/access-logos/openvpn.svg',
  ipsec: '/access-logos/ipsec.png',
  cloudflare: '/access-logos/cloudflare.svg',
  vless: '/access-logos/vless.svg',
  vmess: '/access-logos/vmess.png',
  hysteria: '/access-logos/hysteria.svg',
  shadowsocks,
  ssh: '/access-logos/ssh.gif',
}
