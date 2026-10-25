import { t, useLocale } from '@/lib/i18n'
import { Check } from 'lucide-react'
import { useState } from 'react'

import { Input } from '@/components/ui/input'
import thesvgSlugs from '@/features/gateway/thesvg-slugs.json'
import { DeviceIcon } from '@/features/devices/device-icon'
import {
  BRAND_GROUPS,
  CATEGORY_GROUPS,
  OS_KEYS,
  OS_MARKS,
  categoryLabel,
  variantChoices,
} from '@/features/devices/icons'
import type { DeviceCategory } from '@/lib/config-types'
import { cn } from '@/lib/utils'

/** What the picker chooses: a category, and optionally a picture that overrides its own. */
export interface IconChoice {
  category: DeviceCategory
  icon: string
}

/**
 * Picks what a device looks like, by its picture.
 *
 * A drop-down would be smaller, but the thing being chosen *is* an image, and
 * choosing "nas" from a list of twenty-odd words to find out what it looks like
 * is a guess-and-check loop. Showing the pictures makes the choice the same
 * shape as the result.
 *
 * Vendor pictures are grouped by maker, one section each, instead of being
 * listed in one grid: someone looking for the phone they own is looking for
 * Apple, not for the thirty-first tile. Which makers get a heading is derived
 * from the artwork rather than decided here — see BRAND_GROUPS.
 *
 * Built-in tiles and library logos preserve these distinctions:
 *
 *   - **A category** sets the category and clears any chosen icon, so the
 *     picture goes back to following category and vendor.
 *   - **A variant** — a 4-bay NAS — sets both too, and sits beside its
 *     category's own tile, where someone looking for their NAS will look.
 *   - **A vendor's picture** — iPhone, MacBook — sets both: an iPhone *is* a
 *     phone, and the label should say so. This is the only road to the Apple
 *     pictures for a device whose randomised MAC hides who made it, which is
 *     most Apple devices.
 *   - **An operating system** sets only the icon and leaves the category
 *     alone. A Debian VM is still a server; the mark is how it is drawn, not
 *     what it is.
 *   - **A theSVG logo** also sets only the icon, preserving the category.
 *
 * Categories without artwork are shown too, wearing a line glyph instead (see
 * DeviceIcon). Hiding them would be worse: the category is real and the label is
 * correct, and the operator's answer is worth storing now so that it is already
 * right on the day the picture lands.
 */
export function CategoryPicker({
  value,
  detected,
  onChange,
}: {
  /** The stored override, with '' for each half nothing is set for. */
  value: IconChoice
  /** What detection thinks, shown as the state of the "Detected" choice. */
  detected?: DeviceCategory
  onChange: (next: IconChoice) => void
}) {
  useLocale()
  const auto = value.category === '' && value.icon === ''
  const [search, setSearch] = useState('')
  const query = search.trim().toLowerCase()
  const matches = query ? thesvgSlugs.filter((slug) => slug.includes(query)).slice(0, 32) : []
  const selectedSlug = value.icon.startsWith('thesvg:') ? value.icon.slice(7) : ''

  return (
    <div className="space-y-4">
      {/* Clearing the override is a distinct action, not a value in the grid —
          "let detection decide" and "it is definitely an unknown thing" are
          different answers and the UI must not collapse them. */}
      <button
        type="button"
        onClick={() => onChange({ category: '', icon: '' })}
        aria-pressed={auto}
        className={cn(
          'flex min-h-11 w-full items-center gap-2.5 rounded-lg border px-3 text-left text-sm transition-colors',
          auto ? 'border-primary bg-primary/5 font-medium' : 'hover:bg-accent/60',
        )}
      >
        {auto && <Check className="size-4 shrink-0 text-primary" aria-hidden />}
        <span className="min-w-0 flex-1 truncate">{t('Detect automatically')}{detected ? (
            <span className="text-muted-foreground"> — currently {categoryLabel(detected)}</span>
          ) : (
            <span className="text-muted-foreground"> {t('— nothing detected')}</span>
          )}
        </span>
      </button>

      <div className="space-y-2">
        <Input aria-label={t('Search theSVG icons')} placeholder={t('Search theSVG (e.g. proxmox)')} value={search} onChange={(event) => setSearch(event.target.value)} />
        {(query || selectedSlug) && <Section label="theSVG">
          {(query ? matches : [selectedSlug]).map((slug) => (
            <Tile key={slug} label={slug} selected={value.icon === `thesvg:${slug}`}
              onClick={() => onChange({ category: value.category, icon: `thesvg:${slug}` })}>
              <DeviceIcon icon={`thesvg:${slug}`} category={value.category} size="md" />
            </Tile>
          ))}
        </Section>}
        {query && matches.length === 0 && <p role="status" className="text-sm text-muted-foreground">{t('No matching icons.')}</p>}
        <p className="text-xs text-muted-foreground">{t('Icons by theSVG (MIT). Only the selected ID is saved.')}</p>
      </div>

      {CATEGORY_GROUPS.map((group) => (
        <Section key={group.label} label={group.label}>
          {group.categories.flatMap((category) => [
            <Tile
              key={category}
              label={categoryLabel(category)}
              selected={value.icon === '' && value.category === category}
              onClick={() => onChange({ category, icon: '' })}
            >
              <DeviceIcon category={category} size="md" />
            </Tile>,
            ...variantChoices(category).map((choice) => (
              <Tile
                key={choice.icon}
                label={choice.label}
                selected={value.icon === choice.icon}
                onClick={() => onChange({ category: choice.category, icon: choice.icon })}
              >
                <DeviceIcon icon={choice.icon} category={choice.category} size="md" />
              </Tile>
            )),
          ])}
        </Section>
      ))}

      {BRAND_GROUPS.map((group) => (
        <Section key={group.label} label={t(group.label)}>
          {group.choices.map((choice) => (
            <Tile
              key={choice.icon}
              label={choice.label}
              selected={value.icon === choice.icon}
              onClick={() => onChange({ category: choice.category, icon: choice.icon })}
            >
              <DeviceIcon icon={choice.icon} category={choice.category} size="md" />
            </Tile>
          ))}
        </Section>
      ))}

      <Section label={t('Operating system')}>
        {OS_KEYS.map((os) => (
          <Tile
            key={os}
            label={OS_MARKS[os].label}
            selected={value.icon === `os/${os}`}
            onClick={() => onChange({ category: value.category, icon: `os/${os}` })}
          >
            <DeviceIcon icon={`os/${os}`} category="unknown" size="md" />
          </Tile>
        ))}
      </Section>
    </div>
  )
}

function Section({ label, children }: { label: string; children: React.ReactNode }) {
  useLocale()
  return (
    <div className="space-y-2">
      <div className="text-xs font-medium text-muted-foreground">{label}</div>
      <div className="grid grid-cols-3 gap-1.5 sm:grid-cols-4">{children}</div>
    </div>
  )
}

function Tile({
  label,
  selected,
  onClick,
  children,
}: {
  label: string
  selected: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  useLocale()
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      aria-pressed={selected}
      title={label}
      className={cn(
        'flex min-h-20 flex-col items-center justify-center gap-1 rounded-lg border px-1.5 py-2 transition-colors',
        selected ? 'border-primary bg-primary/5' : 'border-transparent hover:bg-accent/60',
      )}
    >
      {children}
      <span
        className={cn(
          'w-full truncate text-center text-[0.7rem] leading-tight',
          selected ? 'font-medium' : 'text-muted-foreground',
        )}
      >
        {label}
      </span>
    </button>
  )
}
