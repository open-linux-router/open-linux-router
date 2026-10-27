import { t, useLocale } from '@/lib/i18n'
import { cn } from '@/lib/utils'

/**
 * One direction's dial on an internet speed test page.
 *
 * LibreSpeed's page draws a speed on a logarithmic scale, because a linear one
 * makes a 100 Mbit link and a 1 Gbit link look almost the same; the tool pages
 * that borrow its look keep that scale, 0 to 10 Gbps. The ring is the same
 * construction as the quality score ring, so the two dials read as measurements
 * rather than as decoration. An unmeasured direction shows a dash, never a zero.
 */
export function SpeedDial({ label, value, active }: { label: string; value: number | null; active: boolean }) {
  useLocale()
  const radius = 52
  const circumference = 2 * Math.PI * radius
  const fraction = value == null || value <= 0 ? 0 : Math.min(1, Math.log10(value + 1) / Math.log10(10_001))
  return <div className="flex flex-col items-center gap-3">
    <div
      className="relative size-40 shrink-0 sm:size-44"
      role="img"
      aria-label={`${label}: ${value == null ? t('Unavailable') : `${formatReading(value)} Mbps`}`}
    >
      <svg viewBox="0 0 120 120" className="size-full -rotate-90" aria-hidden="true">
        {/* A direction that is being measured but has not reported a rate yet
            keeps its ring breathing, so the page does not look stopped. */}
        <circle cx="60" cy="60" r={radius} className={cn('fill-none stroke-border', active && value == null && 'animate-pulse')} strokeWidth="8" />
        {/* The arc grows from the top like a dial: it is the direction's live
            reading, and it settles where the measurement stops. */}
        {fraction > 0 && <circle
          cx="60" cy="60" r={radius}
          className="fill-none stroke-primary transition-[stroke-dashoffset] duration-300 ease-out"
          strokeWidth="8" strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={circumference * (1 - fraction)}
        />}
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center" aria-hidden="true">
        <span className="text-3xl leading-none font-semibold tracking-tight text-foreground tabular-nums">
          {value == null ? '—' : formatReading(value)}
        </span>
        <span className="mt-1.5 text-[10px] font-medium tracking-wider text-muted-foreground">Mbps</span>
      </div>
    </div>
    <span aria-hidden="true" className={active ? 'text-sm font-medium text-foreground' : 'text-sm text-muted-foreground'}>{label}</span>
  </div>
}

/** How a dial shows a reading: the engine's own rounding, so 9.99 stays legible. */
function formatReading(value: number): string {
  if (value < 10) return value.toFixed(2)
  if (value < 100) return value.toFixed(1)
  return value.toFixed(0)
}
