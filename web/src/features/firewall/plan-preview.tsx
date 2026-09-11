import { Badge } from '@/components/ui/badge'
import type { FirewallPlan, Impact } from '@/lib/api-types'
import { cn } from '@/lib/utils'

/**
 * How each impact class reads to an operator (design.md §5.3.3).
 *
 * The labels answer "what happens to me?", not "what does the kernel do?". Two
 * of them differ from the gateway screen's, because the same word means
 * something else here: `reload` is adding a translation, which no existing
 * connection can notice, and `disruptive` is removing one, which stops
 * translating connections that are still running through it — they die
 * mid-stream rather than being refused.
 */
const IMPACT: Record<
  Impact,
  { label: string; hint: string; variant: 'secondary' | 'success' | 'warning' | 'destructive' }
> = {
  none: {
    label: 'No change',
    hint: 'Nothing running changes.',
    variant: 'secondary',
  },
  reload: {
    label: 'Seamless',
    hint: 'Connections that are already open are unaffected. Only new ones meet the new rules.',
    variant: 'success',
  },
  restart: {
    label: 'Brief pause',
    hint: 'Traffic pauses for a moment.',
    variant: 'warning',
  },
  disruptive: {
    label: 'Connections break',
    hint: 'A forward that is carrying traffic is being taken away, so anything still connected through it stops working and has to reconnect.',
    variant: 'destructive',
  },
}

export function ImpactBadge({ impact, className }: { impact: Impact; className?: string }) {
  const meta = IMPACT[impact] ?? IMPACT.none
  return (
    <Badge variant={meta.variant} className={className}>
      {meta.label}
    </Badge>
  )
}

export function impactHint(impact: Impact): string {
  return (IMPACT[impact] ?? IMPACT.none).hint
}

/**
 * Why the server said what it said, in its own words.
 *
 * Shown unconditionally and outside the disclosure, because on this screen the
 * reasons carry the two facts an operator most needs and cannot see anywhere
 * else: that this router is already serving the port being forwarded away, and
 * that something else on the box is dropping forwarded traffic. Burying those
 * behind the same fold as the evidence would hide the answer.
 */
export function PlanReasons({ plan }: { plan: FirewallPlan }) {
  if (!plan.reasons?.length) return null

  return (
    <ul className="space-y-1 text-sm">
      {plan.reasons.map((reason) => (
        <li key={reason} className="flex gap-2">
          <span aria-hidden className="text-muted-foreground">
            •
          </span>
          {reason}
        </li>
      ))}
    </ul>
  )
}

/**
 * What would change in the kernel, straight from the API's plan.
 *
 * A list of lines rather than per-file diffs, because this module configures the
 * kernel and not a backend's config file. Each line is the same canonical text
 * `olr firewall show --dry-run` prints and the same one stored in the nftables
 * rule's comment — so an operator who reads this and then runs
 * `nft list table inet olr_nat` finds the same strings, which is most of what
 * makes the thing debuggable.
 */
export function PlanDiff({ plan }: { plan: FirewallPlan }) {
  if (plan.empty) {
    return <p className="text-sm text-muted-foreground">No changes.</p>
  }

  return (
    <pre className="max-h-72 overflow-auto rounded-lg border px-3 py-2 font-mono text-xs leading-relaxed">
      {plan.changes.map((change) => (
        <div
          key={`${change.kind}:${change.line}`}
          className={cn(
            change.kind === 'add' && 'text-success-foreground',
            change.kind === 'remove' && 'text-destructive',
          )}
        >
          {change.kind === 'add' ? '+ ' : '- '}
          {change.line}
        </div>
      ))}
    </pre>
  )
}
