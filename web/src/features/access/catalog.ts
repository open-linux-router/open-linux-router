// User intents are independent of configuration owners and routing targets.
// System default is observed separately and cannot be added or deleted.
export type AccessDirection = 'in' | 'out'
export const ACCESS_DEFINITIONS = [
  { kind: 'gateway', direction: 'out', label: 'Gateway', description: 'Another gateway or an existing VPN interface', to: '/access/gateway' },
  { kind: 'socks-out', direction: 'out', label: 'SOCKS5', description: 'Connect through an external proxy', to: '/access/socks5' },
  { kind: 'tunnelbroker', direction: 'out', label: 'Tunnel Broker', description: 'IPv6 through Hurricane Electric', to: '/access/tunnelbroker' },
  { kind: 'wireguard', direction: 'in', label: 'WireGuard', description: 'Connect your devices back to this network', to: '/access/wireguard' },
  { kind: 'socks-in', direction: 'in', label: 'SOCKS5', description: 'Use this router as a SOCKS5 proxy', to: '/access/socks-server' },
  { kind: 'port-forward', direction: 'in', label: 'Port forwarding', description: 'Reach a service through an external port', to: '/access/forwards' },
] as const
export type AccessKind = typeof ACCESS_DEFINITIONS[number]['kind']
