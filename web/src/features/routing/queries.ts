import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api } from '@/lib/api'
import type { RoutingApplyResult, RoutingPlan, RoutingStatus } from '@/lib/api-types'
import type { RoutingConfig } from '@/lib/config-types'

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

export const routingKeys = {
  config: ['routing', 'config'] as const,
  status: ['routing', 'status'] as const,
}

export function useRoutingConfig() {
  return useQuery({
    queryKey: routingKeys.config,
    queryFn: () => api.get<RoutingConfig>('/api/routing/config'),
  })
}

export function useRoutingStatus() {
  return useQuery({
    queryKey: routingKeys.status,
    queryFn: () => api.get<RoutingStatus>('/api/routing/status'),
    // Observed state, never cached by the daemon (design.md §4.5), so the only
    // way to stay current is to ask again.
    refetchInterval: OBSERVED_REFETCH_MS,
  })
}

/**
 * Asks what a config would do without doing it.
 *
 * The API's dry run (design.md §5.1), and on this module it earns its round
 * trip twice over: it is what lets the UI say "this would disconnect you from
 * the router" before rather than after.
 */
export function useRoutingPlanPreview() {
  return useMutation({
    mutationFn: (config: RoutingConfig) => api.post<RoutingPlan>('/api/routing/plan', config),
  })
}

export function useApplyRoutingConfig() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (config: RoutingConfig) =>
      api.put<RoutingApplyResult>('/api/routing/config', config),
    onSettled: () => {
      // Invalidated on failure too. A partial apply changed the kernel, so
      // every observed answer is stale whether or not the call succeeded.
      queryClient.invalidateQueries({ queryKey: ['routing'] })
    },
  })
}

/**
 * Re-applies stored intent unchanged, which is the repair path design.md
 * §5.3.2 offers in place of a rollback: if an apply failed halfway, or somebody
 * ran `ip rule del` by hand, this finishes the job.
 */
export function useReapplyRouting() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: () => api.post<RoutingApplyResult>('/api/routing/apply', undefined),
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['routing'] }),
  })
}
