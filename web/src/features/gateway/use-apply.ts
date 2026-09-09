import { useState } from 'react'
import { toast } from 'sonner'

import { useApplyGatewayChange, type GatewayChange } from '@/features/gateway/queries'
import { ApiError } from '@/lib/api'
import type { GatewayApplyResult, GatewayPlan } from '@/lib/api-types'

/**
 * The apply interaction for the gateway screen.
 *
 * design.md §5.1 says the GUI applies instantly with no "Apply changes" bar;
 * §5.3.3 says it should be able to warn rather than spin. The two resolve as
 * *apply immediately unless the plan comes back disruptive* — and the daemon is
 * what decides that now. Every change is one request. It lands, unless it would
 * move traffic that is flowing, in which case the daemon writes nothing and
 * answers 409 with the plan; the page shows it, and sending the same change with
 * `confirm=true` goes ahead.
 *
 * This used to ask /plan first and then decide here. Two things were wrong with
 * that. Every harmless edit paid for a round trip that only the dangerous ones
 * need — and the decision about what is dangerous lived in the client, where the
 * CLI could not reach it and a second client would have had to reimplement it.
 *
 * What `disruptive` can mean here is worth restating: on the addresses screen the
 * worst case is a client losing its lease, but here it can be the operator losing
 * the connection they are making the change over, which is the one outcome no
 * amount of clicking again will fix.
 */
export function useGatewayApply() {
  const apply = useApplyGatewayChange()

  /** A change the daemon held back because it would be disruptive. */
  const [confirming, setConfirming] = useState<{ change: GatewayChange; plan: GatewayPlan } | null>(
    null,
  )

  /** The steps of the last failed apply, which the page keeps on screen. */
  const [failure, setFailure] = useState<GatewayApplyResult | null>(null)

  /** A refusal — something else is managing gateway on this box (§6). */
  const [blocked, setBlocked] = useState<GatewayPlan | null>(null)

  async function send(change: GatewayChange, confirm: boolean) {
    setBlocked(null)
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

      const body = error.body as GatewayApplyResult | undefined
      const plan = body?.plan

      // 409 means two different things and the body is what tells them apart.
      if (error.status === 409 && plan?.blocked) {
        // Not a dialog: this is not a decision the operator can make here, it
        // is a conflict they have to go and resolve in another program's
        // configuration file.
        setBlocked(plan)
        return false
      }
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
        description: error.problems.map((p) => `${p.path ?? ''} ${p.message}`.trim()).join('\n') || undefined,
      })
      return false
    }
  }

  return {
    submit: (change: GatewayChange) => send(change, false),
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
    blocked,
    dismissBlocked: () => setBlocked(null),
    busy: apply.isPending,
  }
}

function describe(plan: GatewayPlan): string {
  if (plan.empty) return 'Nothing to change'
  if (plan.impact === 'reload') {
    // Worth naming: connections already open keep the exit they started on,
    // which is exactly what the ct-mark save and restore pair buys.
    return 'Applied — open connections kept their current route'
  }
  return 'Applied'
}
