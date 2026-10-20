import type { AccessView } from '@/lib/api'
import type { DialStatus, ForwardsStatus, IngressStatus } from '@/lib/api-types'
import type { DialConfig, GatewayConfig, IngressConfig } from '@/lib/config-types'
import type { IptvConfig, IptvStatus } from '@/features/iptv/queries'

/**
 * What the Advanced directory says about each feature, in one line.
 *
 * The same `{ text, attention }` shape the Access summaries use: the landing
 * page is read before anything is opened, so the row of answers is the page's
 * job. A failed observation is never shown as "off", and a saved setting whose
 * service is not running is shown as needing attention rather than as working.
 *
 * These functions take query snapshots (`{ data, isError }`) rather than the
 * hooks, so the answers can be tested without a router.
 */
export type Summary = { text: string; attention?: boolean }

type Snapshot<T> = { data?: T; isError: boolean }

export const loading: Summary = { text: 'Loading…' }
export const unavailable: Summary = { text: 'Status unavailable', attention: true }
const attention: Summary = { text: 'Needs attention', attention: true }
const off: Summary = { text: 'Off' }
const count = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`

// A failed refresh must not present an old answer as the current state.
function missing<T>(query: Snapshot<T>): Summary | undefined {
  if (query.isError) return unavailable
  if (!query.data) return loading
}

// Port forwarding renders its own rules; the tile says whether what is saved is
// what the kernel is filtering.
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

export function ddnsSummary(config: Snapshot<DialConfig>, status: Snapshot<DialStatus>): Summary {
  const pending = missing(config)
  if (pending) return pending
  const records = config.data!.records ?? []
  if (!records.length) return { text: 'None' }
  // The names are intent, so they stay readable even when the observation is
  // missing. Health is only claimed from an observation that arrived.
  const failed = status.data?.records.filter(record => record.check_error || record.publish_error).length ?? 0
  if (failed) return { text: `${count(failed, 'name')} failing`, attention: true }
  return { text: records.map(record => record.name).join(', ') }
}

export function iptvSummary(config: Snapshot<IptvConfig>, status: Snapshot<IptvStatus>): Summary {
  const pending = missing(config)
  if (pending) return pending
  if (!config.data!.enabled) return off
  const observed = missing(status)
  if (observed) return observed
  const observedStatus = status.data!
  if (observedStatus.problem) return attention
  if (!observedStatus.service.active) return { text: 'Stopped', attention: true }
  return { text: `On · ${config.data!.networks?.join(', ') || 'No LAN'}` }
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

export function adminAccessSummary(access: Snapshot<AccessView>): Summary {
  const pending = missing(access)
  if (pending) return pending
  return { text: access.data!.password_set ? 'Password set' : 'No password set' }
}
