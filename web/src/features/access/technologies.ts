import { t } from '@/lib/i18n'
import type { AccessKind } from './catalog'


export interface AccessTechnology {
  id: string
  name: string
  description: string
  directions: ('network' | 'in' | 'out')[]
  connections: AccessKind[]
  website?: string
}

// Networking technologies appear once, before the directional groups.
// Only real OLR configuration owners belong in connections; documentation is
// not integrated support.
export const ACCESS_TECHNOLOGIES: AccessTechnology[] = [
  { id: 'wireguard', name: 'WireGuard', get description() { return t('Encrypted tunnels for remote access, site links and internet exits. OLR currently configures incoming device connections.') }, directions: ['network'], connections: ['wireguard'], website: 'https://www.wireguard.com/' },
  { id: 'tailscale', name: 'Tailscale', description: 'Managed WireGuard networking with remote access, subnet routes and exit nodes.', directions: ['network'], connections: [], website: 'https://tailscale.com/' },
  { id: 'netbird', name: 'NetBird', get description() { return t('WireGuard networking with managed access, network routes and exit nodes; hosted or self-hosted.') }, directions: ['network'], connections: [], website: 'https://netbird.io/' },
  { id: 'zerotier', name: 'ZeroTier', get description() { return t('Virtual networks connecting devices and sites, with managed routes to other networks.') }, directions: ['network'], connections: [], website: 'https://www.zerotier.com/' },
  { id: 'headscale', name: 'Headscale', get description() { return t('A self-hosted control server for Tailscale clients, including subnet routes and exit nodes.') }, directions: ['network'], connections: [], website: 'https://headscale.net/' },
  { id: 'frp', name: 'frp', description: 'Publish web and TCP/UDP services through a remote server, including private service access with visitor clients.', directions: ['in'], connections: [], website: 'https://gofrp.org/' },
  { id: 'cloudflare', name: 'Cloudflare', get description() { return t('Cloudflare Tunnel publishes services through outbound tunnels; Access can restrict who reaches them.') }, directions: ['in'], connections: [], website: 'https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/' },
  { id: 'ssh', name: 'SSH', get description() { return t('Forward selected ports through encrypted local, reverse or SOCKS tunnels.') }, directions: ['out', 'in'], connections: [], website: 'https://www.openssh.com/' },
  { id: 'socks', name: 'SOCKS5', get description() { return t('Use an external proxy as an exit, or let remote clients use this router as their proxy.') }, directions: ['out', 'in'], connections: ['socks-out', 'socks-in'] },
  { id: 'gateway', get name() { return t('Gateway') }, description: 'Route traffic through another gateway or an existing VPN interface.', directions: ['out'], connections: ['gateway'] },
  { id: 'tunnelbroker', name: 'Tunnel Broker', description: 'Obtain routed IPv6 connectivity through Hurricane Electric Tunnel Broker.', directions: ['out'], connections: ['tunnelbroker'] },
]
