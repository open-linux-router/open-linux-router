import { createElement } from 'react'
import type { LucideIcon } from 'lucide-react'

import { WAY_MARKS, type WayKey } from '@/features/remote/way-marks'
import { cn } from '@/lib/utils'

/**
 * How a connection arrived: the protocol's mark where we have one, the caller's
 * line glyph where we do not.
 *
 * The fallback is passed in rather than chosen here because the callers are
 * asking different questions. The map asks "who is this and how did they get
 * in", where a key says WireGuard and a globe says somebody else's proxy; the
 * proxy card asks the narrower question already answered by its own title, and
 * wants a plain globe for anything that is not Shadowsocks. One map of
 * fallbacks would have to pick one of those answers for both.
 *
 * The glyph stays inside the caller's quiet tile either way — a mark is flat
 * artwork, and floating one bare beside the device photographs made it look
 * like a sticker. See DeviceIcon for the same treatment applied to OS marks.
 */
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
