import { Badge } from '@/components/ui/badge'
import type { Impact, Plan } from '@/lib/api-types'
import { cn } from '@/lib/utils'

/** How each impact class reads to an operator (design.md §5.3.3). */
const IMPACT: Record<Impact, { label: string; hint: string; className: string }> = {
  none: {
    label: 'No effect',
    hint: 'Nothing running changes.',
    className: 'bg-muted text-muted-foreground',
  },
  reload: {
    label: 'Reload',
    hint: 'Picked up by a signal. Clients notice nothing.',
    className: 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-400',
  },
  restart: {
    label: 'Restart',
    hint: 'The DHCP server restarts. Leases survive, so clients keep their addresses.',
    className: 'bg-amber-500/15 text-amber-700 dark:text-amber-400',
  },
  disruptive: {
    label: 'Disruptive',
    hint: 'Clients holding an address will lose it.',
    className: 'bg-destructive/15 text-destructive',
  },
}

export function ImpactBadge({ impact, className }: { impact: Impact; className?: string }) {
  const meta = IMPACT[impact] ?? IMPACT.none
  return (
    <Badge variant="secondary" className={cn(meta.className, className)}>
      {meta.label}
    </Badge>
  )
}

export function impactHint(impact: Impact): string {
  return (IMPACT[impact] ?? IMPACT.none).hint
}

/**
 * The diff a change would make, straight from the API's plan.
 *
 * "3 files will change" is not something an operator can act on. Showing the
 * lines is what lets a human — or an agent proposing a change for review
 * (design.md §6.4) — see that a pool moved by one address rather than that
 * something moved.
 */
export function PlanDiff({ plan }: { plan: Plan }) {
  if (plan.empty) {
    return <p className="text-sm text-muted-foreground">No changes.</p>
  }

  return (
    <div className="space-y-3">
      {plan.reasons && plan.reasons.length > 0 && (
        <ul className="space-y-1 text-sm">
          {plan.reasons.map((reason) => (
            <li key={reason} className="text-destructive">
              {reason}
            </li>
          ))}
        </ul>
      )}

      {plan.changes.map((change) => (
        <div key={change.path} className="overflow-hidden rounded-md border">
          <div className="flex items-center gap-2 border-b bg-muted/50 px-3 py-1.5">
            <span className="truncate font-mono text-xs">{change.path}</span>
            <ImpactBadge impact={change.impact} className="ml-auto shrink-0" />
          </div>
          <pre className="max-h-64 overflow-auto px-3 py-2 font-mono text-xs leading-relaxed">
            {change.diff.split('\n').map((line, i) => (
              <div
                key={i}
                className={cn(
                  line.startsWith('+') && !line.startsWith('+++') && 'text-emerald-600 dark:text-emerald-400',
                  line.startsWith('-') && !line.startsWith('---') && 'text-destructive',
                  (line.startsWith('+++') || line.startsWith('---')) && 'text-muted-foreground',
                )}
              >
                {line || ' '}
              </div>
            ))}
          </pre>
        </div>
      ))}
    </div>
  )
}
