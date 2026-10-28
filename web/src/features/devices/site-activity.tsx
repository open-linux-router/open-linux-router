import { t, useLocale } from '@/lib/i18n'
import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'
import { formatBytes } from '@/lib/utils'

/**
 * What the device did while inspection was on, one axis.
 *
 * Height is traffic and colour is the site the bytes belong to. The series is
 * sampled on the router only while the switch is on and dropped when it is
 * turned off, so the chart starts when the switch does and the footer says so
 * rather than leaving a silently short history on screen.
 *
 * The router also reports the window, so a new bucket takes its place beside
 * the ones already there instead of stretching the chart, and the area covers
 * only the minutes that have happened rather than drawing the rest as traffic
 * that stopped.
 */

interface SiteUsage { site: string; up: number; down: number }
interface Bucket { at: string; sites: SiteUsage[] }
interface Response {
  buckets: Bucket[]
  since?: string
  window_seconds: number
  bucket_seconds: number
  sample_seconds: number
}

const palette = ['#ff3d54', '#fb7299', '#7fb3d5', '#25f4ee', '#ff8a00', '#60a5fa', '#a78bfa']
const otherColor = 'var(--border)'
const unnamedColor = 'var(--input)'

export function SiteActivity({ addresses }: { addresses: string[] }) {
  useLocale()
  const query = useQuery({
    queryKey: ['inspection', 'activity', addresses.join(',')],
    queryFn: () => api.get<Response>(`/api/inspection/activity?${addresses.map((a) => `address=${encodeURIComponent(a)}`).join('&')}`),
    refetchInterval: 15000,
    enabled: addresses.length > 0,
  })

  if (addresses.length === 0) return <p className="px-4 py-4 text-sm text-muted-foreground sm:px-6">{t('This device has no address yet.')}</p>
  if (query.isError) return <p className="px-4 py-4 text-sm text-muted-foreground sm:px-6">{t('The activity series is unavailable.')}</p>
  if (query.isPending) return <p className="px-4 py-4 text-sm text-muted-foreground sm:px-6">{t('Loading activity…')}</p>

  const data = query.data
  const buckets = data.buckets ?? []
  if (buckets.length === 0) return <p className="px-4 py-4 text-sm text-muted-foreground sm:px-6">{t('No traffic recorded yet. The chart fills in as the router samples connections.')}</p>

  const bucketSeconds = Math.max(1, data.bucket_seconds)
  const slots = Math.max(1, Math.round(data.window_seconds / bucketSeconds))
  const started = Date.parse(data.since ?? buckets[0].at)

  // top sites over the session, everything else folded into 其他
  const totals = new Map<string, number>()
  for (const bucket of buckets) for (const row of bucket.sites) {
    totals.set(row.site, (totals.get(row.site) ?? 0) + row.up + row.down)
  }
  const ranked = [...totals.entries()].sort((a, b) => b[1] - a[1])
  const named = ranked.filter(([site]) => site !== '未命名' && site !== '其他')
  const kept = named.slice(0, 6).map(([site]) => site)
  const colorOf = (site: string) => {
    if (site === '未命名') return unnamedColor
    if (site === '其他') return otherColor
    const index = kept.indexOf(site)
    return index >= 0 ? palette[index % palette.length] : otherColor
  }

  const keys = [...kept, ...(named.length > kept.length ? ['其他'] : []), '未命名']
  const values = keys.map(() => new Array(slots).fill(0))
  const columnTotals = new Array(slots).fill(0)
  let lastSlot = 0
  for (const bucket of buckets) {
    const slot = Math.min(slots - 1, Math.max(0, Math.round((Date.parse(bucket.at) - started) / 1000 / bucketSeconds)))
    lastSlot = Math.max(lastSlot, slot)
    for (const row of bucket.sites) {
      const site = row.site
      const key = keys.includes(site) ? site : '其他'
      const index = keys.indexOf(key)
      if (index < 0) continue
      values[index][slot] += row.up + row.down
      columnTotals[slot] += row.up + row.down
    }
  }
  const grand = columnTotals.reduce((a, b) => a + b, 0)
  // The axis is the whole session, but the area only covers the minutes that
  // have happened: drawing the rest as zero would read as traffic that stopped.
  const drawn = Math.min(slots, lastSlot + 1)

  const width = 1000
  const height = 132
  const base = height - 4
  const max = Math.max(...columnTotals.slice(0, drawn), 1)
  const x = (i: number) => (slots === 1 ? 0 : (i / (slots - 1)) * width)
  const lower = new Array(drawn).fill(0)
  const paths = keys.map((key, slot) => {
    const top: number[] = []
    const bottom: number[] = []
    for (let i = 0; i < drawn; i++) {
      const total = columnTotals[i]
      const share = total > 0 ? values[slot][i] / total : 0
      const scaled = Math.sqrt(total / max) * (base - 8)
      const y0 = lower[i]
      const y1 = y0 + scaled * share
      bottom.push(y0)
      top.push(y1)
      lower[i] = y1
    }
    let d = ''
    for (let i = 0; i < top.length; i++) d += `${i ? 'L' : 'M'}${x(i).toFixed(1)},${(base - top[i]).toFixed(1)}`
    for (let i = top.length - 1; i >= 0; i--) d += `L${x(i).toFixed(1)},${(base - bottom[i]).toFixed(1)}`
    return <path key={key} d={`${d}Z`} fill={colorOf(key)} fillOpacity={0.95} />
  })

  return <div className="px-4 pb-4 sm:px-6">
    <div className="mb-1 flex items-baseline gap-4">
      <span className="text-2xl font-semibold tabular-nums">{formatBytes(grand)}</span>
      <span className="text-sm text-muted-foreground">{t('in this inspection')}</span>
    </div>
    <svg viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" className="block h-28 w-full" role="img" aria-label={t('Traffic during this inspection, by site')}>
      {paths}
      <line x1="0" y1={base} x2={width} y2={base} stroke="var(--border)" strokeWidth="1" />
    </svg>
    <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
      {keys.filter((key) => grand > 0 && (totals.get(key) ?? 0) > 0).map((key) => (
        <span key={key} className="inline-flex items-center gap-1.5">
          <i className="size-2 rounded-[2px]" style={{ background: colorOf(key) }} />
          <span className="text-foreground">{key}</span>
          <b className="font-medium tabular-nums">{Math.round((100 * (totals.get(key) ?? 0)) / grand)}%</b>
        </span>
      ))}
      <span className="ml-auto">{t('Recorded while inspection is on; turning it off clears it.')}</span>
    </div>
  </div>
}
