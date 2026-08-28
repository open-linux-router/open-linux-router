import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api } from '@/lib/api'
import type { DhcpLeases, DhcpStatus, ApplyResult, Plan } from '@/lib/api-types'
import type { DhcpConfig } from '@/lib/config-types'

// The UI polls rather than subscribing to /api/events.
//
// Not an oversight: EventSource cannot send an Authorization header, so
// streaming from the browser needs either a cookie session or a fetch-based
// reader, and neither is worth inventing before there is live data to carry.
// Today the only event is "something was applied", which a refetch after a
// mutation already covers. When link lands and throughput arrives, this is the
// place that changes.
const OBSERVED_REFETCH_MS = 5000

export const dhcpKeys = {
  config: ['dhcp', 'config'] as const,
  status: ['dhcp', 'status'] as const,
  leases: ['dhcp', 'leases'] as const,
}

export function useDhcpConfig() {
  return useQuery({
    queryKey: dhcpKeys.config,
    queryFn: () => api.get<DhcpConfig>('/api/dhcp/config'),
  })
}

export function useDhcpStatus() {
  return useQuery({
    queryKey: dhcpKeys.status,
    queryFn: () => api.get<DhcpStatus>('/api/dhcp/status'),
    // Observed state, never cached by the daemon (design.md §4.5), so the only
    // way to stay current is to ask again.
    refetchInterval: OBSERVED_REFETCH_MS,
  })
}

export function useDhcpLeases() {
  return useQuery({
    queryKey: dhcpKeys.leases,
    queryFn: () => api.get<DhcpLeases>('/api/dhcp/leases'),
    refetchInterval: OBSERVED_REFETCH_MS,
  })
}

/**
 * Asks what a config would do without doing it.
 *
 * This is the API's dry run (design.md §5.1) and the reason the UI can warn
 * "this will drop every LAN client" instead of showing a spinner and hoping.
 */
export function usePlanPreview() {
  return useMutation({
    mutationFn: (config: DhcpConfig) => api.post<Plan>('/api/dhcp/plan', config),
  })
}

/**
 * Applies a whole config.
 *
 * Changes take effect on return — there is no staged commit to follow up with
 * (design.md §5.1), which is why the UI confirms *before* calling this rather
 * than offering to undo afterwards.
 */
export function useApplyDhcpConfig() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (config: DhcpConfig) => api.put<ApplyResult>('/api/dhcp/config', config),
    onSettled: () => {
      // Invalidated on failure too. A partial apply changed the system, so
      // every observed answer is stale whether or not the call succeeded.
      queryClient.invalidateQueries({ queryKey: ['dhcp'] })
    },
  })
}
