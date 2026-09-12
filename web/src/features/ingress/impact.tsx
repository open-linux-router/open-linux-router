import {
  type ImpactLabels,
  ImpactBadge as Badge,
  PlanDiff as Diff,
  impactHint as hint,
} from '@/features/plan/plan-preview'
import type { Impact, PlanCore } from '@/lib/api-types'

export { PlanReasons } from '@/features/plan/plan-preview'

/**
 * The same four classes as the other modules', weighted for what is actually
 * being interrupted here — and it is the one thing on this box that does not
 * retry.
 *
 * A DHCP client retries. A resolver's client retries. A browser part-way through
 * an upload does not, and neither does a video call. So the middle of this ladder
 * is heavier than anywhere else in olr:
 *
 * `reload` is the ordinary case and is genuinely invisible: Caddy swaps its
 * configuration while keeping its listeners, so requests in flight finish.
 *
 * `restart` is reached by exactly one thing — a rotated provider credential,
 * which systemd only delivers to a new process — and it drops every connection
 * through the proxy. On the dns screen `restart` is labelled "unnoticed"; here it
 * is the opposite, and using the same word for both would be the drift that makes
 * a label worthless.
 *
 * `disruptive` is a published name going away. Not a pause — an address that
 * stops answering, which nobody's browser recovers from by waiting.
 */
export const IMPACT: ImpactLabels = {
  none: {
    label: 'No change',
    hint: 'Nothing running changes.',
    variant: 'secondary',
  },
  reload: {
    label: 'Seamless',
    hint: 'Picked up without dropping a single connection. Anything open right now finishes normally.',
    variant: 'success',
  },
  restart: {
    label: 'Connections drop',
    hint: 'The proxy has to restart to pick up the new credential, so every connection through it is cut. Pages reload; uploads and calls do not.',
    variant: 'warning',
  },
  disruptive: {
    label: 'An address stops working',
    hint: 'A published name will stop answering. Anyone with it open loses it, and reloading will not help.',
    variant: 'destructive',
  },
}

export function ImpactBadge({ impact, className }: { impact: Impact; className?: string }) {
  return <Badge impact={impact} labels={IMPACT} className={className} />
}

export function impactHint(impact: Impact): string {
  return hint(IMPACT, impact)
}

export function PlanDiff({ plan }: { plan: PlanCore }) {
  return <Diff plan={plan} labels={IMPACT} />
}
