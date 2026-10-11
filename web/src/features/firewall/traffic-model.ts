import type { FirewallOpening, FirewallStatus } from '../../lib/api-types'

export interface TrafficBranch {
  id: string
  title: string
  detail: string
  explanation: string
  tone: 'normal' | 'configured' | 'attention' | 'inactive'
  group: 'incoming' | 'outgoing' | 'local'
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
      group: 'incoming',
      title: 'Replies to existing connections',
      detail: status.enabled ? 'Allowed by policy' : 'Not filtered by OLR',
      explanation: 'Established and related connections are allowed. These packets are not counted separately.',
      tone: status.enabled ? 'normal' : 'inactive',
    },
    {
      id: 'openings',
      group: 'incoming',
      title: 'Published services',
      detail: 'Services and port forwards',
      explanation: 'Enabled remote access, hosted services and traffic matching a port forward are permitted. Configured IPTV streams also pass. Turn off the service or remove the forward to close its opening.',
      tone: status.enabled ? 'configured' : 'inactive',
    },
    {
      id: 'blocked',
      group: 'incoming',
      title: 'Unsolicited inbound traffic',
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
    {
      id: 'essentials', group: 'incoming', title: 'Network essentials',
      detail: status.enabled ? 'Required types allowed' : 'Not filtered by OLR',
      explanation: 'DHCP client replies, permitted ICMP and IPv6 control messages keep addressing, diagnostics and path discovery working. Configured IPTV control traffic also passes.',
      tone: status.enabled ? 'normal' : 'inactive',
    },
    {
      id: 'internet', group: 'outgoing', title: 'Internet access',
      detail: status.enabled ? 'Allowed by policy' : 'Not filtered by OLR',
      explanation: 'Local devices may start connections to the internet. Gateway routing decides which outbound path they use.',
      tone: status.enabled ? 'normal' : 'inactive',
    },
    {
      id: 'router-outgoing', group: 'outgoing', title: 'Router-originated traffic',
      detail: 'Not filtered by OLR',
      explanation: 'The router may start its own DNS queries, updates, time synchronization and tunnel connections. OLR has no output filtering chain.',
      tone: status.enabled ? 'configured' : 'inactive',
    },
    {
      id: 'local', group: 'local', title: 'Local network traffic',
      detail: status.enabled ? 'Trusted networks allowed' : 'Not filtered by OLR',
      explanation: 'Routed connections between trusted inside networks, including connected remote-access VPN clients, are allowed. Traffic switched directly between devices on the same LAN may never reach this firewall.',
      tone: status.enabled ? 'normal' : 'inactive',
    },
    {
      id: 'router-access', group: 'local', title: 'Router access',
      detail: status.enabled ? 'Allowed from inside' : 'Not filtered by OLR',
      explanation: 'Inside devices may reach the router’s DNS, administration and other listening services. Outside access requires a published service opening.',
      tone: status.enabled ? 'configured' : 'inactive',
    },
  ]
}
