import { useMutation, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, Loader2 } from 'lucide-react'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { ApiError, api } from '@/lib/api'
import type { Blocker, FixResult } from '@/lib/api-types'

/**
 * Things about the box, rather than the configuration, standing between a
 * module and its job.
 *
 * Shared by every section rather than written per page, for the same reason
 * core.WriteBlockersText is shared by every CLI surface: one dnsmasq holding
 * both :53 and UDP/67 appears on two pages, and an operator who reads a
 * different sentence on each has to work out whether they are looking at one
 * problem or two. That is also why the button lives here — both pages get it
 * from one edit, and neither can offer a differently-worded version of the same
 * action.
 *
 * Rendered above the section's own warnings wherever it appears. A blocker's
 * cause is outside olr entirely, so advice about this module's units is noise
 * until it is cleared — "nothing is serving DNS" is not actionable next to
 * another daemon holding the socket.
 */
export function BlockerAlerts({
  module,
  blockers,
}: {
  /** Which module's /blockers/fix to post to: 'dns', 'dhcp'. */
  module: string
  blockers?: Blocker[]
}) {
  const fix = useFixBlocker(module)

  if (!blockers?.length) return null

  return (
    <>
      {blockers.map((b) => (
        <Alert key={b.unit ?? b.summary} variant="destructive">
          <AlertTriangle />
          <AlertTitle>{b.summary}</AlertTitle>
          {/* min-w-0 is load-bearing, not tidying. Alert is a grid, and a grid
              item's default min-width is auto — so without it the pre below
              widens its track instead of scrolling, and a long command drags
              the whole card past the edge of a phone. */}
          <AlertDescription className="min-w-0">
            {b.detail && <p>{b.detail}</p>}

            {/* The button above the command block, and the block kept even
                when there is a button. One click is the point; showing what
                the click runs is the condition on doing it at all. An operator
                who would rather type it has lost nothing. */}
            {b.action && (
              <Button
                variant="outline"
                size="sm"
                /* h-auto and whitespace-normal, because Button is
                   whitespace-nowrap and fixed-height by default: a label like
                   "Stand dnsmasq.service down and take UDP/67 and :53" is wider
                   than a phone, and nowrap inside a grid track clips it at the
                   card's edge rather than wrapping. The labels are kept short
                   as well — this is the second line of defence, for the ones
                   built from a package name nobody has seen yet. */
                className="mt-2 h-auto self-start py-1.5 text-left whitespace-normal"
                disabled={fix.isPending}
                onClick={() => fix.mutate(b.action!.id)}
              >
                {fix.isPending && fix.variables === b.action.id && (
                  <Loader2 className="animate-spin" aria-hidden />
                )}
                {b.action.label}
              </Button>
            )}

            {b.fix && (
              /* pre, not a paragraph: this is shell to be pasted, and a
                 command that has been reflowed is a broken one. It scrolls
                 rather than wraps for the same reason. */
              <pre className="mt-2 overflow-x-auto rounded-md bg-muted/50 p-3 font-mono text-xs whitespace-pre">
                {b.fix}
              </pre>
            )}
          </AlertDescription>
        </Alert>
      ))}
    </>
  )
}

/**
 * Asks olrd to clear one blocker.
 *
 * One id per call rather than the whole list, even though the endpoint accepts
 * an empty body meaning "everything": the operator pressed *this* button, under
 * *this* summary, having read what it says it will run. A button that also did
 * the one below it would be doing something nobody agreed to.
 *
 * No optimistic update and no timeout of our own. Installing a package takes as
 * long as it takes — on a box running unattended-upgrades it waits on the dpkg
 * lock first — and olrd's server has no WriteTimeout, so the honest thing is a
 * spinner until it answers.
 */
function useFixBlocker(module: string) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (id: string) =>
      api.post<FixResult>(`/api/${module}/blockers/fix`, { ids: [id] }),
    onSuccess: () => {
      toast.success('Done')
    },
    onError: (error) => {
      // §5.3.2 has no rollback, so a half-finished fix left the box somewhere.
      // The failed step is the useful sentence — it carries the package
      // manager's own complaint, which explains every interesting failure
      // better than anything olr could say about it.
      const steps = (error instanceof ApiError ? (error.body as FixResult)?.steps : undefined) ?? []
      const failed = steps.find((s) => !s.done)
      toast.error(failed?.description ?? String(error), {
        description: failed?.error ?? (error instanceof ApiError ? error.message : undefined),
      })
    },
    onSettled: () => {
      // Invalidated on failure too: a fix that installed the package and then
      // failed to stand the unit down changed the box, and every observed
      // answer for this module is now stale.
      queryClient.invalidateQueries({ queryKey: [module] })
    },
  })
}
