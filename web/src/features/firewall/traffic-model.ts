import type { FirewallOpening, FirewallStatus } from '../../lib/api-types'

export interface TrafficBranch {
  id: string
  title: string
  detail: string
  explanation: string
  tone: 'normal' | 'configured' | 'attention' | 'inactive'
}

export function groupOpenings(openings: FirewallOpening[]) {
  const groups = new Map<string, string[]>()
  for (const opening of openings) {
    const match = opening.from
      ? `${opening.protocol} from ${opening.from}`
      : `${opening.protocol} ${opening.port}`
    groups.set(opening.for, [...(groups.get(opening.for) ?? []), match])
  }
  return [...groups].map(([name, matches]) => ({ name, matches }))
}

export function trafficBranches(status: FirewallStatus): TrafficBranch[] {
  const verified = status.enabled && status.known && !status.drifted && !status.problem
  return [
    {
      id: 'replies',
      title: 'Established replies',
      detail: status.enabled ? 'Allowed by policy' : 'Not filtered by OLR',
      explanation: 'Established and related connections are allowed. These packets are not counted separately.',
      tone: status.enabled ? 'normal' : 'inactive',
    },
    {
      id: 'openings',
      title: 'Configured openings',
      detail: 'Services and port forwards',
      explanation: 'Configured services and traffic matching a port forward are permitted. ICMP, DHCP client traffic and configured IPTV exceptions also pass. Allowed packets are not counted here.',
      tone: status.enabled ? 'configured' : 'inactive',
    },
    {
      id: 'blocked',
      title: status.enabled ? 'Blocked attempts' : 'No inbound filtering',
      detail: !status.enabled
        ? 'Firewall is off'
        : verified
          ? `${(status.blocked_input + status.blocked_forward).toLocaleString()} packets`
          : 'Count not verified',
      explanation: !status.enabled
        ? 'OLR is not enforcing its firewall policy.'
        : verified
          ? `${status.blocked_input.toLocaleString()} packets to this router and ${status.blocked_forward.toLocaleString()} to inside devices were blocked since the rules were written. Blocked does not necessarily mean malicious.`
          : 'The intended policy is shown, but its counters cannot be relied on until the rules are verified.',
      tone: verified ? 'attention' : 'inactive',
    },
  ]
}
