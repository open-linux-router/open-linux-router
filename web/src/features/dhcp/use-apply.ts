import { useState } from 'react'
import { toast } from 'sonner'

import { useApplyDhcpConfig, usePlanPreview } from '@/features/dhcp/queries'
import { ApiError } from '@/lib/api'
import type { ApplyResult, Plan } from '@/lib/api-types'
import type { DhcpConfig } from '@/lib/config-types'

/**
 * The apply interaction for the whole screen.
 *
 * design.md §5.1 is explicit that the GUI applies instantly — toggling a switch
 * takes effect, with no "Apply changes" bar. §5.3.3 is equally explicit that the
 * UI should be able to say "this will drop all LAN connections" rather than
 * showing a spinner. Those only look contradictory: the resolution is to apply
 * immediately *unless* the plan comes back disruptive, and to ask first when it
 * does.
 *
 * That is why every change goes through /plan before /config. The extra round
 * trip buys the one thing an operator cannot recover from cheaply — being
 * disconnected from the router by their own click.
 */
export function useDhcpApply() {
  const preview = usePlanPreview()
  const apply = useApplyDhcpConfig()

  /** A change held back because it would be disruptive. */
  const [confirming, setConfirming] = useState<{ config: DhcpConfig; plan: Plan } | null>(null)

  /** The steps of the last failed apply, which the page keeps on screen. */
  const [failure, setFailure] = useState<ApplyResult | null>(null)

  async function commit(config: DhcpConfig) {
    setFailure(null)
    try {
      const result = await apply.mutateAsync(config)
      toast.success(describe(result.plan))
      return true
    } catch (error) {
      if (error instanceof ApiError) {
        // A failed apply still changed things (§5.3.2 — no rollback, the steps
        // that landed stay landed). Keep the body so the page can show exactly
        // which ones did, and re-running finishes the job.
        const body = error.body as ApplyResult | undefined
        if (body?.steps?.length) setFailure(body)
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
  async function submit(config: DhcpConfig) {
    try {
      const plan = await preview.mutateAsync(config)

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
          description: error.problems.map((p) => `${p.path ?? ''} ${p.message}`.trim()).join('\n') || undefined,
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
    busy: preview.isPending || apply.isPending,
  }
}

function describe(plan: Plan): string {
  switch (plan.action) {
    case 'start':
      return 'Applied — DHCP server started'
    case 'stop':
      return 'Applied — DHCP server stopped'
    case 'restart':
      return 'Applied — DHCP server restarted'
    case 'reload':
      // Worth naming: a reload means live clients noticed nothing, which is the
      // whole reason the hosts directory is split out from the config file.
      return 'Applied — reloaded without interrupting clients'
    default:
      return 'Applied'
  }
}
