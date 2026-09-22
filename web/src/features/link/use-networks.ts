import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'

import { ApiError, api } from '@/lib/api'
import type { InterfaceList, LinkApplyResult, Plan } from '@/lib/api-types'
import type { Group, LinkConfig } from '@/lib/config-types'

import { linkKeys } from './queries'

/**
 * Writing a network, with the confirmation step a network change now needs.
 *
 * `features/link/queries.ts` says its `useApplyLinkConfig` skips the plan
 * preview because "this module's plan is always `impact: none`". That stopped
 * being true when networks landed: adoption still touches nothing, but applying
 * a network writes an address to the interface, and renumbering one takes every
 * client's address away — quite possibly including the browser making the
 * request.
 *
 * So this is the dhcp/dns pattern rather than the adoption one: plan first, and
 * when the plan is disruptive, stop and show it. The same
 * instant-apply-unless-disruptive rule every other module follows.
 *
 * §5.5's lockout guard — the dead-man's switch that reverts a change nobody
 * confirms — is **not built**. This dialog and the warning the server attaches
 * to the plan are the entire safety net, which is why it refuses to be a
 * toast-and-continue.
 */
export function useNetworkEditor() {
  const config = useQuery({
    queryKey: linkKeys.config,
    queryFn: () => api.get<LinkConfig>('/api/link/config'),
  })
  const interfaces = useQuery({
    queryKey: linkKeys.interfaces,
    queryFn: () => api.get<InterfaceList>('/api/link/interfaces'),
    refetchInterval: 5000,
  })

  const queryClient = useQueryClient()
  const [pending, setPending] = useState<{ config: LinkConfig; plan: Plan } | null>(null)
  const [failure, setFailure] = useState<LinkApplyResult | null>(null)

  const apply = useMutation({
    mutationFn: (next: LinkConfig) => api.put<LinkApplyResult>('/api/link/config', next),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['link'] })
      // dhcp too: a network is what its ranges are validated against, so
      // creating or renumbering one changes whether they are still valid.
      queryClient.invalidateQueries({ queryKey: ['dhcp'] })
    },
  })

  async function submit(next: LinkConfig) {
    try {
      const plan = await api.post<Plan>('/api/link/plan', next)
      if (plan.impact === 'disruptive') {
        setPending({ config: next, plan })
        return
      }
    } catch (error) {
      reportError(error)
      return
    }
    await commit(next)
  }

  /**
   * Applies, and says how it went — every outcome, the dhcp way.
   *
   * The dialog closes on submit, before any of this has answered, so an outcome
   * that is not reported here is not reported at all: a refused change used to
   * look exactly like a click that did nothing.
   */
  async function commit(next: LinkConfig) {
    setFailure(null)
    try {
      const result = await apply.mutateAsync(next)
      // A partial apply is reported, never swallowed. There is no rollback
      // (§5.2), so "which steps ran" is the only way back to a known state.
      if (result.error || result.steps?.some((s) => s.error)) {
        setFailure(result)
        return
      }
      toast.success('Networks updated')
    } catch (error) {
      // A half-applied change arrives as a 500 whose body still carries the
      // steps that landed; keep them, for the same reason as above.
      const body = error instanceof ApiError ? (error.body as LinkApplyResult | undefined) : undefined
      if (body?.steps?.length) setFailure(body)
      reportError(error)
    }
  }

  return {
    config: config.data,
    groups: interfaces.data?.groups ?? [],
    interfaces: interfaces.data?.interfaces ?? [],
    problems: interfaces.data?.problems ?? [],
    isPending: config.isPending || interfaces.isPending,
    error: (config.error ?? interfaces.error) as Error | null,
    busy: apply.isPending,

    submit,
    save: (groups: Group[]) => submit({ ...(config.data ?? {}), groups }),

    pending,
    confirm: async () => {
      if (!pending) return
      setPending(null)
      await commit(pending.config)
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
