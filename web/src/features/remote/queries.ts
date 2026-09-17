import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api } from '@/lib/api'
import type {
  ProxyApplyResult,
  ProxyLink,
  ProxyStatus,
  RemoteApplyResult,
  RemotePeers,
  RemoteStatus,
} from '@/lib/api-types'
import type { RemoteConfig } from '@/lib/config-types'

// The same polling story as the other modules: EventSource cannot send an
// Authorization header, so streaming from the browser needs a cookie session or
// a fetch-based reader, and neither is worth inventing before there is live data
// to carry.
//
// Ten seconds, which is faster than ingress and slower than nothing, because of
// one moment in particular: the operator has just scanned a configuration into
// their phone and is watching this screen to find out whether it worked. A
// handshake lands within a second or two of the phone trying, and a screen that
// took half a minute to admit it would send them to check their port forwarding
// for a problem they do not have.
const OBSERVED_REFETCH_MS = 10_000

export const remoteKeys = {
  config: ['remote', 'config'] as const,
  status: ['remote', 'status'] as const,
  peers: ['remote', 'peers'] as const,
}

export function useRemoteConfig() {
  return useQuery({
    queryKey: remoteKeys.config,
    queryFn: () => api.get<RemoteConfig>('/api/remote/config'),
  })
}

export function useRemoteStatus() {
  return useQuery({
    queryKey: remoteKeys.status,
    queryFn: () => api.get<RemoteStatus>('/api/remote/wireguard/status'),
    // Observed state, never cached by the daemon (design.md §4.5), so the only
    // way to stay current is to ask again.
    refetchInterval: OBSERVED_REFETCH_MS,
  })
}

export function useRemotePeers() {
  return useQuery({
    queryKey: remoteKeys.peers,
    queryFn: () => api.get<RemotePeers>('/api/remote/wireguard/peers'),
    refetchInterval: OBSERVED_REFETCH_MS,
  })
}

/**
 * One change, described the way the API names it.
 *
 * A value rather than a call, because a change has to survive being held: when
 * the daemon answers 409 because the change would take somebody's way in away,
 * the page shows the plan and then sends *the same change* again with
 * `confirm=true`. Replaying is only simple if the change is data.
 */
export interface RemoteChangeRequest {
  method: 'PUT' | 'DELETE' | 'PATCH'
  path: string
  body?: unknown
  /** What this change is, for the toast. */
  label: string
}

const base = '/api/remote'
const seg = (s: string) => encodeURIComponent(s)

/**
 * The changes this screen can make, each naming the thing it changes.
 *
 * The split is the one RFC 7386 forces: a merge patch merges an object key by
 * key and replaces an array wholesale, so the tunnel's settings go through
 * PATCH and the devices get a route per item. Here that is not a nicety — a
 * patch meaning to edit one device would revoke every other one.
 */
export const remoteChange = {
  /**
   * Add a device, or change one.
   *
   * The response to an *add* carries the client configuration, and it is the
   * only response in olr that cannot be reproduced: olr keeps the public half
   * of the key and nothing else. Whatever consumes this has exactly one chance
   * to put it in front of somebody.
   */
  savePeer: (name: string, body: { routes?: string; public_key?: string }): RemoteChangeRequest => ({
    method: 'PUT',
    path: `${base}/wireguard/peers/${seg(name)}`,
    body,
    label: `${name} can dial in`,
  }),
  removePeer: (name: string): RemoteChangeRequest => ({
    method: 'DELETE',
    path: `${base}/wireguard/peers/${seg(name)}`,
    label: `${name} can no longer dial in`,
  }),
  /**
   * The tunnel's settings, patched key by key.
   *
   * Nested under `wireguard` because the stored document is: Shadowsocks and
   * SOCKS5 land beside it later, and a flat document would have to move a key
   * in everybody's backup to make room (docs/remote-access.md §11 #4).
   *
   * Note what this cannot accidentally do: a patch that does not mention
   * `private_key` leaves the stored key alone, and one that sends back the mask
   * the API handed out means the same thing. So a form can round-trip every
   * field here without a way to destroy the key by saving a screen it only read
   * — which would invalidate every configuration ever issued.
   */
  settings: (fields: Record<string, unknown>): RemoteChangeRequest => ({
    method: 'PATCH',
    path: `${base}/wireguard/config`,
    body: fields,
    label: 'Saved',
  }),
  /**
   * The one field that is not the tunnel's.
   *
   * Where devices dial belongs to the box: the proxy beside the tunnel needs
   * exactly the same value, and a field typed twice is a field that can
   * disagree with itself. So it has its own route, and its own consequence —
   * changing it invalidates every configuration already on a device, which is
   * why the daemon refuses it without confirm.
   */
  endpoint: (endpoint: string): RemoteChangeRequest => ({
    method: 'PATCH',
    path: `${base}/config`,
    body: { endpoint },
    label: 'Saved',
  }),
}

/**
 * Sends one change, optionally already confirmed.
 *
 * There is no separate preview call. The daemon plans before it writes and
 * refuses on its own when the plan is disruptive, so the round trip that would
 * be spent asking is only spent when the answer is "ask the operator".
 */
export function useApplyRemoteChange() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: ({ change, confirm }: { change: RemoteChangeRequest; confirm?: boolean }) =>
      api.send<RemoteApplyResult>(
        change.method,
        confirm ? `${change.path}?confirm=true` : change.path,
        change.body,
      ),
    onSettled: () => {
      // Invalidated on failure too: a partial apply may already have created
      // the interface, so every observed answer is stale whether or not the
      // call succeeded.
      queryClient.invalidateQueries({ queryKey: ['remote'] })
    },
  })
}

/**
 * Re-program the tunnel from stored intent, changing nothing.
 *
 * The repair path design.md §5.3.2 asks for in place of rollback, and here it
 * has a concrete job the other modules do not: kernel state does not survive a
 * reboot and an interface can be removed by hand, so this is the button that
 * puts the tunnel back. It needs no confirmation — the daemon treats it as
 * already confirmed, because the intent being re-applied is intent the operator
 * stored earlier.
 */
export function useReapplyRemote() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: () => api.send<RemoteApplyResult>('POST', `${base}/wireguard/apply`, undefined),
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['remote'] }),
  })
}

// --- the proxy ---------------------------------------------------------------
//
// A second object under the same module, with its own status, its own apply and
// its own change builder — mirroring internal/remote, where the two share a
// namespace and no plan. What they do share is `remoteKeys`' root, so one
// invalidation after any write refreshes both; a change to the endpoint really
// does affect them both at once.

export function useShadowsocksStatus() {
  return useQuery({
    queryKey: [...remoteKeys.status, 'shadowsocks'] as const,
    queryFn: () => api.get<ProxyStatus>(`${base}/shadowsocks/status`),
    refetchInterval: OBSERVED_REFETCH_MS,
  })
}

export const shadowsocksChange = {
  /**
   * The proxy's settings, patched key by key.
   *
   * Same safety as the tunnel's: a patch that does not mention `password`
   * leaves the stored one alone, and sending back the mask the API handed out
   * means the same thing — so a form can round-trip every field it only read
   * without re-issuing a credential to everybody.
   *
   * The exception is the cipher, and it is the daemon's to enforce rather than
   * this form's: changing it changes what a password *is*, so olr regenerates
   * one and the plan comes back disruptive. That is why the confirmation dialog
   * has to show `password_generated` rather than treating it as detail.
   */
  settings: (fields: Record<string, unknown>): RemoteChangeRequest => ({
    method: 'PATCH',
    path: `${base}/shadowsocks/config`,
    body: fields,
    label: 'Saved',
  }),
  enabled: (enabled: boolean): RemoteChangeRequest => ({
    method: 'PATCH',
    path: `${base}/shadowsocks/config`,
    body: { enabled },
    label: enabled ? 'The proxy is on' : 'The proxy is off',
  }),
}

/**
 * Fetch the client link, on purpose and never in the background.
 *
 * A mutation rather than a query, which is not a workaround — it is the point.
 * **The response is the credential.** A query would prefetch it, cache it, and
 * refetch it on window focus, which would put a password in memory and in the
 * devtools network log of anybody who merely opened this page. Asking for it has
 * to be an act.
 *
 * The daemon answers 409 rather than 500 when there is no link to give (no
 * password yet, or no endpoint set): nothing is broken, the configuration is
 * simply not far enough along, and the message says which half is missing.
 */
export function useShadowsocksLink() {
  return useMutation({
    mutationFn: () => api.get<ProxyLink>(`${base}/shadowsocks/link`),
  })
}

/**
 * Re-render the proxy's configuration and restart it from stored intent.
 *
 * The repair path, and it has a different job from the tunnel's: a file on disk
 * can be edited by hand or lost, and this is what puts it back. Needs no
 * confirmation, because the intent being re-applied is intent the operator
 * stored earlier.
 */
export function useReapplyShadowsocks() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: () => api.send<ProxyApplyResult>('POST', `${base}/shadowsocks/apply`, undefined),
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['remote'] }),
  })
}
