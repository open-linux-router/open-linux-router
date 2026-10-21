import { t } from '@/lib/i18n'
// User intents are independent of configuration owners and routing targets.
// System default is observed separately and cannot be added or deleted.
export type AccessDirection = 'in' | 'out'
export const ACCESS_DEFINITIONS = [
  { kind: 'gateway', direction: 'out', get label() { return t('Gateway') }, get description() { return t('Another gateway or an existing VPN interface') }, to: '/access/gateway' },
  { kind: 'socks-out', direction: 'out', label: 'SOCKS5', get description() { return t('Connect through an external proxy') }, to: '/access/socks5' },
  { kind: 'tunnelbroker', direction: 'out', label: 'Tunnel Broker', get description() { return t('IPv6 through Hurricane Electric') }, to: '/access/tunnelbroker' },
  { kind: 'wireguard', direction: 'in', label: 'WireGuard', get description() { return t('Connect your devices back to this network') }, to: '/access/wireguard' },
  { kind: 'socks-in', direction: 'in', label: 'SOCKS5', get description() { return t('Use this router as a SOCKS5 proxy') }, to: '/access/socks-server' },
] as const
export type AccessKind = typeof ACCESS_DEFINITIONS[number]['kind']
