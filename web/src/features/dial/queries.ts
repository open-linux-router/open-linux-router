import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'

import { ApiError, api } from '@/lib/api'
import type {
  DialApplyResult,
  LinkApplyResult,
  Plan,
  Uplink,
  UplinkResponse,
} from '@/lib/api-types'
import type { LinkConfig } from '@/lib/config-types'

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

  const [pending, setPending] = useState<{
    uplink: Uplink | null
    plan: Plan
    replaced?: string
  } | null>(null)
  const [failure, setFailure] = useState<DialApplyResult | null>(null)

  const apply = useMutation({
    mutationFn: (next: Uplink | null) =>
      next === null
        ? api.delete<DialApplyResult>('/api/dial/uplink')
        : api.put<DialApplyResult>('/api/dial/uplink', next),
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
  async function submit(next: Uplink | null, replacing?: string) {
    try {
      if (replacing) await handOver(replacing)
      const current = await api.get<{ records?: unknown[] }>('/api/dial/config')
      const plan = await api.post<Plan>('/api/dial/plan', {
        ...current,
        uplink: next ?? undefined,
      })
      if (plan.impact === 'disruptive') {
        setPending({ uplink: next, plan, replaced: replacing })
        return
      }
    } catch (error) {
      reportError(error)
      return
    }
    await commit(next, replacing)
  }

  /**
   * Removes the network on the interface the uplink is taking over, keeping
   * its address on the interface.
   *
   * First, because the server refuses an uplink on an interface a network
   * carries — two owners for one interface's addressing is the state link's
   * writer cannot survive. `keep_addresses` is what makes it safe to do from a
   * browser that is very likely connected through that address: without it,
   * removing the network takes the address with it, and the request that sets
   * the uplink never arrives.
   *
   * Not undone if what follows is refused or cancelled. Nothing in olr can put
   * a network back as it was (§5.2), and what is left — the address still on
   * the interface, no network on it — is the state the uplink is then set on
   * by trying again.
   */
  async function handOver(network: string) {
    const link = await api.get<LinkConfig>('/api/link/config')
    await api.put<LinkApplyResult>('/api/link/config?keep_addresses=true', {
      ...link,
      networks: (link.networks ?? []).filter((n) => n.name !== network),
    })
    // The page's network list, and the ranges that may have been on it.
    await queryClient.invalidateQueries({ queryKey: ['link'] })
    await queryClient.invalidateQueries({ queryKey: ['dhcp'] })
  }

  /**
   * Applies, and says how it went — every outcome, the dhcp way.
   *
   * The dialog closes on submit, before any of this has answered, so an outcome
   * that is not reported here is not reported at all: a refused PUT used to look
   * exactly like a click that did nothing.
   */
  async function commit(next: Uplink | null, replaced?: string) {
    setFailure(null)
    try {
      const result = await apply.mutateAsync(next)
      // A partial apply is reported, never swallowed. There is no rollback
      // (§5.2), so "which steps ran" is the only way back to a known state —
      // and here a half-applied change can mean an address that landed and a
      // route that did not.
      if (result.error || result.steps?.some((s) => s.error)) {
        setFailure(result)
        return
      }
      toast.success(
        next === null ? 'olr no longer owns the way out' : `Uplink set on ${next.interface}`,
        replaced ? { description: `The network ${replaced} was removed to make way for it.` } : undefined,
      )
    } catch (error) {
      // A half-applied change arrives as a 500 whose body still carries the
      // steps that landed; keep them, for the same reason as above.
      const body = error instanceof ApiError ? (error.body as DialApplyResult | undefined) : undefined
      if (body?.steps?.length) setFailure(body)
      reportError(error)
    }
  }

  return {
    uplink: uplink.data?.uplink,
    isPending: uplink.isPending,
    error: uplink.error as Error | null,
    busy: apply.isPending,

    save: (next: Uplink, replacing?: string) => submit(next, replacing),
    remove: () => submit(null),

    pending,
    confirm: async () => {
      if (!pending) return
      setPending(null)
      await commit(pending.uplink, pending.replaced)
    },
    cancel: () => setPending(null),

    failure,
    dismissFailure: () => setFailure(null),
  }
}

function reportError(error: unknown) {
  if (error instanceof ApiError) {
    toast.error(error.message, {
      description: error.problems.map((p) => p.message).join('\n') || undefined,
    })
  } else {
    toast.error(String(error))
  }
}
