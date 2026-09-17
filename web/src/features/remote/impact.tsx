import {
  type ImpactLabels,
  ImpactBadge as Badge,
  impactHint as hint,
} from '@/features/plan/plan-preview'
import type { Impact, RemotePlan } from '@/lib/api-types'
import { cn } from '@/lib/utils'

/**
 * The same four classes as the other modules', weighted for what is actually
 * lost here — and this is the only screen in olr where the thing lost is not on
 * this box at all.
 *
 * `reload` is the ordinary case and is genuinely invisible: loading the peers
 * keeps the session state of every device whose key is unchanged, so adding one
 * costs the others nothing.
 *
 * `restart` means live tunnels drop and come back by themselves. A WireGuard
 * client retries forever, so this is an interruption rather than a loss —
 * closer to the gateway screen's use of the word than to ingress's, where it
 * means a dropped upload.
 *
 * `disruptive` is the rung that needs the most care, because two quite
 * different things reach it and both are irreversible by retrying. Either a
 * device can no longer get in, or a configuration that already left this box —
 * and is sitting on somebody's phone — is now wrong. No reload fixes a file on
 * a phone, which is why the label talks about the device rather than about a
 * connection.
 */
export const IMPACT: ImpactLabels = {
  none: {
    label: 'No change',
    hint: 'Nothing running changes.',
    variant: 'secondary',
  },
  reload: {
    label: 'Seamless',
    hint: 'Picked up without disturbing anyone already connected.',
    variant: 'success',
  },
  restart: {
    label: 'Brief drop',
    hint: 'Anyone connected right now is cut off and reconnects by themselves within a few seconds. Nothing has to be re-issued.',
    variant: 'warning',
  },
  disruptive: {
    label: 'A device loses access',
    hint: 'Either a device can no longer get in, or the configuration already on it stops working. Neither is fixed by trying again — the device has to be set up afresh.',
    variant: 'destructive',
  },
}

export function ImpactBadge({ impact, className }: { impact: Impact; className?: string }) {
  return <Badge impact={impact} labels={IMPACT} className={className} />
}

export function impactHint(impact: Impact): string {
  return hint(IMPACT, impact)
}

/**
 * Why the server called this disruptive, in its own words.
 *
 * Shown unconditionally and outside the disclosure: the reasons are the one
 * part of a plan already written for a human, and on this screen the most
 * important of them names the device that is about to lose its way in. Burying
 * that behind the same fold as the evidence would hide the answer.
 *
 * Typed to this module's plan rather than to the shared PlanCore, which the
 * gateway screen also has to do: a plan over kernel lines carries no `backend`,
 * because there is no backend.
 */
export function PlanReasons({ plan }: { plan: RemotePlan }) {
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
 * the kernel and not a backend's config file — there is no file at all, since
 * the configuration `wg` reads is handed to it on a pipe. Each line is the same
 * canonical text `olr remote show --dry-run` prints.
 */
export function PlanDiff({ plan }: { plan: RemotePlan }) {
  if (!plan.known) {
    // "Nothing to do" and "we could not look" are different answers and only
    // one of them is honest here (design.md §3.4).
    return (
      <p className="text-sm text-muted-foreground">
        This box&rsquo;s kernel could not be read, so there is nothing to compare against.
      </p>
    )
  }
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
