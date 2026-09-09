import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api } from '@/lib/api'
import type { GatewayApplyResult, GatewayStatus, GatewayTraffic } from '@/lib/api-types'
import type { GatewayConfig } from '@/lib/config-types'

// The same polling story as the other modules: EventSource cannot send an
// Authorization header, so streaming from the browser needs a cookie session or
// a fetch-based reader, and neither is worth inventing before there is live
// data to carry.
//
// This screen has one reason to poll the others do not: an exit's health is
// decided by a background prober inside olrd, so the answer can change with
// nobody having clicked anything. "Living Room TV: no internet — Clash is down"
// is only useful if it appears on its own.
const OBSERVED_REFETCH_MS = 5000

// Byte counts refetch at half that rate, because they are not the same question.
//
// An exit going down is worth knowing within seconds: nothing else on screen
// would say so. A running total is worth knowing whenever the eye lands on it,
// and producing it costs a walk of every device on the network. Polling both at
// one interval meant paying for the expensive one at the cadence the cheap one
// needed.
const TRAFFIC_REFETCH_MS = 10000

export const gatewayKeys = {
  config: ['gateway', 'config'] as const,
  status: ['gateway', 'status'] as const,
  traffic: ['gateway', 'traffic'] as const,
}

export function useGatewayConfig() {
  return useQuery({
    queryKey: gatewayKeys.config,
    queryFn: () => api.get<GatewayConfig>('/api/gateway/config'),
  })
}

export function useGatewayStatus() {
  return useQuery({
    queryKey: gatewayKeys.status,
    queryFn: () => api.get<GatewayStatus>('/api/gateway/status'),
    // Observed state, never cached by the daemon (design.md §4.5), so the only
    // way to stay current is to ask again.
    refetchInterval: OBSERVED_REFETCH_MS,
  })
}

/**
 * Per-device byte counts, read from the kernel on every request.
 *
 * Its own query rather than part of the status one, because it costs a walk of
 * every device on the network and is wanted at a different rate.
 */
export function useGatewayTraffic() {
  return useQuery({
    queryKey: gatewayKeys.traffic,
    queryFn: () => api.get<GatewayTraffic>('/api/gateway/traffic'),
    refetchInterval: TRAFFIC_REFETCH_MS,
  })
}

/**
 * One change, described the way the API names it.
 *
 * A value rather than a call, because a change has to survive being held: when
 * the daemon answers 409 because the change would move traffic that is flowing,
 * the page shows the plan and then sends *the same change* again with
 * `confirm=true`. Replaying is only simple if the change is data.
 */
export interface GatewayChange {
  method: 'PUT' | 'DELETE' | 'PATCH'
  path: string
  body?: unknown
}

const base = '/api/gateway'
const seg = (s: string) => encodeURIComponent(s)

/**
 * The changes this screen can make, each naming the thing it changes.
 *
 * The split is not arbitrary. RFC 7386 merges an object key by key but replaces
 * an array wholesale, so the scalars go through PATCH and the lists get a route
 * per item. It is also what keeps the rules on the daemon's side: `saveExit`
 * posts the new exit to the *old* name's path, and renaming every reference
 * along with it is Config.Rename's job, not this file's.
 */
export const gatewayChange = {
  saveExit: (name: string, exit: unknown): GatewayChange => ({
    method: 'PUT',
    path: `${base}/exits/${seg(name)}`,
    body: exit,
  }),
  removeExit: (name: string): GatewayChange => ({
    method: 'DELETE',
    path: `${base}/exits/${seg(name)}`,
  }),
  assign: (iface: string, exit: string): GatewayChange => ({
    method: 'PUT',
    path: `${base}/assignments/${seg(iface)}`,
    body: { exit },
  }),
  settings: (fields: Partial<GatewayConfig>): GatewayChange => ({
    method: 'PATCH',
    path: `${base}/config`,
    body: fields,
  }),
}

/**
 * Sends one change, optionally already confirmed.
 *
 * There is no separate preview call. The daemon plans before it writes and
 * refuses on its own when the plan is disruptive, so the round trip that used to
 * be spent asking is only spent when the answer is "ask the operator" — which on
 * this screen is the one answer worth waiting for. Every other edit lands first
 * time.
 */
export function useApplyGatewayChange() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: ({ change, confirm }: { change: GatewayChange; confirm?: boolean }) =>
      api.send<GatewayApplyResult>(
        change.method,
        confirm ? `${change.path}?confirm=true` : change.path,
        change.body,
      ),
    onSettled: () => {
      // Invalidated on failure too. A partial apply changed the kernel, so
      // every observed answer is stale whether or not the call succeeded.
      queryClient.invalidateQueries({ queryKey: ['gateway'] })
    },
  })
}

/**
 * Re-applies stored intent unchanged, which is the repair path design.md
 * §5.3.2 offers in place of a rollback: if an apply failed halfway, or somebody
 * ran `ip rule del` by hand, this finishes the job.
 */
export function useReapplyGateway() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: () => api.post<GatewayApplyResult>('/api/gateway/apply', undefined),
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['gateway'] }),
  })
}
