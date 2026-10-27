import { t, useLocale } from '@/lib/i18n'
import { cn } from '@/lib/utils'

/**
 * One direction's dial, drawn the way LibreSpeed draws its own: a half circle
 * with the reading inside and the direction underneath.
 *
 * The scale is LibreSpeed's logarithmic one (0 to 10 Gbps), because a linear
 * scale makes a 100 Mbit link and a 1 Gbit link look almost the same. The
 * download arc takes the receive colour and the upload arc the transmit colour,
 * so the two dials are told apart the way the traffic meters tell them apart.
 */
export function SpeedDial({ label, value, direction, active }: {
  label: string
  value: number | null
  direction: 'download' | 'upload'
  active: boolean
}) {
  useLocale()
  const radius = 88
  const length = Math.PI * radius
  const fraction = value == null || value <= 0 ? 0 : Math.min(1, Math.log10(value + 1) / Math.log10(10_001))
  const arc = `M12,112 A${radius},${radius} 0 0 1 188,112`
  // The arc's leading end, marked like a gauge pointer.
  const tip = Math.PI * (1 - fraction)
  const knob = { x: 100 + radius * Math.cos(tip), y: 112 - radius * Math.sin(tip) }
  return <div className="flex flex-col items-center gap-2">
    <div
      className="relative w-64 shrink-0 sm:w-80"
      role="img"
      aria-label={`${label}: ${value == null ? t('Unavailable') : `${formatReading(value)} Mbps`}`}
    >
      <svg viewBox="0 0 200 120" className="w-full" aria-hidden="true">
        {/* A direction that is being measured but has not reported a rate yet
            keeps its track breathing, so the page does not look stopped. */}
        <path d={arc} fill="none" strokeWidth="9" strokeLinecap="round" className={cn('stroke-border', active && value == null && 'animate-pulse')} />
        {fraction > 0 && <path
          d={arc} fill="none" strokeWidth="9" strokeLinecap="round"
          className={cn('transition-[stroke-dashoffset] duration-300 ease-out', direction === 'download' ? 'stroke-receive' : 'stroke-transmit')}
          strokeDasharray={length}
          strokeDashoffset={length * (1 - fraction)}
        />}
        {fraction > 0 && <circle
          cx={knob.x} cy={knob.y} r="7" strokeWidth="3"
          className={cn('stroke-card', direction === 'download' ? 'fill-receive' : 'fill-transmit')}
        />}
        <text x="100" y="86" textAnchor="middle" className="fill-foreground text-[40px] font-semibold tabular-nums">
          {value == null ? '—' : formatReading(value)}
        </text>
        <text x="100" y="104" textAnchor="middle" className="fill-muted-foreground text-[12px]">Mbps</text>
      </svg>
    </div>
    <span className={cn('text-xs font-medium tracking-[0.14em] uppercase', active ? 'text-foreground' : 'text-muted-foreground')}>{label}</span>
  </div>
}

/** How a dial shows a reading: the engine's own rounding, so 9.99 stays legible. */
function formatReading(value: number): string {
  if (value < 10) return value.toFixed(2)
  if (value < 100) return value.toFixed(1)
  return value.toFixed(0)
}
