import type { AccessKind } from './catalog'

export const ACCESS_USES = ['All', 'Remote access', 'Networking', 'Internet exit', 'Publishing'] as const
export type AccessUse = Exclude<typeof ACCESS_USES[number], 'All'>

export interface AccessTechnology {
  id: string
  name: string
  description: string
  uses: AccessUse[]
  connections: AccessKind[]
  website?: string
}

// A technology appears once even when it serves several purposes. Only real
// OLR configuration owners belong in connections; documentation is not support.
export const ACCESS_TECHNOLOGIES: AccessTechnology[] = [
  { id: 'wireguard', name: 'WireGuard', description: 'Encrypted tunnels for remote access, site links and internet exits. OLR currently configures incoming device connections.', uses: ['Remote access', 'Networking', 'Internet exit'], connections: ['wireguard'] },
  { id: 'tailscale', name: 'Tailscale', description: 'Managed WireGuard networking with remote access, subnet routes and exit nodes.', uses: ['Remote access', 'Networking', 'Internet exit'], connections: [], website: 'https://tailscale.com/' },
  { id: 'netbird', name: 'NetBird', description: 'WireGuard networking with managed access, network routes and exit nodes; hosted or self-hosted.', uses: ['Remote access', 'Networking', 'Internet exit'], connections: [], website: 'https://netbird.io/' },
  { id: 'zerotier', name: 'ZeroTier', description: 'Virtual networks connecting devices and sites, with managed routes to other networks.', uses: ['Remote access', 'Networking'], connections: [], website: 'https://www.zerotier.com/' },
  { id: 'headscale', name: 'Headscale', description: 'A self-hosted control server for Tailscale clients, including subnet routes and exit nodes.', uses: ['Remote access', 'Networking', 'Internet exit'], connections: [], website: 'https://headscale.net/' },
  { id: 'nebula', name: 'Nebula', description: 'Certificate-based overlay networking for connecting devices and sites.', uses: ['Remote access', 'Networking'], connections: [], website: 'https://nebula.defined.net/' },
  { id: 'openvpn', name: 'OpenVPN', description: 'Client and site-to-site VPN tunnels for private network access or an internet exit.', uses: ['Remote access', 'Networking', 'Internet exit'], connections: [], website: 'https://openvpn.net/' },
  { id: 'ipsec', name: 'IPsec', description: 'IPsec / IKEv2 VPN connections for remote devices, site links and internet exits.', uses: ['Remote access', 'Networking', 'Internet exit'], connections: [], website: 'https://www.strongswan.org/' },
  { id: 'frp', name: 'frp', description: 'Publish web and TCP/UDP services through a remote server, including private service access with visitor clients.', uses: ['Remote access', 'Publishing'], connections: [], website: 'https://gofrp.org/' },
  { id: 'cloudflare', name: 'Cloudflare', description: 'Cloudflare Tunnel publishes services through outbound tunnels; Access can restrict who reaches them.', uses: ['Remote access', 'Publishing'], connections: [], website: 'https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/' },
  { id: 'rathole', name: 'rathole', description: 'Expose selected TCP/UDP services through a server using a reverse tunnel.', uses: ['Publishing'], connections: [], website: 'https://github.com/rathole-org/rathole' },
  { id: 'ssh', name: 'SSH', description: 'Forward selected ports through encrypted local, reverse or SOCKS tunnels.', uses: ['Remote access', 'Internet exit', 'Publishing'], connections: [], website: 'https://www.openssh.com/' },
  { id: 'shadowsocks', name: 'Shadowsocks', description: 'Encrypted proxy connections for internet access. OLR currently provides a server for remote clients.', uses: ['Internet exit'], connections: ['shadowsocks'] },
  { id: 'socks', name: 'SOCKS5', description: 'Use an external proxy as an exit, or let remote clients use this router as their proxy.', uses: ['Internet exit'], connections: ['socks-out', 'socks-in'] },
  { id: 'http', name: 'HTTP', description: 'Use an HTTP proxy, including CONNECT tunnels for HTTPS traffic. Proxy authentication and transport security depend on deployment.', uses: ['Internet exit'], connections: [], website: 'https://www.squid-cache.org/' },
  { id: 'trojan', name: 'Trojan', description: 'Proxy internet traffic through an encrypted TLS connection to a compatible server.', uses: ['Internet exit'], connections: [], website: 'https://trojan-gfw.github.io/trojan/' },
  { id: 'vless', name: 'VLESS', description: 'Proxy internet traffic using Xray; encryption depends on the configured transport and security settings.', uses: ['Internet exit'], connections: [], website: 'https://xtls.github.io/' },
  { id: 'vmess', name: 'VMess', description: 'Proxy internet traffic through an authenticated connection to a compatible V2Ray server.', uses: ['Internet exit'], connections: [], website: 'https://www.v2fly.org/' },
  { id: 'hysteria', name: 'Hysteria', description: 'Hysteria 2 proxies TCP/UDP traffic over QUIC through a compatible server.', uses: ['Internet exit'], connections: [], website: 'https://v2.hysteria.network/' },
  { id: 'tuic', name: 'TUIC', description: 'Proxy TCP/UDP traffic through a QUIC-based connection to a compatible server.', uses: ['Internet exit'], connections: [], website: 'https://github.com/tuic-protocol/tuic' },
  { id: 'gateway', name: 'Gateway', description: 'Route traffic through another gateway or an existing VPN interface.', uses: ['Networking', 'Internet exit'], connections: ['gateway'] },
  { id: 'tunnelbroker', name: '6in4', description: 'Obtain routed IPv6 connectivity through Hurricane Electric Tunnel Broker.', uses: ['Internet exit'], connections: ['tunnelbroker'] },
  { id: 'https', name: 'HTTPS', description: 'Publish a web service by domain name using the router’s HTTPS reverse proxy.', uses: ['Publishing'], connections: ['https'] },
  { id: 'nat', name: 'NAT', description: 'Forward an external TCP/UDP port to a service inside this network.', uses: ['Publishing'], connections: ['port-forward'] },
]
