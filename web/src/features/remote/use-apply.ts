import { useState } from 'react'
import { toast } from 'sonner'

import { useApplyRemoteChange, type RemoteChangeRequest } from '@/features/remote/queries'
import { ApiError } from '@/lib/api'
import type { RemoteApplyResult, RemotePeerResult, RemotePlan } from '@/lib/api-types'

/**
 * The apply interaction for the remote-access screen.
 *
 * design.md §5.1 says the GUI applies instantly with no "Apply changes" bar;
 * §5.3.3 says it should be able to warn rather than spin. The two resolve as
 * *apply immediately unless the plan comes back disruptive* — and the daemon is
 * what decides that. Every change is one request. It lands, unless it would take
 * somebody's way into the network away, in which case the daemon writes nothing
 * and answers 409 with the plan; the page shows it, and sending the same change
 * with `confirm=true` goes ahead.
 *
 * What `disruptive` means here is sharper than on any other screen, and it is
 * worth naming because the dialog is what an operator reads before revoking
 * access: it is a device that can no longer get in, or a configuration already
 * on somebody's phone that is now wrong. Neither is fixed by trying again.
 *
 * It cannot disconnect the operator from this UI unless they are *using* the
 * tunnel to reach it — which is a real case and the one thing this hook cannot
 * detect, because a browser cannot see which route its own packets took.
 */
export function useRemoteApply() {
  const apply = useApplyRemoteChange()

  /** A change the daemon held back because it would be disruptive. */
  const [confirming, setConfirming] = useState<{
    change: RemoteChangeRequest
    plan: RemotePlan
  } | null>(null)

  /** The steps of the last failed apply, which the page keeps on screen. */
  const [failure, setFailure] = useState<RemoteApplyResult | null>(null)

  /**
   * The one-shot client configuration, held until the operator dismisses it.
   *
   * Not a toast, and not something the page can re-fetch. The private key in it
   * is stored nowhere, so this value existing in a React state field is the
   * only copy there is anywhere outside the device it is about to be imported
   * on. Dropping it on a re-render would cost the operator a device.
   */
  const [issued, setIssued] = useState<RemotePeerResult | null>(null)

  async function send(change: RemoteChangeRequest, confirm: boolean) {
    setFailure(null)
    try {
      const result = await apply.mutateAsync({ change, confirm })
      if (result.peer?.client_config) {
        // Held on screen rather than announced. A toast that disappears would
        // take the only copy of a private key with it.
        setIssued(result.peer)
      } else if (result.peer?.note) {
        // An edit only the device can apply — narrowing what it sends home
        // changes no kernel state, and the note carries the line to change
        // there (docs/remote-access.md §6.2).
        toast.info(change.label, { description: result.peer.note, duration: 15_000 })
      } else {
        toast.success(result.plan.empty ? 'Nothing to change' : change.label)
      }
      return true
    } catch (error) {
      if (!(error instanceof ApiError)) {
        toast.error(String(error))
        return false
      }

      const body = error.body as RemoteApplyResult | undefined
      const plan = body?.plan

      if (error.status === 409 && plan?.blocked) {
        // Not a decision the operator can make from here: the interface name
        // belongs to another program, and the answer is in that program's
        // configuration or in a different name.
        toast.error('That interface is not olr’s', { description: plan.blocked })
        return false
      }

      if (error.status === 409 && plan?.impact === 'disruptive') {
        // Nothing was written. The same change, sent again with confirm, is what
        // goes ahead — which is why a change is a value and not a call.
        setConfirming({ change, plan })
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
    submit: (change: RemoteChangeRequest) => send(change, false),
    confirming,
    confirm: async () => {
      if (!confirming) return
      const { change } = confirming
      setConfirming(null)
      await send(change, true)
    },
    cancel: () => setConfirming(null),
    issued,
    dismissIssued: () => setIssued(null),
    failure,
    dismissFailure: () => setFailure(null),
    busy: apply.isPending,
  }
}
