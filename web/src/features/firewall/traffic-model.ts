import type { FirewallTraffic } from '../../lib/api-types'

export type TrafficFlow = Omit<FirewallTraffic, 'description' | 'destination'> & {
  descriptions: string[]
  destinations: FirewallTraffic['destination'][]
}

export const sourceLabels = {
  outside: 'External networks',
  inside: 'LAN / trusted networks',
  local: 'This router',
}

// Merge input/forward and IPTV paths with the same source, semantic category
// and verdict. Never join different origins or different decisions.
export function trafficFlows(traffic: FirewallTraffic[]): TrafficFlow[] {
  const flows = new Map<string, TrafficFlow>()
  for (const row of traffic) {
    if (!(row.packets > 0)) continue
    const id = JSON.stringify([row.source, row.category, row.verdict])
    const flow = flows.get(id)
    if (flow) {
      flow.packets += row.packets
      flow.bytes += row.bytes
      if (!flow.descriptions.includes(row.description)) flow.descriptions.push(row.description)
      if (!flow.destinations.includes(row.destination)) flow.destinations.push(row.destination)
    } else {
      flows.set(id, { ...row, id, descriptions: [row.description], destinations: [row.destination] })
    }
  }
  return [...flows.values()]
}
