import type { DeviceRow, DialStatus } from '@/lib/api-types'

/** Only kernel default-route next hops belong above the router. */
export interface NextHop {
  key: string
  dev: string
  routes: { family: 4 | 6; via?: string; metric: number; down: boolean }[]
  down: boolean
  device?: DeviceRow
}

export interface Outside {
  hops: NextHop[]
}

export function buildOutside(dial?: DialStatus, devices?: DeviceRow[]): Outside | undefined {
  if (!dial) return undefined
  const hops: NextHop[] = []
  for (const route of dial.default_routes ?? []) {
    if (route.family !== 4 && route.family !== 6) continue
    const device = route.via ? devices?.find((d) =>
      d.fixed_ip === route.via || d.ips?.includes(route.via!) ||
      (route.gateway_mac && d.mac.toLowerCase() === route.gateway_mac.toLowerCase())) : undefined
    // A known device may use different IPv4 and IPv6 gateway addresses.
    const key = device ? `device:${device.mac}:${route.dev}` : `${route.dev}:${route.via ?? `direct:${route.family}`}`
    const existing = hops.find((h) => h.key === key)
    const path = { family: route.family, via: route.via, metric: route.metric,
      down: route.gateway_state === 'silent' }
    if (existing) {
      existing.routes.push(path)
      existing.down ||= path.down
      continue
    }
    hops.push({
      key,
      dev: route.dev,
      routes: [path],
      down: path.down,
      device,
    })
  }
  return { hops }
}
