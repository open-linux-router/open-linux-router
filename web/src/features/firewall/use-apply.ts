import { useState } from 'react'
import { toast } from 'sonner'

import { useApplyFirewallChange, type FirewallChange } from '@/features/firewall/queries'
import { ApiError } from '@/lib/api'
import type { FirewallApplyResult, FirewallPlan } from '@/lib/api-types'

/**
 * The apply interaction for the port-forwarding screen.
 *
 * design.md §5.1 says the GUI applies instantly with no "Apply changes" bar;
 * §5.3.3 says it should be able to warn rather than spin. The two resolve as
 * *apply immediately unless the plan comes back disruptive* — and the daemon is
 * what decides that. Every change is one request. It lands, unless it would
 * break connections that are open right now, in which case the daemon writes
 * nothing and answers 409 with the plan; the page shows it, and sending the same
 * change with `confirm=true` goes ahead.
 *
 * What `disruptive` means here is narrower than on the gateway screen, and the
 * dialog says so: removing or editing a forward stops translating connections
 * still running through it, so they die mid-stream rather than being refused.
 * It cannot disconnect the operator from the router — nothing here moves the
 * path of traffic the router itself is carrying.
 *
 * There is no `blocked` state either, which is the other difference from
 * gateway. A foreign filter on the forward hook is reported on the plan and
 * never refuses the change (docs/firewall.md §5.2), because it may well be
 * accepting exactly this traffic in a rule olr cannot evaluate.
 */
export function useFirewallApply() {
  const apply = useApplyFirewallChange()

  /** A change the daemon held back because it would be disruptive. */
  const [confirming, setConfirming] = useState<{
    change: FirewallChange
    plan: FirewallPlan
  } | null>(null)

  /** The steps of the last failed apply, which the page keeps on screen. */
  const [failure, setFailure] = useState<FirewallApplyResult | null>(null)

  async function send(change: FirewallChange, confirm: boolean) {
    setFailure(null)
    try {
      const result = await apply.mutateAsync({ change, confirm })
      toast.success(describe(result.plan))
      return true
    } catch (error) {
      if (!(error instanceof ApiError)) {
        toast.error(String(error))
        return false
      }

      const body = error.body as FirewallApplyResult | undefined
      const plan = body?.plan

      if (error.status === 409 && plan?.impact === 'disruptive') {
        // Nothing was written. The same change, sent again with confirm, is
        // what goes ahead — which is why a change is a value and not a call.
        setConfirming({ change, plan })
        return false
      }

      if (plan && !plan.known) {
        toast.error('The kernel could not be read, so nothing was changed', {
          description: 'On Linux this usually means olrd is missing CAP_NET_ADMIN.',
        })
        return false
      }
      if (body?.steps?.length) {
        // A failed apply still changed things (§5.3.2 — no rollback, the steps
        // that landed stay landed). Keep the body so the page can show which
        // ones did; re-applying picks up where this left off.
        setFailure(body)
      }
      toast.error(error.message, {
        description:
          error.problems.map((p) => `${p.path ?? ''} ${p.message}`.trim()).join('\n') || undefined,
      })
      return false
    }
  }

  return {
    submit: (change: FirewallChange) => send(change, false),
    confirming,
    confirm: async () => {
      if (!confirming) return
      const { change } = confirming
      setConfirming(null)
      await send(change, true)
    },
    cancel: () => setConfirming(null),
    failure,
    dismissFailure: () => setFailure(null),
    busy: apply.isPending,
  }
}

function describe(plan: FirewallPlan): string {
  if (plan.empty) return 'Nothing to change'
  return 'Applied'
}
