import { createElement } from 'react'

import { deviceGlyph, deviceIcon, vendorInitials } from '@/features/devices/icons'
import type { VendorKey } from '@/lib/api-types'
import type { DeviceCategory } from '@/lib/config-types'
import { cn } from '@/lib/utils'

/**
 * A device's picture, its vendor's initials, or its glyph.
 *
 * Three presentations, because the icon set is filled in gradually and the gap
 * has to look deliberate. What it never shows is the *unknown photograph* on a
 * device that is merely undrawn — an identical grey box on a doorbell, a
 * speaker and a smart plug makes the list read as broken.
 *
 *   1. **A photograph**, when something has been drawn for this device. Which
 *      one is deviceIcon's business; it reads the vendor as well as the
 *      category, most specifically drawn first.
 *   2. **The vendor's initials**, when nothing has, and nothing says what kind
 *      of thing this is. See below — this is the case a network with DHCP off
 *      is made entirely of.
 *   3. **A line glyph** for the category, otherwise.
 *
 * Initials before the glyph, but only when the category is unknown, and the
 * order is the whole argument. A category glyph says "printer", which is what
 * someone scanning a list is actually looking for; initials say "Brother",
 * which is worth less. But when the category is *unknown* the glyph is a
 * question mark — and a column of identical question marks tells you nothing at
 * all, while "TP", "WN" and "HW" tell you these are three different things
 * before you have read a single line of text.
 *
 * Initials rather than artwork for that case, deliberately. Drawing "an Apple
 * something" means choosing whether to draw a phone, a watch or a laptop, and
 * inventing a kind of device is the one thing ICONS.md and detect.go both
 * refuse to do. Letters claim only what is known. They also work for all thirty
 * thousand vendors in the registry rather than the few dozen anyone will ever
 * draw, which no asset-based answer can.
 *
 * The photographs carry no baked shadow (see ICONS.md) precisely so the UI can
 * supply one, which is what stops a cut-out picture on a flat card looking like a
 * sticker. It is kept subtle: this is a list of things you own, not a shop.
 *
 * Offline devices are dimmed and desaturated rather than hidden or flattened to
 * a silhouette. The device is still yours and still worth recognising at a
 * glance; what changed is whether it is here.
 */
export function DeviceIcon({
  category,
  vendor,
  vendorKey,
  online,
  size = 'md',
  className,
}: {
  category: DeviceCategory
  /**
   * Who built it, as a person reads it — "TP-Link", "WNC". Used for the
   * initials, so it is worth passing even for the vendors that will never have
   * artwork, which is nearly all of them.
   */
  vendor?: string
  /**
   * The same vendor as an artwork key, present only for the few dozen we might
   * have drawn. Both are optional because not every caller has a device: the
   * category picker is showing the vocabulary itself, not anything anyone owns.
   */
  vendorKey?: VendorKey
  online?: boolean
  size?: 'sm' | 'md' | 'lg'
  className?: string
}) {
  const box = { sm: 'size-8', md: 'size-11', lg: 'size-20' }[size]
  const dimmed = online === false

  // Worked out before the picture, because it is what deviceIcon needs in order
  // to know whether the anonymous grey box is still the right last resort.
  const initials = category === 'unknown' || category === '' ? vendorInitials(vendor) : ''

  const photo = deviceIcon(category, vendorKey, initials !== '')
  if (photo) {
    return (
      <img
        src={photo}
        // Empty alt: the row already names the device and its category in text,
        // so announcing the picture too would make a screen reader say
        // everything twice. It is decoration for an already-labelled row.
        alt=""
        aria-hidden
        loading="lazy"
        decoding="async"
        className={cn(
          box,
          'shrink-0 object-contain transition-[filter,opacity]',
          'drop-shadow-[0_1px_2px_rgb(0_0_0/0.18)] dark:drop-shadow-[0_1px_3px_rgb(0_0_0/0.5)]',
          dimmed && 'opacity-45 saturate-50',
          className,
        )}
      />
    )
  }

  // The quiet tile, shared by the two lettering-or-glyph cases so they are the
  // same object with different contents rather than two things that have to be
  // kept looking alike.
  const tile = cn(
    box,
    'flex shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground transition-opacity',
    dimmed && 'opacity-50',
    className,
  )

  if (initials) {
    return (
      // Named rather than aria-hidden, which is where this parts company with
      // every other tile here. A category glyph is decoration because the row
      // already says "Printer" in text; "WN" is the only place the row mentions
      // WNC at all, so hiding it would delete the one fact it carries. The full
      // name is a click away in the detail dialog either way.
      <div title={vendor} aria-label={vendor} className={tile}>
        {/* Tabular figures and tight tracking so two- and three-letter marks
            sit on the same optical centre down a column. Sized by hand rather
            than by a scale step: these have to survive next to a 32px
            photograph without looking like a caption that lost its picture. */}
        <span
          className={cn(
            'font-medium tabular-nums tracking-tight',
            size === 'lg' ? 'text-lg' : size === 'sm' ? 'text-[0.6rem]' : 'text-[0.7rem]',
          )}
        >
          {initials}
        </span>
      </div>
    )
  }

  // createElement rather than binding the glyph to a capitalised local and
  // rendering <Glyph />: the lookup returns an existing component from a
  // module-level map, but that pattern reads to the linter as constructing a
  // component during render, and this codebase has no lint suppressions to
  // follow.
  return (
    <div aria-hidden className={tile}>
      {createElement(deviceGlyph(category), {
        className: size === 'lg' ? 'size-9' : size === 'sm' ? 'size-4' : 'size-5',
      })}
    </div>
  )
}
