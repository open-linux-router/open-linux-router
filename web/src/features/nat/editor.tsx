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
import { ImpactBadge, PlanDiff, PlanReasons, impactHint } from '@/features/nat/plan-preview'
import { useForwardsConfig, type ForwardsChange } from '@/features/nat/queries'
import { useForwardsApply } from '@/features/nat/use-apply'

/** As features/dns/editor.tsx, for port forwards. */
export function useForwardsEditor() {
  const config = useForwardsConfig()
  const applier = useForwardsApply()

  return {
    config: config.data,
    busy: applier.busy,
    /** Every edit names the one thing it changes and sends that. */
    change: (c: ForwardsChange) => applier.submit(c),
    applier,
    gate: config.isPending ? (
      <PageSkeleton />
    ) : config.isError ? (
      <Alert variant="destructive">
        <AlertTriangle />
        <AlertTitle>Could not load the port forwards</AlertTitle>
        <AlertDescription>{(config.error as Error).message}</AlertDescription>
      </Alert>
    ) : null,
  }
}

export function ApplyOutcome({ applier }: { applier: ReturnType<typeof useForwardsApply> }) {
  return (
    <>
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
 * back `disruptive`, which here means a forward that is currently carrying
 * traffic is being taken away — the connections through it will not be refused,
 * they will stop mid-stream.
 */
function ConfirmDialog({ applier }: { applier: ReturnType<typeof useForwardsApply> }) {
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
