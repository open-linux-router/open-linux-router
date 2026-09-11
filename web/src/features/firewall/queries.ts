import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api } from '@/lib/api'
import type { FirewallApplyResult, FirewallStatus } from '@/lib/api-types'
import type { FirewallConfig } from '@/lib/config-types'

// The same polling story as the other modules: EventSource cannot send an
// Authorization header, so streaming from the browser needs a cookie session or
// a fetch-based reader, and neither is worth inventing before there is live data
// to carry.
//
// This screen polls for one reason of its own: the counter beside each forward
// is the answer to "is anything actually arriving?", and an operator who has
// just opened a port on their router is about to go and test it from their
// phone. A number that only moves on reload would make them reload.
const OBSERVED_REFETCH_MS = 5000

export const firewallKeys = {
  config: ['firewall', 'config'] as const,
  status: ['firewall', 'status'] as const,
}

export function useFirewallConfig() {
  return useQuery({
    queryKey: firewallKeys.config,
    queryFn: () => api.get<FirewallConfig>('/api/firewall/config'),
  })
}

export function useFirewallStatus() {
  return useQuery({
    queryKey: firewallKeys.status,
    queryFn: () => api.get<FirewallStatus>('/api/firewall/status'),
    // Observed state, never cached by the daemon (design.md §4.5), so the only
    // way to stay current is to ask again.
    refetchInterval: OBSERVED_REFETCH_MS,
  })
}

/**
 * One change, described the way the API names it.
 *
 * A value rather than a call, because a change has to survive being held: when
 * the daemon answers 409 because the change would break connections that are
 * open now, the page shows the plan and then sends *the same change* again with
 * `confirm=true`. Replaying is only simple if the change is data.
 */
export interface FirewallChange {
  method: 'PUT' | 'DELETE' | 'PATCH'
  path: string
  body?: unknown
}

const base = '/api/firewall'
const seg = (s: string) => encodeURIComponent(s)

/**
 * The changes this screen can make, each naming the thing it changes.
 *
 * The split is not arbitrary. RFC 7386 merges an object key by key but replaces
 * an array wholesale, so `enabled` goes through PATCH and the forwards get a
 * route per item. It is also what keeps the rules on the daemon's side:
 * `saveForward` posts to the *old* name's path, and keeping the slot — and so
 * the counter — across a rename is Config.Upsert's job, not this file's.
 */
export const firewallChange = {
  saveForward: (name: string, forward: unknown): FirewallChange => ({
    method: 'PUT',
    path: `${base}/forwards/${seg(name)}`,
    body: forward,
  }),
  removeForward: (name: string): FirewallChange => ({
    method: 'DELETE',
    path: `${base}/forwards/${seg(name)}`,
  }),
  settings: (fields: Partial<FirewallConfig>): FirewallChange => ({
    method: 'PATCH',
    path: `${base}/config`,
    body: fields,
  }),
}

/**
 * Sends one change, optionally already confirmed.
 *
 * There is no separate preview call. The daemon plans before it writes and
 * refuses on its own when the plan is disruptive, so the round trip that would
 * be spent asking is only spent when the answer is "ask the operator". Every
 * other edit lands first time.
 */
export function useApplyFirewallChange() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: ({ change, confirm }: { change: FirewallChange; confirm?: boolean }) =>
      api.send<FirewallApplyResult>(
        change.method,
        confirm ? `${change.path}?confirm=true` : change.path,
        change.body,
      ),
    onSettled: () => {
      // Invalidated on failure too: a partial apply changed the kernel, so every
      // observed answer is stale whether or not the call succeeded.
      queryClient.invalidateQueries({ queryKey: ['firewall'] })
    },
  })
}
