import type { GatewayTraffic } from '@/lib/api-types'

/** The half of a traffic reading the next poll differences against. */
export interface TrafficSample {
  as_of: string
  counting: boolean
  usage: { address: string; exit: string; up_bytes: number; down_bytes: number }[]
}

/**
 * Where the last reading is kept between visits.
 *
 * A rate is two readings differenced, and that pair used to live only in the
 * page: a refresh threw the earlier half away, so every speed on the Overview
 * read "—" until the next poll came back to pair with. The delay belonged to
 * the tab, not to the router, whose counters are cumulative since load — so
 * the reading is remembered here and the first poll after a reload is
 * differenced against it.
 *
 * A sample older than TRAFFIC_SAMPLE_MAX_AGE_MS is dropped: differencing
 * across an idle afternoon would report an average as the current speed, and
 * one poll of "—" is the more honest answer. This is only ever the *before*
 * half of a calculation; it is never drawn as a number of its own.
 */
export const TRAFFIC_SAMPLE_KEY = 'olr.gateway.traffic-sample-v1'

/** The oldest reading the first poll after a reload may use as its before. */
export const TRAFFIC_SAMPLE_MAX_AGE_MS = 60_000

/** Writing on every poll would be work for a value only a reload reads. */
export const TRAFFIC_SAMPLE_SAVE_MS = 5_000

type Store = Pick<Storage, 'getItem' | 'setItem'>

function storage(): Store | undefined {
  try {
    return typeof sessionStorage === 'undefined' ? undefined : sessionStorage
  } catch {
    // Private modes can throw on access rather than return undefined.
    return undefined
  }
}

/** Only the fields a difference needs, so a full accounting set stays small. */
function sampleOf(data: GatewayTraffic): TrafficSample {
  return {
    as_of: data.as_of,
    counting: data.counting,
    usage: data.usage.map(({ address, exit, up_bytes, down_bytes }) => ({ address, exit, up_bytes, down_bytes })),
  }
}

export function readTrafficSample(now = Date.now(), store = storage()): TrafficSample | undefined {
  if (!store) return undefined
  try {
    const stored = store.getItem(TRAFFIC_SAMPLE_KEY)
    if (!stored) return undefined
    const sample = JSON.parse(stored) as TrafficSample
    if (!sample || typeof sample.as_of !== 'string' || !Array.isArray(sample.usage) || !sample.counting) return undefined
    const at = Date.parse(sample.as_of)
    if (!Number.isFinite(at) || now - at > TRAFFIC_SAMPLE_MAX_AGE_MS) return undefined
    return sample
  } catch {
    return undefined
  }
}

export function writeTrafficSample(data: GatewayTraffic, store = storage()): void {
  if (!store || !data.counting || !Number.isFinite(Date.parse(data.as_of))) return
  try {
    store.setItem(TRAFFIC_SAMPLE_KEY, JSON.stringify(sampleOf(data)))
  } catch {
    /* Storage full or disabled: the next refresh just waits for a second poll. */
  }
}
