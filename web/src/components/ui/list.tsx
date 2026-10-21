import { Pencil } from 'lucide-react'
import { Link } from 'react-router'

import { RowAction, RowChevron } from '@/components/ui/row-action'
import { cn } from '@/lib/utils'

function List({ className, ...props }: React.ComponentProps<'ul'>) {
  return <ul data-slot="list" className={cn('divide-y divide-border overflow-hidden rounded-xl border bg-card', className)} {...props} />
}

/** Values reflow below the title on phones. Actions stay outside the row link/button. */
function ListRow({ title, subtitle, leading, trailing, trailingTone = 'muted', action, editLabel, onSelect, to, wrap = false, className, ...props }: Omit<React.ComponentProps<'li'>, 'title' | 'onSelect'> & {
  title: React.ReactNode
  subtitle?: React.ReactNode
  leading?: React.ReactNode
  trailing?: React.ReactNode
  trailingTone?: 'muted' | 'value'
  action?: React.ReactNode
  /**
   * The accessible name for the row's edit affordance. A row that opens an
   * editor wears a pencil rather than the navigation chevron, so the glyphs in
   * a list only ever mean one thing each.
   */
  editLabel?: string
  onSelect?: () => void
  to?: string
  wrap?: boolean
}) {
  const body = <>
    {leading}
    <div className="min-w-0 flex-1 sm:flex sm:items-center sm:justify-between sm:gap-6">
      <div className="min-w-0 flex-1">
        {/* The title keeps one treatment in every row: whether a row happens to
            have a value beside it is not a reason for its label to change colour
            and weight halfway down a list. Emphasis lives on the value. */}
        <div className={cn('text-sm text-foreground', wrap ? 'break-words' : 'truncate')}>{title}</div>
        {subtitle && <div className={cn('mt-0.5 text-[0.8125rem] leading-relaxed text-muted-foreground', wrap ? 'break-words' : 'truncate')}>{subtitle}</div>}
      </div>
      {trailing != null && <div className={cn('mt-1 min-w-0 text-sm leading-5 [overflow-wrap:anywhere] sm:mt-0 sm:max-w-[45%] sm:text-right', trailingTone === 'value' ? 'font-medium text-foreground' : 'text-muted-foreground')}>{trailing}</div>}
    </div>
  </>
  const interactive = 'flex min-h-16 min-w-0 flex-1 items-center gap-3 px-4 py-3 text-left transition-colors duration-150 hover:bg-accent/60 active:bg-accent focus-visible:-outline-offset-2 focus-visible:outline-2 focus-visible:outline-ring sm:px-5'
  // The chevron means "this row leads somewhere". A row that opens an editor
  // says so with a pencil instead, so the same glyph never means two verbs.
  return (
    <li data-slot="list-row" className={cn('flex items-center bg-card', className)} {...props}>
      {to ? <Link to={to} className={interactive}>{body}<RowChevron /></Link>
        : onSelect ? <button type="button" onClick={onSelect} className={interactive}>{body}</button>
          : <div className="flex min-h-16 min-w-0 flex-1 items-center gap-3 px-4 py-3 sm:px-5">{body}</div>}
      {(action || (editLabel && onSelect)) && (
        <div className="flex shrink-0 items-center gap-1 pr-3">
          {editLabel && onSelect && <RowAction icon={Pencil} label={editLabel} onClick={onSelect} />}
          {action}
        </div>
      )}
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
