import { createElement } from 'react'

import { deviceGlyph, deviceIcon } from '@/features/devices/icons'
import type { DeviceCategory } from '@/lib/config-types'
import { cn } from '@/lib/utils'

/**
 * A device's picture, or its glyph.
 *
 * Two presentations, because the icon set is filled in gradually and the gap
 * has to look deliberate. A category with artwork gets the photograph; one
 * without gets a line glyph in a quiet tile. What it never gets is the
 * *unknown photograph*, which would put an identical grey box on a doorbell, a
 * speaker and a smart plug and make the list read as broken.
 *
 * The photographs carry no baked shadow (see ICONS.md) precisely so the UI can
 * supply one, which is what stops a matte render on a flat card looking like a
 * sticker. It is kept subtle: this is a list of things you own, not a shop.
 *
 * Offline devices are dimmed and desaturated rather than hidden or flattened to
 * a silhouette. The device is still yours and still worth recognising at a
 * glance; what changed is whether it is here.
 */
export function DeviceIcon({
  category,
  online,
  size = 'md',
  className,
}: {
  category: DeviceCategory
  online?: boolean
  size?: 'sm' | 'md' | 'lg'
  className?: string
}) {
  const box = { sm: 'size-8', md: 'size-11', lg: 'size-20' }[size]
  const dimmed = online === false

  const photo = deviceIcon(category)
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

  // createElement rather than binding the glyph to a capitalised local and
  // rendering <Glyph />: the lookup returns an existing component from a
  // module-level map, but that pattern reads to the linter as constructing a
  // component during render, and this codebase has no lint suppressions to
  // follow.
  return (
    <div
      aria-hidden
      className={cn(
        box,
        'flex shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground transition-opacity',
        dimmed && 'opacity-50',
        className,
      )}
    >
      {createElement(deviceGlyph(category), {
        className: size === 'lg' ? 'size-9' : size === 'sm' ? 'size-4' : 'size-5',
      })}
    </div>
  )
}
