import { AlertTriangle } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import type { Blocker } from '@/lib/api-types'

/**
 * Things about the box, rather than the configuration, standing between a
 * module and its job.
 *
 * Shared by every section rather than written per page, for the same reason
 * core.WriteBlockersText is shared by every CLI surface: one dnsmasq holding
 * both :53 and UDP/67 appears on two pages, and an operator who reads a
 * different sentence on each has to work out whether they are looking at one
 * problem or two.
 *
 * Rendered above the section's own warnings wherever it appears. A blocker's
 * cause is outside olr entirely, so advice about this module's units is noise
 * until it is cleared — "nothing is serving DNS" is not actionable next to
 * another daemon holding the socket.
 */
export function BlockerAlerts({ blockers }: { blockers?: Blocker[] }) {
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
