import { createElement } from 'react'
import type { LucideIcon } from 'lucide-react'

import { WAY_MARKS, type WayKey } from '@/features/remote/way-marks'
import { cn } from '@/lib/utils'


export function WayMark({
  way,
  fallback,
  className,
}: {
  /** Which way in, when the caller knows it. */
  way?: WayKey
  /** The line glyph for a way with no mark — SOCKS5, or a status too old to say. */
  fallback: LucideIcon
  className?: string
}) {
  const src = way ? WAY_MARKS[way] : undefined
  if (!src) return createElement(fallback, { className })

  return (
    // Empty alt: every caller already names the way in text right beside it
    // ("via WireGuard"), so announcing the artwork would say it twice.
    <img src={src} alt="" aria-hidden className={cn('object-contain', className)} />
  )
}
