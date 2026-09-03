import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api } from '@/lib/api'
import type { DnsApplyResult, DnsNames, DnsPlan, DnsQueries, DnsStatus } from '@/lib/api-types'
import type { DnsConfig } from '@/lib/config-types'

// Polling, not /api/events, for the reason given in features/dhcp/queries.ts:
// EventSource cannot send an Authorization header.
const OBSERVED_REFETCH_MS = 5000

/**
 * How many observed rows to pull per poll.
 *
 * The relay holds QueryLog.entries — 5000 by default — and a page fetching all
 * of them every five seconds would move most of a megabyte per poll to render a
 * screenful. `stats.held` still carries the true total, so the page can say how
 * many it is not showing rather than implying a quiet network.
 */
const OBSERVED_LIMIT = 200

export const dnsKeys = {
  config: ['dns', 'config'] as const,
  status: ['dns', 'status'] as const,
  queries: ['dns', 'queries'] as const,
  names: ['dns', 'names'] as const,
}

export function useDnsConfig() {
  return useQuery({
    queryKey: dnsKeys.config,
    queryFn: () => api.get<DnsConfig>('/api/dns/config'),
  })
}

export function useDnsStatus() {
  return useQuery({
    queryKey: dnsKeys.status,
    queryFn: () => api.get<DnsStatus>('/api/dns/status'),
    // Observed state, never cached by the daemon (design.md §4.5), so the only
    // way to stay current is to ask again.
    refetchInterval: OBSERVED_REFETCH_MS,
  })
}

/**
 * The query log.
 *
 * `enabled` gates the poll on the *card being shown*, not on the log being
 * turned on: a relay that is not answering is itself the most useful thing this
 * endpoint can report (it 503s), and suppressing the request would replace that
 * with an empty list.
 */
export function useDnsQueries(enabled = true) {
  return useQuery({
    queryKey: dnsKeys.queries,
    queryFn: () => api.get<DnsQueries>(`/api/dns/queries?limit=${OBSERVED_LIMIT}`),
    refetchInterval: OBSERVED_REFETCH_MS,
    enabled,
    // A 503 means the relay is down, which is a state to report and not a
    // transient to retry into a spinner.
    retry: false,
  })
}

/** The domain→address map the relay observed. */
export function useDnsNames(enabled = true) {
  return useQuery({
    queryKey: dnsKeys.names,
    queryFn: () => api.get<DnsNames>(`/api/dns/names?limit=${OBSERVED_LIMIT}`),
    refetchInterval: OBSERVED_REFETCH_MS,
    enabled,
    retry: false,
  })
}

/**
 * Asks what a config would do without doing it.
 *
 * This is the API's dry run (design.md §5.1) and the reason the UI can warn
 * "every device loses name resolution" instead of showing a spinner and hoping.
 */
export function useDnsPlanPreview() {
  return useMutation({
    mutationFn: (config: DnsConfig) => api.post<DnsPlan>('/api/dns/plan', config),
  })
}

/**
 * Applies a whole config.
 *
 * Changes take effect on return — there is no staged commit to follow up with
 * (design.md §5.1), which is why the UI confirms *before* calling this rather
 * than offering to undo afterwards.
 */
export function useApplyDnsConfig() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (config: DnsConfig) => api.put<DnsApplyResult>('/api/dns/config', config),
    onSettled: () => {
      // Invalidated on failure too. A partial apply changed the system, so
      // every observed answer is stale whether or not the call succeeded.
      queryClient.invalidateQueries({ queryKey: ['dns'] })
    },
  })
}
