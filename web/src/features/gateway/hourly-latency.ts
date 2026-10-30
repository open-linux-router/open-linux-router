import type { GatewayLatency } from '@/lib/api-types'

export type LatencySite = GatewayLatency['custom'][number]
export type HourlySample = NonNullable<LatencySite['samples']>[number]

function browserWallTime(at: Date) {
  return `${at.getFullYear()}-${String(at.getMonth() + 1).padStart(2, '0')}-${String(at.getDate()).padStart(2, '0')}T${String(at.getHours()).padStart(2, '0')}:00:00`
}

/** The router's calendar defines both the scheduled probe and the fixed axis. */
export function latencyDay(localTime?: string, fallback = new Date()) {
  const valid = localTime && Number.isFinite(Date.parse(localTime))
    && /^\d{4}-\d{2}-\d{2}T\d{2}:/.test(localTime)
  const at = valid ? localTime : browserWallTime(fallback)
  const offset = valid ? localTime.match(/(Z|[+-]\d{2}:\d{2})$/)?.[1] : undefined
  return {
    date: at.slice(0, 10),
    hour: Number(at.slice(11, 13)),
    zone: offset === 'Z' ? 'UTC' : offset ? `UTC${offset}` : '',
  }
}

/** Retests replace a slot; yesterday and tomorrow never move onto today's axis. */
export function hourlyLatency(site: LatencySite, day: ReturnType<typeof latencyDay>): (HourlySample | null)[] {
  const slots: (HourlySample | null)[] = Array.from({ length: 24 }, () => null)
  // Older daemons lack hour_start; the latest outcome can still occupy its hour.
  const samples = site.samples?.length ? site.samples : site.checked_at && !site.checked_at.startsWith('0001-')
    ? [{ milliseconds: site.milliseconds, error: site.error, checked_at: site.checked_at }] : []
  const offset = day.zone.match(/^UTC([+-])(\d{2}):(\d{2})$/)
  const offsetMinutes = offset ? (Number(offset[2]) * 60 + Number(offset[3])) * (offset[1] === '-' ? -1 : 1) : 0
  for (const sample of samples) {
    const checked = Date.parse(sample.checked_at)
    if (!Number.isFinite(checked)) continue
    const fallback = day.zone ? new Date(checked + offsetMinutes * 60_000).toISOString() : browserWallTime(new Date(checked))
    const at = sample.hour_start ?? fallback
    if (!Number.isFinite(Date.parse(at)) || at.slice(0, 10) !== day.date) continue
    const hour = Number(at.slice(11, 13))
    if (!Number.isInteger(hour) || hour < 0 || hour >= 24 || hour > day.hour) continue
    const previous = slots[hour]
    if (!previous || checked > Date.parse(previous.checked_at)) slots[hour] = sample
  }
  return slots
}

export function hourRange(hour: number) {
  return `${String(hour).padStart(2, '0')}:00–${String(hour + 1).padStart(2, '0')}:00`
}
