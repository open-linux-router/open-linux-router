import { useState } from 'react'
import { toast } from 'sonner'

import { useApplyDnsConfig, useDnsPlanPreview } from '@/features/dns/queries'
import { unitLabel } from '@/features/dns/units'
import { ApiError } from '@/lib/api'
import type { DnsApplyResult, DnsPlan, ServicePlan } from '@/lib/api-types'
import type { DnsConfig } from '@/lib/config-types'

/**
 * The apply interaction for the whole screen. Same shape as
 * features/dhcp/use-apply.ts, and for the same reason: design.md §5.1 says the
 * GUI applies instantly, §5.3.3 says it should be able to warn first, and the
 * two resolve as "apply immediately *unless* the plan comes back disruptive".
 *
 * The stakes are higher here than on the address page, which is why the extra
 * round trip through /plan is worth even more: the change an operator cannot
 * recover from cheaply is the one that takes away the resolver they are
 * currently using to reach this page.
 */
export function useDnsApply() {
  const preview = useDnsPlanPreview()
  const apply = useApplyDnsConfig()

  /** A change held back because it would be disruptive. */
  const [confirming, setConfirming] = useState<{ config: DnsConfig; plan: DnsPlan } | null>(null)

  /** The steps of the last failed apply, which the page keeps on screen. */
  const [failure, setFailure] = useState<DnsApplyResult | null>(null)

  async function commit(config: DnsConfig) {
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
        const body = error.body as DnsApplyResult | undefined
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
  async function submit(config: DnsConfig) {
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
    busy: preview.isPending || apply.isPending,
  }
}

function list(services: ServicePlan[]): string {
  const names = services.map((s) => unitLabel(s.unit))
  if (names.length <= 1) return names[0] ?? 'DNS'
  return `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`
}

/**
 * The toast for a completed apply.
 *
 * dhcp can switch on a single `plan.action`; this module drives two daemons that
 * are signalled independently, so the message reports the strongest thing that
 * happened and names which backend it happened to. Saying only "Applied" would
 * throw away the difference between a blocklist edit that interrupted nothing
 * and a restart of the thing the whole house resolves through.
 */
function describe(plan: DnsPlan): string {
  const acting = plan.services.filter((s) => s.action !== 'none')

  if (acting.some((s) => s.action === 'stop')) {
    return 'Applied — DNS stopped'
  }
  if (acting.length === 0) {
    // Nothing was signalled, but a plan with no actions still reaches here when
    // only the boot-time state moved — which is real work and worth confirming.
    if (plan.services.some((s) => s.enable !== undefined)) {
      return 'Applied — updated what starts at boot'
    }
    return 'Applied'
  }

  const started = acting.filter((s) => s.action === 'start')
  if (started.length) return `Applied — ${list(started)} started`

  const restarted = acting.filter((s) => s.action === 'restart')
  if (restarted.length) {
    // Named as harmless because it is: DNS is stateless and clients retry
    // unprompted, so a restart costs milliseconds. An operator who does not
    // know that reads "restarted" as an outage.
    return `Applied — ${list(restarted)} restarted; lookups in flight were retried`
  }

  return `Applied — ${list(acting)} reloaded without interrupting a lookup`
}
