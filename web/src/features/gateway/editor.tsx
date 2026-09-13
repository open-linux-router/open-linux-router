import { AlertTriangle } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Disclosure } from '@/components/ui/disclosure'
import { Skeleton } from '@/components/ui/skeleton'
import { ImpactBadge, PlanDiff, PlanReasons, impactHint } from '@/features/gateway/plan-preview'
import { useGatewayConfig, type GatewayChange } from '@/features/gateway/queries'
import { useGatewayApply } from '@/features/gateway/use-apply'

/** As features/dns/editor.tsx, for gateway. */
export function useGatewayEditor() {
  const config = useGatewayConfig()
  const applier = useGatewayApply()

  return {
    config: config.data,
    busy: applier.busy,
    /**
     * Every edit names the one thing it changes and sends that, rather than
     * building a whole new config in the browser — which is what once left
     * `default` and every assignment pointing at a renamed exit that no longer
     * existed. Config.Rename and Config.Upsert live in Go for a reason.
     */
    change: (c: GatewayChange) => applier.submit(c),
    applier,
    gate: config.isPending ? (
      <PageSkeleton />
    ) : config.isError ? (
      <Alert variant="destructive">
        <AlertTriangle />
        <AlertTitle>Could not load the gateway settings</AlertTitle>
        <AlertDescription>{(config.error as Error).message}</AlertDescription>
      </Alert>
    ) : null,
  }
}

export function ApplyOutcome({ applier }: { applier: ReturnType<typeof useGatewayApply> }) {
  return (
    <>
      {applier.blocked && (
        <Alert variant="destructive" role="alert">
          <AlertTriangle />
          <AlertTitle>Something else is managing routing on this box</AlertTitle>
          <AlertDescription className="space-y-3">
            <p>{applier.blocked.blocked}</p>
            {applier.blocked.foreign?.length ? (
              <Disclosure summary="The rules we found">
                <pre className="overflow-auto font-mono text-xs leading-relaxed">
                  {applier.blocked.foreign
                    .map(
                      (f) => `priority ${f.priority}  ${f.family}  table ${f.table}  ${f.selector}`,
                    )
                    .join('\n')}
                </pre>
              </Disclosure>
            ) : null}
            <Button size="sm" variant="outline" onClick={applier.dismissBlocked}>
              Dismiss
            </Button>
          </AlertDescription>
        </Alert>
      )}

      {applier.failure && (
        <Alert variant="destructive" role="alert">
          <AlertTriangle />
          <AlertTitle>Only part of the change went through</AlertTitle>
          <AlertDescription className="space-y-3">
            <p>
              The steps that finished have already taken effect and will not be undone. Fixing the
              cause and applying again is safe — it picks up where this left off.
            </p>
            <Disclosure summary="Which steps ran">
              <ul className="space-y-1 font-mono text-xs">
                {applier.failure.steps?.map((s, i) => (
                  <li key={i}>
                    {s.done ? 'done   ' : s.error ? 'failed ' : 'skipped'} {s.description}
                    {s.error ? ` — ${s.error}` : ''}
                  </li>
                ))}
              </ul>
            </Disclosure>
            <Button size="sm" variant="outline" onClick={applier.dismissFailure}>
              Dismiss
            </Button>
          </AlertDescription>
        </Alert>
      )}

      <ConfirmDialog applier={applier} />
    </>
  )
}

/**
 * The one thing that interrupts an operator (design.md §6.3).
 *
 * Everything else applies on the click. This appears only when the plan came
 * back `disruptive`, which on this screen most often means the change would
 * route the operator's own connection somewhere else — the single outcome they
 * cannot recover from by clicking again.
 */
function ConfirmDialog({ applier }: { applier: ReturnType<typeof useGatewayApply> }) {
  const held = applier.confirming
  if (!held) return null

  return (
    <Dialog open onOpenChange={(open) => !open && applier.cancel()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            Apply this change?
            <ImpactBadge impact={held.plan.impact} />
          </DialogTitle>
          <DialogDescription>{impactHint(held.plan.impact)}</DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <PlanReasons plan={held.plan} />
          <Disclosure summary="What would change">
            <PlanDiff plan={held.plan} />
          </Disclosure>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={applier.cancel}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={applier.confirm} disabled={applier.busy}>
            Apply anyway
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function PageSkeleton() {
  return (
    <div className="space-y-6">
      <Skeleton className="h-24 w-full rounded-xl" />
      <Skeleton className="h-56 w-full rounded-xl" />
    </div>
  )
}
