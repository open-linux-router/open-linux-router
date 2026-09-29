import { Check } from 'lucide-react'

import { DeviceIcon } from '@/features/devices/device-icon'
import {
  BRAND_CHOICES,
  CATEGORY_GROUPS,
  OS_KEYS,
  OS_MARKS,
  categoryLabel,
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
 * Three kinds of tile, and what each one stores is the whole design:
 *
 *   - **A category** sets the category and clears any chosen icon, so the
 *     picture goes back to following category and vendor.
 *   - **A vendor's picture** — iPhone, MacBook — sets both: an iPhone *is* a
 *     phone, and the label should say so. This is the only road to the Apple
 *     pictures for a device whose randomised MAC hides who made it, which is
 *     most Apple devices.
 *   - **An operating system** sets only the icon and leaves the category
 *     alone. A Debian VM is still a server; the mark is how it is drawn, not
 *     what it is.
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
  const auto = value.category === '' && value.icon === ''

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
        <span className="min-w-0 flex-1 truncate">
          Detect automatically
          {detected ? (
            <span className="text-muted-foreground"> — currently {categoryLabel(detected)}</span>
          ) : (
            <span className="text-muted-foreground"> — nothing detected</span>
          )}
        </span>
      </button>

      {CATEGORY_GROUPS.map((group) => (
        <Section key={group.label} label={group.label}>
          {group.categories.map((category) => (
            <Tile
              key={category}
              label={categoryLabel(category)}
              selected={value.icon === '' && value.category === category}
              onClick={() => onChange({ category, icon: '' })}
            >
              <DeviceIcon category={category} size="md" />
            </Tile>
          ))}
        </Section>
      ))}

      <Section label="Brands">
        {BRAND_CHOICES.map((choice) => (
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

      <Section label="Operating system">
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
  return (
    <button
      type="button"
      onClick={onClick}
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
