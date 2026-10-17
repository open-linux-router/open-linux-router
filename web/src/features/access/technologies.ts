import type { AccessKind } from './catalog'


export interface AccessTechnology {
  id: string
  name: string
  description: string
  directions: ('in' | 'out')[]
  connections: AccessKind[]
  website?: string
}

// A technology can appear in both direction groups. Only real OLR configuration
// owners belong in connections; documentation is not integrated support.
export const ACCESS_TECHNOLOGIES: AccessTechnology[] = [
  { id: 'wireguard', name: 'WireGuard', description: 'Encrypted tunnels for remote access, site links and internet exits. OLR currently configures incoming device connections.', directions: ['out', 'in'], connections: ['wireguard'], website: 'https://www.wireguard.com/' },
  { id: 'tailscale', name: 'Tailscale', description: 'Managed WireGuard networking with remote access, subnet routes and exit nodes.', directions: ['out', 'in'], connections: [], website: 'https://tailscale.com/' },
  { id: 'netbird', name: 'NetBird', description: 'WireGuard networking with managed access, network routes and exit nodes; hosted or self-hosted.', directions: ['out', 'in'], connections: [], website: 'https://netbird.io/' },
  { id: 'zerotier', name: 'ZeroTier', description: 'Virtual networks connecting devices and sites, with managed routes to other networks.', directions: ['out', 'in'], connections: [], website: 'https://www.zerotier.com/' },
  { id: 'headscale', name: 'Headscale', description: 'A self-hosted control server for Tailscale clients, including subnet routes and exit nodes.', directions: ['out', 'in'], connections: [], website: 'https://headscale.net/' },
  { id: 'nebula', name: 'Nebula', description: 'Certificate-based overlay networking for connecting devices and sites.', directions: ['out', 'in'], connections: [], website: 'https://nebula.defined.net/' },
  { id: 'openvpn', name: 'OpenVPN', description: 'Client and site-to-site VPN tunnels for private network access or an internet exit.', directions: ['out', 'in'], connections: [], website: 'https://openvpn.net/' },
  { id: 'ipsec', name: 'IPsec', description: 'IPsec / IKEv2 VPN connections for remote devices, site links and internet exits.', directions: ['out', 'in'], connections: [], website: 'https://www.strongswan.org/' },
  { id: 'frp', name: 'frp', description: 'Publish web and TCP/UDP services through a remote server, including private service access with visitor clients.', directions: ['in'], connections: [], website: 'https://gofrp.org/' },
  { id: 'cloudflare', name: 'Cloudflare', description: 'Cloudflare Tunnel publishes services through outbound tunnels; Access can restrict who reaches them.', directions: ['in'], connections: [], website: 'https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/' },
  { id: 'rathole', name: 'rathole', description: 'Expose selected TCP/UDP services through a server using a reverse tunnel.', directions: ['in'], connections: [], website: 'https://github.com/rathole-org/rathole' },
  { id: 'ssh', name: 'SSH', description: 'Forward selected ports through encrypted local, reverse or SOCKS tunnels.', directions: ['out', 'in'], connections: [], website: 'https://www.openssh.com/' },
  { id: 'shadowsocks', name: 'Shadowsocks', description: 'Encrypted proxy connections for internet access. OLR currently provides a server for remote clients.', directions: ['out', 'in'], connections: ['shadowsocks'], website: 'https://shadowsocks.org/' },
  { id: 'socks', name: 'SOCKS5', description: 'Use an external proxy as an exit, or let remote clients use this router as their proxy.', directions: ['out', 'in'], connections: ['socks-out', 'socks-in'] },
  { id: 'http', name: 'HTTP', description: 'Use an HTTP proxy, including CONNECT tunnels for HTTPS traffic. Proxy authentication and transport security depend on deployment.', directions: ['out'], connections: [], website: 'https://www.squid-cache.org/' },
  { id: 'trojan', name: 'Trojan', description: 'Proxy internet traffic through an encrypted TLS connection to a compatible server.', directions: ['out'], connections: [], website: 'https://trojan-gfw.github.io/trojan/' },
  { id: 'vless', name: 'VLESS', description: 'Proxy internet traffic using Xray; encryption depends on the configured transport and security settings.', directions: ['out'], connections: [], website: 'https://xtls.github.io/' },
  { id: 'vmess', name: 'VMess', description: 'Proxy internet traffic through an authenticated connection to a compatible V2Ray server.', directions: ['out'], connections: [], website: 'https://www.v2fly.org/' },
  { id: 'hysteria', name: 'Hysteria', description: 'Hysteria 2 proxies TCP/UDP traffic over QUIC through a compatible server.', directions: ['out'], connections: [], website: 'https://v2.hysteria.network/' },
  { id: 'tuic', name: 'TUIC', description: 'Proxy TCP/UDP traffic through a QUIC-based connection to a compatible server.', directions: ['out'], connections: [], website: 'https://github.com/tuic-protocol/tuic' },
  { id: 'gateway', name: 'Gateway', description: 'Route traffic through another gateway or an existing VPN interface.', directions: ['out'], connections: ['gateway'] },
  { id: 'tunnelbroker', name: '6in4', description: 'Obtain routed IPv6 connectivity through Hurricane Electric Tunnel Broker.', directions: ['out'], connections: ['tunnelbroker'] },
  { id: 'https', name: 'HTTPS', description: 'Publish a web service by domain name using the router’s HTTPS reverse proxy.', directions: ['in'], connections: ['https'] },
  { id: 'nat', name: 'NAT', description: 'Forward an external TCP/UDP port to a service inside this network.', directions: ['in'], connections: ['port-forward'] },
]
