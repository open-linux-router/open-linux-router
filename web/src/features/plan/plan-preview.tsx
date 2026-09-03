import { Badge } from '@/components/ui/badge'
import type { Impact, PlanCore } from '@/lib/api-types'
import { cn } from '@/lib/utils'

/**
 * The plan preview, shared by every module that has one.
 *
 * The *rendering* of a plan is module-agnostic — a diff is a diff, and a reason
 * is already a sentence written for a human. The *wording of the impact* is
 * not: dhcp's `disruptive` means devices lose an address, dns's means the whole
 * house loses name resolution, and one label cannot honestly say both. So the
 * widgets live here and the labels arrive from the caller.
 *
 * Typed to {@link PlanCore} rather than to either module's plan: nothing below
 * reads `action` or `services`, and demanding one of them would be the only
 * reason this file could not serve both.
 */

/**
 * How each impact class reads to an operator (design.md §5.3.3).
 *
 * The labels answer "what happens to me?", not "what does the daemon do?".
 * `reload` and `restart` are facts about a daemon that an operator cannot act
 * on; whether their devices keep working is the thing they actually want to
 * know, so that is what the badge says. The daemon's own word for it is still
 * one disclosure away, in the change detail.
 */
export type ImpactLabels = Record<
  Impact,
  { label: string; hint: string; variant: 'secondary' | 'success' | 'warning' | 'destructive' }
>

function entry(labels: ImpactLabels, impact: Impact) {
  // Falls back rather than crashing on an impact a newer daemon knows and this
  // build does not. A wrong-but-quiet badge beats a blank page over a label.
  return labels[impact] ?? labels.none
}

export function ImpactBadge({
  impact,
  labels,
  className,
}: {
  impact: Impact
  labels: ImpactLabels
  className?: string
}) {
  const meta = entry(labels, impact)
  return (
    <Badge variant={meta.variant} className={className}>
      {meta.label}
    </Badge>
  )
}

export function impactHint(labels: ImpactLabels, impact: Impact): string {
  return entry(labels, impact).hint
}

/**
 * Why the server called this disruptive, in its own words.
 *
 * Kept out of {@link PlanDiff} and shown unconditionally: the reasons are the
 * one part of a plan already written for a human, so burying them behind the
 * same disclosure as the file diffs would hide the answer along with the
 * evidence.
 */
export function PlanReasons({ plan }: { plan: PlanCore }) {
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
 * something moved. It sits behind a disclosure rather than in front of the
 * decision: still available to whoever wants it, no longer the first thing
 * between an operator and their answer.
 */
export function PlanDiff({ plan, labels }: { plan: PlanCore; labels: ImpactLabels }) {
  if (plan.empty) {
    return <p className="text-sm text-muted-foreground">No changes.</p>
  }

  return (
    <div className="space-y-3">
      {plan.changes.map((change) => (
        <div key={change.path} className="overflow-hidden rounded-lg border">
          <div className="flex items-center gap-2 border-b bg-muted/50 px-3 py-1.5">
            <span className="truncate font-mono text-xs">{change.path}</span>
            <ImpactBadge impact={change.impact} labels={labels} className="ml-auto shrink-0" />
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
