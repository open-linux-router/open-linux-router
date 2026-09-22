import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { api } from '@/lib/api'
import type { DialApplyResult, Plan, Uplink, UplinkResponse } from '@/lib/api-types'

// Polling, for the reason features/dhcp/queries.ts gives about /api/events.
// It earns it harder here than elsewhere: half of what this endpoint returns is
// read from the kernel per request — the address on the interface and the
// default route actually in the main table — and both change without olr.
const OBSERVED_REFETCH_MS = 5000

export const dialKeys = {
  uplink: ['dial', 'uplink'] as const,
}

export function useUplink() {
  return useQuery({
    queryKey: dialKeys.uplink,
    queryFn: () => api.get<UplinkResponse>('/api/dial/uplink'),
    refetchInterval: OBSERVED_REFETCH_MS,
  })
}

/**
 * Writing the uplink, with the confirmation a route change needs.
 *
 * The dhcp/dns pattern rather than the adoption one, and for a sharper reason
 * than either: this endpoint replaces the default route in the main table. If
 * the operator is reaching this router from outside the house, the request that
 * changes the route is arriving over the route being changed.
 *
 * So: plan first, and when the plan comes back disruptive, stop and show it.
 * §5.5's lockout guard — the dead-man's switch that reverts a change nobody
 * confirms — is **not built**. This dialog and the warning the server attaches
 * to the plan are the entire safety net, which is why it refuses to be a
 * toast-and-continue.
 */
export function useUplinkEditor() {
  const uplink = useUplink()
  const queryClient = useQueryClient()

  const [pending, setPending] = useState<{ uplink: Uplink | null; plan: Plan } | null>(null)
  const [failure, setFailure] = useState<DialApplyResult | null>(null)

  const apply = useMutation({
    mutationFn: (next: Uplink | null) =>
      next === null
        ? api.delete<DialApplyResult>('/api/dial/uplink')
        : api.put<DialApplyResult>('/api/dial/uplink', next),
    onSuccess: (result) => {
      // A partial apply is reported, never swallowed. There is no rollback
      // (§5.2), so "which steps ran" is the only way back to a known state —
      // and here a half-applied change can mean an address that landed and a
      // route that did not.
      if (result.error || result.steps?.some((s) => s.error)) setFailure(result)
      setPending(null)
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['dial'] })
      // link too: the interfaces list says which NIC is the uplink, and it
      // reads that from here.
      queryClient.invalidateQueries({ queryKey: ['link'] })
      // gateway too: its landing page tells the operator where the box's own
      // way out is configured, and whether there is one.
      queryClient.invalidateQueries({ queryKey: ['gateway'] })
    },
  })

  /**
   * Plans the change, then applies it or stops.
   *
   * The plan goes to `/api/dial/plan` with the whole document, because a plan
   * is a diff and a diff needs both sides — so the records have to travel with
   * it or removing every record would be part of what is previewed.
   */
  async function submit(next: Uplink | null) {
    const current = await api.get<{ records?: unknown[] }>('/api/dial/config')
    const plan = await api.post<Plan>('/api/dial/plan', {
      ...current,
      uplink: next ?? undefined,
    })
    if (plan.impact === 'disruptive') {
      setPending({ uplink: next, plan })
      return
    }
    apply.mutate(next)
  }

  return {
    uplink: uplink.data?.uplink,
    isPending: uplink.isPending,
    error: uplink.error as Error | null,
    busy: apply.isPending,

    save: (next: Uplink) => submit(next),
    remove: () => submit(null),

    pending,
    confirm: () => pending && apply.mutate(pending.uplink),
    cancel: () => setPending(null),

    failure,
    dismissFailure: () => setFailure(null),
  }
}
