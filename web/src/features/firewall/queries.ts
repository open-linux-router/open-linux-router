import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api } from '@/lib/api'
import type { FirewallApplyResult, FirewallStatus } from '@/lib/api-types'

// Status is observed state — the blocked counters move on their own — so it
// polls like the other modules' status screens.
const OBSERVED_REFETCH_MS = 10_000

export const firewallKeys = {
  status: ['firewall', 'status'] as const,
}

export function useFirewallStatus() {
  return useQuery({
    queryKey: firewallKeys.status,
    queryFn: () => api.get<FirewallStatus>('/api/firewall/status'),
    refetchInterval: OBSERVED_REFETCH_MS,
  })
}

/**
 * Turns the firewall on or off.
 *
 * Without `confirm` the daemon holds a change that would cut off this browser's
 * own connection and answers 409 with the plan; the page shows the warning and
 * sends the same switch again confirmed.
 */
export function useSetFirewall() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ enabled, confirm }: { enabled: boolean; confirm?: boolean }) =>
      api.send<FirewallApplyResult>(
        'PATCH',
        confirm ? '/api/firewall/config?confirm=true' : '/api/firewall/config',
        { enabled },
      ),
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['firewall'] }),
  })
}

/** Programs the stored setting again, for a box whose rules somebody removed. */
export function useReapplyFirewall() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => api.send<FirewallApplyResult>('POST', '/api/firewall/apply'),
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['firewall'] }),
  })
}
