import { Check } from 'lucide-react'

import { DeviceIcon } from '@/features/devices/device-icon'
import { CATEGORY_GROUPS, categoryLabel } from '@/features/devices/icons'
import type { DeviceCategory } from '@/lib/config-types'
import { cn } from '@/lib/utils'

/**
 * Picks a category by its picture.
 *
 * A drop-down would be smaller, but the thing being chosen *is* an image, and
 * choosing "nas" from a list of twenty-odd words to find out what it looks like
 * is a guess-and-check loop. Showing the pictures makes the choice the same
 * shape as the result.
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
  /** The stored override, or '' when nothing is set. */
  value: DeviceCategory
  /** What detection thinks, shown as the state of the "Detected" choice. */
  detected?: DeviceCategory
  onChange: (next: DeviceCategory) => void
}) {
  return (
    <div className="space-y-4">
      {/* Clearing the override is a distinct action, not a value in the grid —
          "let detection decide" and "it is definitely an unknown thing" are
          different answers and the UI must not collapse them. */}
      <button
        type="button"
        onClick={() => onChange('')}
        aria-pressed={value === ''}
        className={cn(
          'flex min-h-11 w-full items-center gap-2.5 rounded-lg border px-3 text-left text-sm transition-colors',
          value === ''
            ? 'border-primary bg-primary/5 font-medium'
            : 'hover:bg-accent/60',
        )}
      >
        {value === '' && <Check className="size-4 shrink-0 text-primary" aria-hidden />}
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
        <div key={group.label} className="space-y-2">
          <div className="text-xs font-medium text-muted-foreground">{group.label}</div>
          <div className="grid grid-cols-3 gap-1.5 sm:grid-cols-4">
            {group.categories.map((category) => {
              const selected = value === category
              return (
                <button
                  key={category}
                  type="button"
                  onClick={() => onChange(category)}
                  aria-pressed={selected}
                  title={categoryLabel(category)}
                  className={cn(
                    'flex min-h-20 flex-col items-center justify-center gap-1 rounded-lg border px-1.5 py-2 transition-colors',
                    selected
                      ? 'border-primary bg-primary/5'
                      : 'border-transparent hover:bg-accent/60',
                  )}
                >
                  <DeviceIcon category={category} size="md" />
                  <span
                    className={cn(
                      'w-full truncate text-center text-[0.7rem] leading-tight',
                      selected ? 'font-medium' : 'text-muted-foreground',
                    )}
                  >
                    {categoryLabel(category)}
                  </span>
                </button>
              )
            })}
          </div>
        </div>
      ))}
    </div>
  )
}
