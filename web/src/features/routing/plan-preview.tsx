import { Badge } from '@/components/ui/badge'
import type { Impact, RoutingPlan } from '@/lib/api-types'
import { cn } from '@/lib/utils'

/**
 * How each impact class reads to an operator (design.md §5.3.3).
 *
 * The labels answer "what happens to me?", not "what does the kernel do?". Two
 * of them differ from the addresses screen's because the same word means
 * something else here: `reload` is a change that established connections do not
 * notice — which is what the ct-mark save and restore buys — and `restart` is
 * never produced at all, because there is no daemon to bounce.
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
    hint: 'Connections that are already open keep the route they started on. Only new ones use the new setting.',
    variant: 'success',
  },
  restart: {
    label: 'Brief pause',
    hint: 'Traffic pauses for a moment.',
    variant: 'warning',
  },
  disruptive: {
    label: 'Connections break',
    hint: 'Traffic changes path, so open connections have to be re-established.',
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
 * Why the server called this disruptive, in its own words.
 *
 * Shown unconditionally and outside the disclosure: the reasons are the one
 * part of a plan already written for a human, and on this screen the most
 * important of them is "applying this could disconnect you from the router".
 * Burying that behind the same fold as the evidence would hide the answer.
 */
export function PlanReasons({ plan }: { plan: RoutingPlan }) {
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
 * A list of lines rather than per-file diffs, because this module configures
 * the kernel and not a backend's config file. Each line is the same canonical
 * text `olr routing show --dry-run` prints and the same one stored in the
 * nftables rule's comment — so an operator who reads this and then runs
 * `nft list ruleset` finds the same strings, which is most of what makes the
 * thing debuggable.
 */
export function PlanDiff({ plan }: { plan: RoutingPlan }) {
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
