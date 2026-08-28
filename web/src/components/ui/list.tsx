import { ChevronRight } from 'lucide-react'

import { cn } from '@/lib/utils'

/**
 * A grouped list, in the shape iOS and macOS Settings use for the same job.
 *
 * A table is the right tool for homogeneous columns you want to compare down a
 * column — leases are like that. Pools and fixed addresses are not: there are a
 * handful of them, each is an object you act on rather than a number you scan,
 * and most columns held "—" or a default. A row that leads with the thing's
 * name and pushes the rest to a quiet second line reads faster and, unlike a
 * table, needs no horizontal scrollbar on a phone.
 */
function List({ className, ...props }: React.ComponentProps<'ul'>) {
  return (
    <ul
      data-slot="list"
      className={cn('divide-y divide-border overflow-hidden rounded-xl border', className)}
      {...props}
    />
  )
}

/**
 * One row. Give it `onSelect` and the whole row becomes the control.
 *
 * That is deliberate, and it is why there is no per-row icon button here. Two
 * 32px glyphs sitting 4px apart cannot both carry a 44px touch target without
 * their hit areas overlapping, and the fix for that is not a bigger glyph — it
 * is to stop having two targets. Editing is the row; removing lives inside the
 * editor, which is also where the confirmation belongs.
 */
function ListRow({
  title,
  subtitle,
  trailing,
  onSelect,
  className,
  ...props
}: Omit<React.ComponentProps<'li'>, 'title' | 'onSelect'> & {
  title: React.ReactNode
  subtitle?: React.ReactNode
  /** Quiet supporting value, right-aligned. Stands down on narrow screens. */
  trailing?: React.ReactNode
  onSelect?: () => void
}) {
  const body = (
    <>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">{title}</div>
        {subtitle && <div className="truncate text-[0.8rem] text-muted-foreground">{subtitle}</div>}
      </div>
      {trailing && (
        <div className="hidden shrink-0 text-[0.8rem] text-muted-foreground sm:block">
          {trailing}
        </div>
      )}
    </>
  )

  return (
    <li data-slot="list-row" className={cn('bg-card', className)} {...props}>
      {onSelect ? (
        <button
          type="button"
          onClick={onSelect}
          // min-h-14 is 56px, so the row clears the 44px minimum on its own.
          className="flex min-h-14 w-full items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-accent/60 focus-visible:-outline-offset-2 focus-visible:outline-2 focus-visible:outline-ring"
        >
          {body}
          <ChevronRight className="size-4 shrink-0 text-muted-foreground/60" aria-hidden />
        </button>
      ) : (
        <div className="flex min-h-14 items-center gap-3 px-4 py-2.5">{body}</div>
      )}
    </li>
  )
}

/**
 * An empty state that keeps the section's shape whether or not it has rows.
 */
function ListEmpty({ children }: { children: React.ReactNode }) {
  return (
    <div className="rounded-xl border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">
      {children}
    </div>
  )
}

export { List, ListRow, ListEmpty }
