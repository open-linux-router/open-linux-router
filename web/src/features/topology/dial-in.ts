import type { RemotePeer, RemoteStatus } from '@/lib/api-types'

/**
 * The devices that dialed in over WireGuard, as the map draws them.
 *
 * They are not `DeviceRow`s, and turning them into ones is the thing
 * docs/remote-access.md §3.3 refuses: `devices` is keyed by MAC, a peer is a
 * public key at the end of a tunnel, and inventing a hardware address so one
 * list could hold both would put a made-up NIC in the operator's own device
 * list — where the vendor lookup and the icon detection would then report on
 * it. The map reads the tunnel's own answer instead and shows what a tunnel
 * can actually say: the address it was given, whether it is connected now,
 * where it was last heard from.
 *
 * Existence is decided by `remote`, never by this module. A box with the
 * tunnel switched off gets nothing at all — no empty group, no "WireGuard is
 * off" card on a network map that is about what is on the network.
 */

/**
 * Connected, asleep, never used, or unreadable.
 *
 * Four states because collapsing them loses the only two distinctions an
 * operator acts on (docs/remote-access.md §3.4, and `PeerBadge` on the
 * tunnel's page): a peer that has never handshaked is a configuration that was
 * not imported or a port that is not reachable, while one last seen an hour ago
 * is working and asleep. And `unknown` is an absence of an answer where the
 * kernel could not be read at all, which must not be reported as "never".
 */
export type DialInState = 'connected' | 'away' | 'never' | 'unknown'

/** One peer, as the map needs it. Fields, not sentences: the node formats. */
export interface DialInPeer {
  /** Unique on the map. Peer names are unique in the configuration. */
  key: string
  name: string
  address?: string
  state: DialInState
  lastHandshake?: string
  /** Where it was last heard from, which is its address out in the world. */
  endpoint?: string
  rxBytes: number
  txBytes: number
}

export interface DialIn {
  peers: DialInPeer[]
  /** The interface they are on, so the map can say where they landed. */
  interface: string
}

export function dialInState(peer: RemotePeer): DialInState {
  if (peer.unknown) return 'unknown'
  if (peer.online) return 'connected'
  if (peer.last_handshake) return 'away'
  return 'never'
}

/**
 * Nothing, or every peer — never a half-drawn state.
 *
 * Name order, not "connected first". The band sits above the router, where a
 * card that jumped to the front every time a phone woke up would read as the
 * picture rearranging itself; each card carries its own dot and its own word,
 * so the order has no work left to do.
 */
export function buildDialIn(status?: RemoteStatus): DialIn | undefined {
  if (!status?.enabled || status.peers.length === 0) return undefined
  return {
    interface: status.interface,
    peers: status.peers
      .map((peer) => ({
        key: peer.name,
        name: peer.name,
        address: peer.address,
        state: dialInState(peer),
        lastHandshake: peer.last_handshake,
        endpoint: peer.endpoint,
        rxBytes: peer.rx_bytes ?? 0,
        txBytes: peer.tx_bytes ?? 0,
      }))
      .sort((a, b) => a.name.localeCompare(b.name)),
  }
}
