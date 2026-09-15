import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { api } from '@/lib/api'
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
    onSuccess: (result) => {
      // A partial apply is reported, never swallowed. There is no rollback
      // (§5.2), so "which steps ran" is the only way back to a known state.
      if (result.error || result.steps?.some((s) => s.error)) setFailure(result)
      setPending(null)
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['link'] })
      // dhcp too: a network is what its ranges are validated against, so
      // creating or renumbering one changes whether they are still valid.
      queryClient.invalidateQueries({ queryKey: ['dhcp'] })
    },
  })

  async function submit(next: LinkConfig) {
    const plan = await api.post<Plan>('/api/link/plan', next)
    if (plan.impact === 'disruptive') {
      setPending({ config: next, plan })
      return
    }
    apply.mutate(next)
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
    confirm: () => pending && apply.mutate(pending.config),
    cancel: () => setPending(null),

    failure,
    dismissFailure: () => setFailure(null),
  }
}
