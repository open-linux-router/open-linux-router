import { useState } from 'react'
import { toast } from 'sonner'

import { useApplyIngressChange, type IngressChange } from '@/features/ingress/queries'
import { ApiError } from '@/lib/api'
import type { IngressApplyResult, IngressPlan } from '@/lib/api-types'

/**
 * The apply interaction for the ingress screen.
 *
 * design.md §5.1 says the GUI applies instantly with no "Apply changes" bar;
 * §5.3.3 says it should be able to warn rather than spin. The two resolve as
 * *apply immediately unless the plan comes back disruptive* — and the daemon is
 * what decides that. Every change is one request. It lands, unless it would take
 * a published name away, in which case the daemon writes nothing and answers 409
 * with the plan; the page shows it, and sending the same change with
 * `confirm=true` goes ahead.
 *
 * What `disruptive` means here is narrower than on the gateway screen and
 * sharper than on the firewall's: a URL somebody may have open, bookmarked, or
 * configured into another application stops resolving to anything. Reloading
 * does not help, which is what separates it from the `restart` rung above it.
 *
 * It cannot disconnect the operator from this UI. The API listener is a separate
 * port that this module never touches, deliberately and permanently
 * (docs/ingress.md §6) — so even publishing the WebUI through the proxy and then
 * breaking the proxy leaves a way back in.
 */
export function useIngressApply() {
  const apply = useApplyIngressChange()

  /** A change the daemon held back because it would be disruptive. */
  const [confirming, setConfirming] = useState<{
    change: IngressChange
    plan: IngressPlan
  } | null>(null)

  /** The steps of the last failed apply, which the page keeps on screen. */
  const [failure, setFailure] = useState<IngressApplyResult | null>(null)

  async function send(change: IngressChange, confirm: boolean) {
    setFailure(null)
    try {
      const result = await apply.mutateAsync({ change, confirm })
      toast.success(result.plan.empty ? 'Nothing to change' : change.label)
      return true
    } catch (error) {
      if (!(error instanceof ApiError)) {
        toast.error(String(error))
        return false
      }

      const body = error.body as IngressApplyResult | undefined
      const plan = body?.plan

      if (error.status === 409 && plan?.impact === 'disruptive') {
        // Nothing was written. The same change, sent again with confirm, is what
        // goes ahead — which is why a change is a value and not a call.
        setConfirming({ change, plan })
        return false
      }

      if (error.status === 503) {
        // There is no proxy on this box. The daemon's message is the long one
        // with both ways to get a binary in it, so it is shown rather than
        // summarised — this is the single error where the text *is* the fix.
        toast.error('No proxy is installed', { description: error.message })
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
    submit: (change: IngressChange) => send(change, false),
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
