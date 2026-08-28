import { Badge } from '@/components/ui/badge'
import type { Impact, Plan } from '@/lib/api-types'
import { cn } from '@/lib/utils'

/**
 * How each impact class reads to an operator (design.md §5.3.3).
 *
 * The labels answer "what happens to me?", not "what does the daemon do?".
 * `reload` and `restart` are facts about dnsmasq that an operator cannot act
 * on; whether their devices stay connected is the thing they actually want to
 * know, so that is what the badge says. The daemon's own word for it is still
 * one disclosure away, in the change detail.
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
    hint: 'Devices stay connected and notice nothing.',
    variant: 'success',
  },
  restart: {
    label: 'Brief pause',
    hint: 'Address handling stops for a moment. Devices keep the addresses they already have.',
    variant: 'warning',
  },
  disruptive: {
    label: 'Devices disconnect',
    hint: 'Devices using an address from this router will lose it.',
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
 * Kept out of {@link PlanDiff} and shown unconditionally: the reasons are the
 * one part of a plan already written for a human, so burying them behind the
 * same disclosure as the file diffs would hide the answer along with the
 * evidence.
 */
export function PlanReasons({ plan }: { plan: Plan }) {
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
 * The diff a change would make, straight from the API's plan.
 *
 * "3 files will change" is not something an operator can act on. Showing the
 * lines is what lets a human — or an agent proposing a change for review
 * (design.md §6.4) — see that a pool moved by one address rather than that
 * something moved. It now sits behind a disclosure rather than in front of the
 * decision: still available to whoever wants it, no longer the first thing
 * between an operator and their answer.
 */
export function PlanDiff({ plan }: { plan: Plan }) {
  if (plan.empty) {
    return <p className="text-sm text-muted-foreground">No changes.</p>
  }

  return (
    <div className="space-y-3">
      {plan.changes.map((change) => (
        <div key={change.path} className="overflow-hidden rounded-lg border">
          <div className="flex items-center gap-2 border-b bg-muted/50 px-3 py-1.5">
            <span className="truncate font-mono text-xs">{change.path}</span>
            <ImpactBadge impact={change.impact} className="ml-auto shrink-0" />
          </div>
          <pre className="max-h-64 overflow-auto px-3 py-2 font-mono text-xs leading-relaxed">
            {change.diff.split('\n').map((line, i) => (
              <div
                key={i}
                className={cn(
                  line.startsWith('+') && !line.startsWith('+++') && 'text-success-foreground',
                  line.startsWith('-') && !line.startsWith('---') && 'text-destructive',
                  (line.startsWith('+++') || line.startsWith('---')) && 'text-muted-foreground',
                )}
              >
                {line || ' '}
              </div>
            ))}
          </pre>
        </div>
      ))}
    </div>
  )
}
