import { KeyRound } from 'lucide-react'

import { WayMark } from '@/features/remote/way-mark'
import type { WayKey } from '@/features/remote/way-marks'
import { cn } from '@/lib/utils'

/**
 * The way in, worn on the corner of a device's picture.
 *
 * A device that dialled in is drawn as a device everywhere it appears — the
 * map's band and its own page use the same picture, because an operator reads
 * "Li's Macbook Pro" as a laptop either way (docs/remote-access.md §3.3–3.4).
 * So the one thing that says it arrived through the tunnel has to sit *on* the
 * picture rather than in place of it: a badge, the way an app icon wears the
 * account it is signed in to. It costs the card no width, it cannot be mistaken
 * for the machine's identity, and it is the method in its own colour rather
 * than a second word to read.
 *
 * The white disc is the backing a protocol's artwork already gets on the Access
 * page — the WireGuard mark is red, and red needs a light ground in either theme
 * — and the ring in the surface's colour is what separates the disc from the
 * tile underneath, so it reads as a badge rather than as a second picture. Both
 * are `aria-hidden`: the card names the tunnel in its tooltip and the page names
 * it in its Way in row, so the badge never carries the only copy of the fact.
 */
export function WayBadge({
  way = 'wireguard',
  size = 'sm',
  className,
}: {
  /** Which way in. The band and a peer's page hold one kind today. */
  way?: WayKey
  /** The picture it sits on: 32px in the map's cards, 80px on a device page. */
  size?: 'sm' | 'lg'
  className?: string
}) {
  return (
    <span
      aria-hidden
      className={cn(
        'absolute flex items-center justify-center rounded-full bg-white ring-card',
        size === 'sm' ? 'size-3.5 -right-1 -bottom-1 ring-2' : 'size-6 -right-1.5 -bottom-1.5 ring-[3px]',
        className,
      )}
    >
      <WayMark way={way} fallback={KeyRound} className={size === 'sm' ? 'size-[9px]' : 'size-4'} />
    </span>
  )
}
