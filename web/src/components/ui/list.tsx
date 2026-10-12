import { ChevronRight } from 'lucide-react'
import { Link } from 'react-router'

import { cn } from '@/lib/utils'

function List({ className, ...props }: React.ComponentProps<'ul'>) {
  return <ul data-slot="list" className={cn('divide-y divide-border overflow-hidden rounded-xl border bg-card', className)} {...props} />
}

/** Values reflow below the title on phones. Actions stay outside the row link/button. */
function ListRow({ title, subtitle, leading, trailing, action, onSelect, to, wrap = false, className, ...props }: Omit<React.ComponentProps<'li'>, 'title' | 'onSelect'> & {
  title: React.ReactNode
  subtitle?: React.ReactNode
  leading?: React.ReactNode
  trailing?: React.ReactNode
  action?: React.ReactNode
  onSelect?: () => void
  to?: string
  wrap?: boolean
}) {
  const body = <>
    {leading}
    <div className="min-w-0 flex-1 sm:flex sm:items-center sm:justify-between sm:gap-6">
      <div className="min-w-0 flex-1">
        <div className={cn('text-sm font-medium', wrap ? 'break-words' : 'truncate')}>{title}</div>
        {subtitle && <div className={cn('mt-0.5 text-[0.8125rem] leading-relaxed text-muted-foreground', wrap ? 'break-words' : 'truncate')}>{subtitle}</div>}
      </div>
      {trailing && <div className="mt-1 min-w-0 break-words text-[0.8125rem] leading-relaxed text-muted-foreground sm:mt-0 sm:max-w-[45%] sm:text-right">{trailing}</div>}
    </div>
  </>
  const interactive = 'flex min-h-16 min-w-0 flex-1 items-center gap-3 px-4 py-3 text-left transition-colors duration-150 hover:bg-accent/60 active:bg-accent focus-visible:-outline-offset-2 focus-visible:outline-2 focus-visible:outline-ring sm:px-5'
  const chevron = <ChevronRight className="size-4 shrink-0 text-muted-foreground" aria-hidden />
  return (
    <li data-slot="list-row" className={cn('flex items-center bg-card', className)} {...props}>
      {to ? <Link to={to} className={interactive}>{body}{chevron}</Link>
        : onSelect ? <button type="button" onClick={onSelect} className={interactive}>{body}{chevron}</button>
          : <div className="flex min-h-16 min-w-0 flex-1 items-center gap-3 px-4 py-3 sm:px-5">{body}</div>}
      {action && <div className="shrink-0 pr-3">{action}</div>}
    </li>
  )
}

function ListEmpty({ children, action }: { children: React.ReactNode; action?: React.ReactNode }) {
  return (
    <div className="rounded-xl border bg-card px-6 py-10 text-center text-sm leading-relaxed text-muted-foreground">
      <div className="mx-auto max-w-md">{children}</div>
      {action && <div className="mt-4 flex justify-center">{action}</div>}
    </div>
  )
}

export { List, ListRow, ListEmpty }
