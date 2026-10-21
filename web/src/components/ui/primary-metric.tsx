import type { ReactNode } from 'react'

import { MetricValue } from '@/components/ui/metric-value'
import { textClass } from '@/components/ui/status'
import type { Tone } from '@/lib/state'
import { cn } from '@/lib/utils'

/**
 * A summary card's headline answer, as a figure or as the words that answer it.
 *
 * One per card, at the top of its content, so a screen of cards can be ranked
 * by weight instead of read row by row; everything under it is a supporting
 * row and keeps one shared, quieter treatment. The block reserves the same line
 * box at both sizes, which is what keeps a word answer beside a figure answer
 * on one baseline across the row.
 *
 * `size="md"` is for an answer made of words, where 20px already carries the
 * emphasis a 30px figure needs. `note` is one short line under the figure —
 * what was measured, or what it counts — never a second metric.
 */
export function PrimaryMetric({ value, unit, note, icon, size = 'lg', tone, onClick, ariaLabel, title, className }: {
  value: ReactNode
  unit?: string
  note?: ReactNode
  icon?: ReactNode
  size?: 'md' | 'lg'
  /** A verdict in words: the tone it is drawn in comes from `lib/state`. */
  tone?: Tone
  onClick?: () => void
  ariaLabel?: string
  title?: string
  className?: string
}) {
  const toneClass = tone ? textClass(tone) : undefined

  const body = <>
    <span className="flex min-w-0 items-center gap-2">
      {icon && <span className="flex size-4 shrink-0 items-center justify-center">{icon}</span>}
      <MetricValue size={size} value={value} unit={unit} className={cn('min-w-0', toneClass)} />
    </span>
    {note && <span className="mt-0.5 block min-w-0 text-xs leading-4 text-muted-foreground">{note}</span>}
  </>

  // Stretched by the card's column rather than given a width, so the negative
  // margin below can reach into the card's padding without breaking the grid.
  const shell = 'flex min-h-9 min-w-0 flex-col items-start justify-center text-left'
  if (!onClick) return <div className={cn(shell, className)}>{body}</div>
  return <button type="button" onClick={onClick} aria-label={ariaLabel} title={title}
    className={cn(shell, '-mx-1.5 min-h-11 rounded-md px-1.5 py-0.5 transition-colors hover:bg-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring sm:min-h-9', className)}>
    {body}
  </button>
}
