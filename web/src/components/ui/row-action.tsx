import { MoreHorizontal, type LucideIcon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { cn } from '@/lib/utils'

/**
 * One action at the trailing edge of a row, and the menu for when there is more
 * than one.
 *
 * The grammar is the point: a chevron means "this row leads somewhere", a pencil
 * means "this row can be changed", an ellipsis means "there are more actions
 * here". Before this component those three were five different hit areas — a
 * 36px ghost button, a 36px link, a plain `<button>`, a 28px icon-sm and a
 * text-glyph button — so the same gesture was a different size from page to
 * page. Every one of them is now 36px on desktop and 44px under a finger.
 */
export function RowAction({
  icon: Icon,
  label,
  onClick,
  disabled,
  destructive,
  className,
}: {
  icon: LucideIcon
  /** The accessible name and the tooltip: a verb and its object. */
  label: string
  onClick: () => void
  disabled?: boolean
  destructive?: boolean
  className?: string
}) {
  return (
    <Button
      type="button"
      size="icon"
      variant="ghost"
      disabled={disabled}
      aria-label={label}
      title={label}
      onClick={onClick}
      className={cn('text-muted-foreground', destructive && 'text-destructive hover:bg-destructive/10 hover:text-destructive', className)}
    >
      <Icon aria-hidden />
    </Button>
  )
}

/**
 * The row's "more" menu.
 *
 * Reserved for rows with two or more secondary actions; a row with one action
 * shows that action's own icon, because a menu that opens to reveal a single
 * item costs a click and teaches nothing.
 */
export function RowMenu({
  label,
  children,
  className,
}: {
  /** The trigger's accessible name, e.g. "Living room options". */
  label: string
  children: React.ReactNode
  className?: string
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<Button type="button" size="icon" variant="ghost" aria-label={label} title={label} className="text-muted-foreground" />}
      >
        <MoreHorizontal aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className={cn('w-48', className)}>
        {children}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/** The navigation glyph at the end of a row that leads to a page. */
export function RowChevron({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
      className={cn('size-4 shrink-0 text-muted-foreground', className)}
    >
      <path d="m9 18 6-6-6-6" />
    </svg>
  )
}
