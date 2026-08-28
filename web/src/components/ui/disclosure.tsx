import { ChevronRight } from 'lucide-react'

import { cn } from '@/lib/utils'

/**
 * Detail that is worth keeping but not worth reading first.
 *
 * Built on native `<details>` on purpose: it is keyboard operable, announced,
 * and searchable by the browser's find-in-page without any state of our own.
 * The house rule for what belongs in here is that hiding it must not cost the
 * operator a fact they need to act — file paths, diffs, unit names and exact
 * timestamps qualify; whether the server is running does not.
 */
export function Disclosure({
  summary,
  children,
  className,
  defaultOpen = false,
}: {
  summary: React.ReactNode
  children: React.ReactNode
  className?: string
  defaultOpen?: boolean
}) {
  return (
    <details className={cn('group', className)} open={defaultOpen}>
      <summary className="flex min-h-11 cursor-pointer list-none items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring [&::-webkit-details-marker]:hidden">
        <ChevronRight
          className="size-3.5 shrink-0 transition-transform group-open:rotate-90"
          aria-hidden
        />
        {summary}
      </summary>
      <div className="pb-1">{children}</div>
    </details>
  )
}
