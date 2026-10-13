import { cn } from '@/lib/utils'

export function Meter({ label, current, maximum, percent, tone = 'blue', arrow, onClick }: {
  label: string
  current?: string
  maximum?: string
  percent?: number
  tone?: 'blue' | 'amber' | 'neutral'
  arrow?: '↓' | '↑'
  onClick?: () => void
}) {
  const content = <>
    {percent != null && <span aria-hidden className={cn('absolute inset-y-0 left-0 border-r transition-[width] duration-150 ease-out',
      tone === 'amber' ? 'border-transmit/30 bg-transmit-subtle' : tone === 'neutral' ? 'border-border bg-muted' : 'border-receive/30 bg-receive-subtle')}
      style={{ width: `${Math.min(100, Math.max(0, percent))}%` }} />}
    <span className="relative z-10 flex min-w-0 items-center gap-2 truncate text-sm font-semibold tabular-nums">
      {arrow && <span aria-hidden className="text-base font-normal text-muted-foreground">{arrow}</span>}{current ?? '—'}
    </span>
    <span className="relative z-10 text-xs text-muted-foreground tabular-nums">{maximum ? `of ${maximum}` : onClick ? 'Set limit' : '—'}</span>
  </>
  const classes = 'relative flex min-h-11 sm:min-h-9 w-full flex-wrap items-center justify-between gap-2 overflow-hidden rounded-lg bg-muted/50 px-3 py-1.5'
  return <div className="min-w-0">
    {onClick ? <button type="button" onClick={onClick} aria-label={`${label}: ${current ?? 'unavailable'}. ${maximum ? `Limit ${maximum}` : 'Set limit'}`}
      className={cn(classes, 'cursor-pointer transition-colors hover:bg-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring')}>
      {content}
    </button> : <>
      <div className="mb-2 flex items-center justify-between gap-2 text-xs text-muted-foreground"><span>{label}</span><span className="tabular-nums">{percent == null ? '—' : `${percent.toFixed(0)}%`}</span></div>
      <div role={percent == null ? undefined : 'progressbar'} aria-label={label} aria-valuenow={percent == null ? undefined : Math.min(100, Math.max(0, Math.round(percent)))} aria-valuemin={0} aria-valuemax={100} className={classes}>
        {content}
      </div>
    </>}
  </div>
}

export function MetricPill({ label, value, detail, percent, tone = 'blue', health, valueRight = false, meter = false }: {
  label?: string
  value: string
  detail?: string
  percent?: number
  tone?: 'blue' | 'neutral'
  health?: 'good' | 'bad' | 'unknown'
  valueRight?: boolean
  meter?: boolean
}) {
  return <div className="min-w-0"><div role={percent == null ? undefined : 'progressbar'} aria-label={label}
    aria-valuenow={percent == null ? undefined : Math.min(100, Math.max(0, Math.round(percent)))}
    aria-valuemin={0} aria-valuemax={100}
    className={cn('relative flex min-h-9 min-w-0 flex-wrap items-center justify-between gap-x-2 gap-y-0.5 py-1.5', meter && 'overflow-hidden rounded-lg bg-muted/50 px-3')}>
    {percent != null && <span aria-hidden className={cn('absolute inset-y-0 left-0 border-r transition-[width] duration-150 ease-out', tone === 'neutral' ? 'border-border bg-muted' : 'border-receive/30 bg-receive-subtle')}
      style={{ width: `${Math.min(100, Math.max(0, percent))}%` }} />}
    <span className={cn('relative z-10 flex min-w-0 items-center gap-2 text-sm', health ? 'flex-1' : 'flex-wrap', valueRight && 'w-full justify-between')}>
      {health && <span aria-hidden className={cn('size-2 shrink-0 rounded-full', health === 'good' ? 'bg-success' : health === 'bad' ? 'bg-destructive' : 'bg-muted-foreground')} />}
      {label && <span className="shrink-0 text-xs text-muted-foreground">{label}</span>}
      <span title={value} className={cn('min-w-0 break-words font-semibold tabular-nums', health === 'bad' && 'text-destructive')}>{value}</span>
    </span>
    {detail && <span className="relative z-10 shrink-0 text-xs text-muted-foreground tabular-nums">{detail}</span>}
  </div></div>
}
