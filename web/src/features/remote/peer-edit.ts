import { remoteChange, type PeerPatch, type RemoteChangeRequest } from './queries'
import type { RemotePeer } from '@/lib/api-types'

/**
 * Saving an edited peer, in the order that does not leave the two halves
 * disagreeing.
 *
 * The dialog asks for a name, a label and what the device sends home; those live
 * in two requests. The name is the one that can be refused — another device may
 * hold it — and there is no point rewriting what the device sends if the device
 * is still called what it was, so the labels go first and the routes follow.
 *
 * Shared by the tunnel page's list and the device's own page, so that editing a
 * peer from either place means the same three requests in the same order.
 */
export async function savePeerEdit(
  peer: RemotePeer,
  next: { name: string; display_name: string; routes: string },
  change: (request: RemoteChangeRequest) => Promise<boolean>,
): Promise<boolean> {
  const patch: PeerPatch = {}
  if (next.name !== peer.name) patch.name = next.name
  if (next.display_name !== (peer.display_name ?? '')) patch.display_name = next.display_name
  if (Object.keys(patch).length > 0) {
    if (!(await change(remoteChange.patchPeer(peer.name, patch)))) return false
  }
  // Compared with the same default the dialog applies, or an unstored `routes`
  // would read as a change on every save.
  if (next.routes !== (peer.routes || 'home')) {
    if (!(await change(remoteChange.savePeer(next.name, { routes: next.routes })))) return false
  }
  return true
}
