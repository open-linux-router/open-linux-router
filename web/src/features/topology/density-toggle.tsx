import { LayoutGrid, LayoutList } from 'lucide-react'

import type { Density } from '@/features/topology/layout'
import { cn } from '@/lib/utils'

const OPTIONS: { value: Density; label: string; icon: typeof LayoutGrid }[] = [
  { value: 'compact', label: 'Compact', icon: LayoutGrid },
  { value: 'detail', label: 'Detail', icon: LayoutList },
]

/**
 * Compact | Detail, as a segmented control. A radio group underneath, because
 * that is what it is: one of two, always one chosen. The labels drop to icons
 * on a phone, where the card header also has to hold "New group".
 */
export function DensityToggle({ value, onChange }: { value: Density; onChange: (d: Density) => void }) {
  return (
    <div role="radiogroup" aria-label="Node density" className="inline-flex h-7 items-center rounded-lg bg-muted p-0.5">
      {OPTIONS.map(({ value: v, label, icon: Icon }) => {
        const on = v === value
        return (
          <button
            key={v}
            type="button"
            role="radio"
            aria-checked={on}
            aria-label={label}
            title={label}
            onClick={() => onChange(v)}
            className={cn(
              'inline-flex h-6 items-center gap-1.5 rounded-md px-2 text-xs font-medium transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring/50',
              on ? 'bg-background text-foreground shadow-xs dark:bg-input/60' : 'text-muted-foreground hover:text-foreground',
            )}
          >
            <Icon className="size-3.5" aria-hidden />
            <span className="hidden sm:inline">{label}</span>
          </button>
        )
      })}
    </div>
  )
}
