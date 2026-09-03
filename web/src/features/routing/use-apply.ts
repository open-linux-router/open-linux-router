import { useState } from 'react'
import { toast } from 'sonner'

import { useApplyRoutingConfig, useRoutingPlanPreview } from '@/features/routing/queries'
import { ApiError } from '@/lib/api'
import type { RoutingApplyResult, RoutingPlan } from '@/lib/api-types'
import type { RoutingConfig } from '@/lib/config-types'

/**
 * The apply interaction for the routing screen.
 *
 * Same shape as the addresses screen's, and for the same reason: design.md §5.1
 * says the GUI applies instantly with no "Apply changes" bar, §5.3.3 says it
 * should be able to warn rather than spin, and the two resolve as *apply
 * immediately unless the plan comes back disruptive*.
 *
 * What is different here is what `disruptive` can mean. On the addresses screen
 * the worst case is a client losing its lease; here it can be the operator
 * losing the connection they are making the change over, which is the one
 * outcome no amount of clicking again will fix. That is the whole reason every
 * edit round-trips through /plan first.
 */
export function useRoutingApply() {
  const preview = useRoutingPlanPreview()
  const apply = useApplyRoutingConfig()

  /** A change held back because it would be disruptive. */
  const [confirming, setConfirming] = useState<{ config: RoutingConfig; plan: RoutingPlan } | null>(
    null,
  )

  /** The steps of the last failed apply, which the page keeps on screen. */
  const [failure, setFailure] = useState<RoutingApplyResult | null>(null)

  /** A refusal — something else is managing routing on this box (§6). */
  const [blocked, setBlocked] = useState<RoutingPlan | null>(null)

  async function commit(config: RoutingConfig) {
    setFailure(null)
    try {
      const result = await apply.mutateAsync(config)
      toast.success(describe(result.plan))
      return true
    } catch (error) {
      if (error instanceof ApiError) {
        const body = error.body as RoutingApplyResult | undefined
        if (body?.plan?.blocked) {
          setBlocked(body.plan)
        } else if (body?.steps?.length) {
          // A failed apply still changed things (§5.3.2 — no rollback, the
          // steps that landed stay landed). Keep the body so the page can show
          // which ones did; re-applying picks up where this left off.
          setFailure(body)
        }
        toast.error(error.message, {
          description: error.problems.map((p) => p.message).join('\n') || undefined,
        })
      } else {
        toast.error(String(error))
      }
      return false
    }
  }

  /** Applies config, pausing for confirmation if the plan is disruptive. */
  async function submit(config: RoutingConfig) {
    setBlocked(null)
    try {
      const plan = await preview.mutateAsync(config)

      if (plan.blocked) {
        // Not a dialog: this is not a decision the operator can make here, it
        // is a conflict they have to go and resolve in another program's
        // configuration file.
        setBlocked(plan)
        return false
      }
      if (!plan.known) {
        toast.error('The kernel could not be read, so nothing was changed', {
          description: 'On Linux this usually means olrd is missing CAP_NET_ADMIN.',
        })
        return false
      }
      if (plan.empty) {
        toast.info('Nothing to change')
        return true
      }
      if (plan.impact === 'disruptive') {
        setConfirming({ config, plan })
        return false
      }
      return await commit(config)
    } catch (error) {
      if (error instanceof ApiError) {
        toast.error(error.message, {
          description:
            error.problems.map((p) => `${p.path ?? ''} ${p.message}`.trim()).join('\n') || undefined,
        })
      } else {
        toast.error(String(error))
      }
      return false
    }
  }

  async function confirm() {
    if (!confirming) return
    const { config } = confirming
    setConfirming(null)
    await commit(config)
  }

  return {
    submit,
    confirming,
    confirm,
    cancel: () => setConfirming(null),
    failure,
    dismissFailure: () => setFailure(null),
    blocked,
    dismissBlocked: () => setBlocked(null),
    busy: preview.isPending || apply.isPending,
  }
}

function describe(plan: RoutingPlan): string {
  if (plan.empty) return 'Nothing to change'
  if (plan.impact === 'reload') {
    // Worth naming: connections already open keep the exit they started on,
    // which is exactly what the ct-mark save and restore pair buys.
    return 'Applied — open connections kept their current route'
  }
  return 'Applied'
}
