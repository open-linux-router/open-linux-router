import { Badge } from '@/components/ui/badge'
import type { RemotePeer } from '@/lib/api-types'

/**
 * Four states, not two, and the one that matters is "never connected".
 *
 * A device that has *never* connected almost always means the configuration was
 * never imported, or the port is not reachable from outside — a setup problem
 * with somewhere to look. A device last seen yesterday is working and asleep.
 * Collapsing them into "offline" would hide the only failure this module
 * reliably produces on a first install.
 *
 * Shared by the device list on the tunnel page and the device's own page, so
 * that one peer cannot read as "Connected" on one screen and "offline" on the
 * next.
 */
export function PeerBadge({ peer }: { peer: RemotePeer }) {
  if (peer.unknown) return <Badge variant="secondary">Unknown</Badge>
  if (peer.online) return <Badge variant="success">Connected</Badge>
  if (!peer.last_handshake) return <Badge variant="warning">Never connected</Badge>
  return <Badge variant="secondary">{lastSeen(peer.last_handshake)}</Badge>
}

function lastSeen(iso: string): string {
  const minutes = Math.round((Date.now() - new Date(iso).getTime()) / 60_000)
  if (minutes < 60) return `${minutes} min ago`
  const hours = Math.round(minutes / 60)
  if (hours < 48) return `${hours}h ago`
  return `${Math.round(hours / 24)}d ago`
}
