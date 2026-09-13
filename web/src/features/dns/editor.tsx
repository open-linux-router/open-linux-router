import { AlertTriangle, Check, X } from 'lucide-react'

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
import { PlanDiff, PlanReasons, impactHint } from '@/features/dns/impact'
import { useDnsConfig } from '@/features/dns/queries'
import { useDnsApply } from '@/features/dns/use-apply'
import type { DnsConfig } from '@/lib/config-types'

/**
 * Everything a page needs to edit the DNS config, wherever that page sits.
 *
 * Splitting the section into a landing page and six sub-pages turned one screen
 * that loaded the config into seven, and each of them submits whole documents
 * through the same plan-then-apply path. Written out per page that would be six
 * copies of the same four hooks and the same two error surfaces. React Query
 * dedupes the fetch itself, so this costs nothing beyond one shared shape.
 */
export function useDnsEditor() {
  const config = useDnsConfig()
  const applier = useDnsApply()

  return {
    /** Undefined until the config lands; `gate` is what to render meanwhile. */
    config: config.data,
    busy: applier.busy,
    /** Every edit is a whole new config sent through the same path. */
    change: (next: DnsConfig) => applier.submit(next),
    applier,
    gate: config.isPending ? (
      <PageSkeleton />
    ) : config.isError ? (
      <Alert variant="destructive">
        <AlertTriangle />
        <AlertTitle>Could not load the DNS settings</AlertTitle>
        <AlertDescription>{(config.error as Error).message}</AlertDescription>
      </Alert>
    ) : null,
  }
}

/**
 * What an apply left behind: the confirmation it is waiting for, or the mess it
 * made.
 *
 * Both belong to the change rather than to the page, so every page that can
 * submit one renders this. A sub-page whose switch needs confirming must be
 * able to ask for it where the operator is standing.
 */
export function ApplyOutcome({ applier }: { applier: ReturnType<typeof useDnsApply> }) {
  return (
    <>
      {applier.failure && (
        <Alert variant="destructive" role="alert">
          <AlertTriangle />
          <AlertTitle>Only part of the change went through</AlertTitle>
          <AlertDescription className="space-y-3">
            <p>
              The steps that finished have already taken effect and will not be
              undone. Fixing the cause and applying again is safe — it picks up
              where this left off.
            </p>
            <Disclosure summary="Which steps ran">
              <ul className="space-y-1 pt-1 font-mono text-xs">
                {applier.failure.steps?.map((step) => (
                  <li key={step.description} className="flex gap-2">
                    {step.done ? (
                      <Check className="mt-0.5 size-3.5 shrink-0 text-success" aria-label="done" />
                    ) : (
                      <X className="mt-0.5 size-3.5 shrink-0 text-destructive" aria-label="failed" />
                    )}
                    <span>
                      {step.description}
                      {step.error && <span className="block opacity-80">{step.error}</span>}
                    </span>
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

function ConfirmDialog({ applier }: { applier: ReturnType<typeof useDnsApply> }) {
  const pending = applier.confirming

  return (
    <Dialog open={pending !== null} onOpenChange={(open) => !open && applier.cancel()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <AlertTriangle className="size-4 text-destructive" aria-hidden />
            This will break the internet for everyone
          </DialogTitle>
          <DialogDescription>
            {pending && impactHint(pending.plan.impact)} Every other change
            applies the moment you make it — this one asks first.
          </DialogDescription>
        </DialogHeader>

        {/* The reasons stay in front of the operator; the diff is the evidence
            behind them. design.md §6.4 needs the diff to stay inspectable, so
            it is one click away rather than the first thing in the dialog. */}
        {pending && (
          <div className="space-y-2">
            <PlanReasons plan={pending.plan} />
            <Disclosure summary="Show exactly what will change">
              <div className="max-h-[45vh] overflow-auto pt-2">
                <PlanDiff plan={pending.plan} />
              </div>
            </Disclosure>
          </div>
        )}

        <DialogFooter>
          <Button variant="ghost" onClick={applier.cancel}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={applier.confirm}>
            Apply anyway
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** Skeletons match the height and rhythm of what replaces them. */
function PageSkeleton() {
  return (
    <div className="space-y-6">
      <Skeleton className="h-24 w-full rounded-xl" />
      <Skeleton className="h-64 w-full rounded-xl" />
      <Skeleton className="h-48 w-full rounded-xl" />
    </div>
  )
}
