import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api } from '@/lib/api'
import type { InterfaceList, LinkApplyResult } from '@/lib/api-types'
import type { LinkConfig } from '@/lib/config-types'

// Polling, for the reason features/dhcp/queries.ts gives about /api/events.
const OBSERVED_REFETCH_MS = 5000

export const linkKeys = {
  config: ['link', 'config'] as const,
  interfaces: ['link', 'interfaces'] as const,
}

export function useLinkConfig() {
  return useQuery({
    queryKey: linkKeys.config,
    queryFn: () => api.get<LinkConfig>('/api/link/config'),
  })
}

/**
 * The machine's interfaces, joined to what has been adopted.
 *
 * Refetched on an interval because the observed half is genuinely live: a cable
 * going in, an address arriving from an upstream DHCP server, a USB adapter
 * being plugged in. An operator standing at the box plugging things in is a
 * real way this screen gets used, and a stale list there is worse than useless.
 */
export function useInterfaces() {
  return useQuery({
    queryKey: linkKeys.interfaces,
    queryFn: () => api.get<InterfaceList>('/api/link/interfaces'),
    refetchInterval: OBSERVED_REFETCH_MS,
  })
}

/**
 * Adopts or releases interfaces.
 *
 * No plan preview beforehand, unlike every dhcp write. This used to be
 * justified as "this module's plan is always `impact: none`", which stopped
 * being true the day `link` grew networks: the same endpoint writes addresses
 * to the kernel now, and its plan reports `disruptive` when it does.
 *
 * The narrower claim still holds and is the one to state: an *adoption-only*
 * change touches nothing on the box, so its plan is empty and the round trip
 * could only ever return "go ahead". That is a claim about the body rather than
 * about the module — it holds only while the caller sends the stored networks
 * back unchanged, which is exactly what features/link/interfaces-card failed to
 * do. Anything that changes a network belongs on useNetworkEditor's path, which
 * plans first and stops when the plan is disruptive.
 *
 * The one consequence worth warning about — a pool that will stop validating —
 * is not in this module's plan at all and is joined client-side by the card.
 */
export function useApplyLinkConfig() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (config: LinkConfig) => api.put<LinkApplyResult>('/api/link/config', config),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['link'] })
      // dhcp too: adopting is what makes its pool form stop rejecting an
      // interface, and releasing is what makes an existing pool invalid. The
      // screen shows both, so writing one without re-reading the other leaves
      // a page contradicting itself.
      queryClient.invalidateQueries({ queryKey: ['dhcp'] })
    },
  })
}
