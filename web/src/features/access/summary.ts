import type { DialStatus, ForwardsStatus, GatewayStatus, IngressStatus, ProxyStatus, RemoteStatus, SocksStatus } from '@/lib/api-types'
import type { DialConfig, GatewayConfig, IngressConfig, RemoteConfig } from '@/lib/config-types'
import type { TunnelBrokerStatus } from '@/features/dial/queries'
import type { SocksOutConfig, SocksOutStatus } from '@/features/socksout/queries'

type Snapshot<T> = { data?: T; isError: boolean }
export type Summary = { text: string; attention?: boolean }

const loading: Summary = { text: 'Loading…' }
const unavailable: Summary = { text: 'Status unavailable', attention: true }
const attention: Summary = { text: 'Needs attention', attention: true }
const off: Summary = { text: 'Off' }
const count = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`

// A failed refresh must not present an old answer as the current state.
function missing<T>(query: Snapshot<T>): Summary | undefined {
  if (query.isError) return unavailable
  if (!query.data) return loading
}

export function systemSummary(status: Snapshot<DialStatus>): Summary {
  const pending = missing(status)
  if (pending) return pending
  const routes = status.data!.default_routes
  if (!routes.length) return { text: 'No default route', attention: true }
  return { text: routes.map(route => `IPv${route.family} via ${route.via ? `${route.via} · ` : ''}${route.dev}`).join(' · ') }
}

export function tunnelBrokerSummary(config: Snapshot<DialConfig>, status: Snapshot<TunnelBrokerStatus>): Summary {
  const pending = missing(config)
  if (pending) return pending
  const broker = config.data!.tunnelbroker
  if (!broker) return { text: 'Not configured' }
  if (!broker.enabled) return { text: 'Saved · Off' }
  const observed = missing(status)
  if (observed) return observed
  if (!status.data!.present || !status.data!.up || !status.data!.matches) return attention
  return { text: 'Tunnel configured' }
}

export function gatewaySummary(config: Snapshot<GatewayConfig>, status: Snapshot<GatewayStatus>): Summary {
  const pending = missing(config)
  if (pending) return pending
  if (!config.data!.enabled) return off
  const observed = missing(status)
  if (observed) return observed
  if (!status.data!.known) return unavailable
  if (!status.data!.enabled || status.data!.drifted || status.data!.problems?.length || status.data!.exits.some((e) => e.probed && !e.up)) return attention
  const exits = config.data!.exits?.filter((e) => e.via.kind !== 'blocked') ?? []
  return { text: exits.length ? count(exits.length, 'way out', 'ways out') : 'None configured' }
}

export function socksOutSummary(config: Snapshot<SocksOutConfig>, status: Snapshot<SocksOutStatus>): Summary {
  const pending = missing(config)
  if (pending) return pending
  if (!config.data!.enabled) return off
  const observed = missing(status)
  if (observed) return observed
  if (!status.data!.enabled || status.data!.problem || !status.data!.service.active) return attention
  // Running is the local TUN service, not proof that the external proxy works.
  return { text: 'TUN running' }
}

export function remoteSummary(
  config: Snapshot<RemoteConfig>,
  wireguard: Snapshot<RemoteStatus>,
  shadowsocks: Snapshot<ProxyStatus>,
  socks: Snapshot<SocksStatus>,
  only?: 'wireguard' | 'shadowsocks' | 'socks',
): Summary {
  const pending = (only === 'socks' ? missing(socks) : missing(config)) ?? (!only ? missing(socks) : undefined)
  if (pending) return pending
  const parts: string[] = []
  if ((!only || only === 'wireguard') && config.data!.wireguard.enabled) {
    const observed = missing(wireguard)
    if (observed) return observed
    const wg = wireguard.data!
    if (!wg.kernel_known) return unavailable
    if (!wg.enabled || !wg.interface_present || !wg.interface_up || wg.interface_foreign || wg.drifted || wg.blockers?.length) return attention
    parts.push(`${count(wg.peers.length, 'device')} · ${wg.peers.filter((p) => p.online).length} connected`)
  }
  if ((!only || only === 'shadowsocks') && config.data!.shadowsocks.enabled) {
    const observed = missing(shadowsocks)
    if (observed) return observed
    const ss = shadowsocks.data!
    if (ss.service_error || !ss.service) return unavailable
    if (!ss.enabled || !ss.service.active || ss.binary_error || ss.drifted) return attention
    parts.push('Shadowsocks on')
  }
  if ((!only || only === 'socks') && socks.data!.enabled) {
    const s = socks.data!
    if (s.service_error || !s.service) return unavailable
    if (!s.service.active || s.binary_error || s.drifted || s.warning) return attention
    parts.push('SOCKS5 on')
  }
  return parts.length ? { text: parts.join(' · ') } : off
}

export function ingressSummary(config: Snapshot<IngressConfig>, status: Snapshot<IngressStatus>): Summary {
  const pending = missing(config)
  if (pending) return pending
  if (!config.data!.enabled) return off
  const observed = missing(status)
  if (observed) return observed
  const s = status.data!
  if (s.service_error || !s.service || s.certificate_error || s.drift_error) return unavailable
  if (!s.enabled || !s.service.active || s.binary_error || s.drifted || s.certificate.renewal_overdue || s.certificate_warnings?.length || (s.certificate.found && (s.certificate.expires_in_days ?? 0) <= 0)) return attention
  if (!s.certificate.found) return { text: 'Waiting for certificate' }
  return { text: count(config.data!.services?.length ?? 0, 'address', 'addresses') }
}

export function forwardsSummary(config: Snapshot<GatewayConfig>, status: Snapshot<ForwardsStatus>): Summary {
  const pending = missing(config)
  if (pending) return pending
  if (!config.data!.enabled) return off
  const observed = missing(status)
  if (observed) return observed
  if (!status.data!.known) return unavailable
  if (!status.data!.enabled || status.data!.drifted || status.data!.problems?.length) return attention
  return { text: count(config.data!.forwards?.length ?? 0, 'rule') }
}
