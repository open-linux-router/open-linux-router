import { t, useLocale } from '@/lib/i18n'
import { LoaderCircle, Pin } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'

import { PageHeading } from '@/components/layout/page-heading'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { hourRange, hourlyLatency, latencyDay, type HourlySample } from '@/features/gateway/hourly-latency'
import { SiteIcon } from '@/features/gateway/latency-site-icon'
import { useGatewayLatency, useMeasureLatencySite, usePinLatencySite } from '@/features/gateway/queries'
import type { GatewayLatency } from '@/lib/api-types'
import { cn } from '@/lib/utils'

function formatLatency(ms: number) {
  return ms < 1 ? '<1 ms' : `${Math.round(ms)} ms`
}

function tone(sample: HourlySample | null) {
  if (!sample) return 'bg-muted'
  const ms = sample.milliseconds
  // A failed attempt and an hour nobody measured are one colour: neither is a
  // value on the day's scale, and the tooltip names which happened.
  if (ms == null) return 'bg-muted'
  if (ms < 100) return 'bg-success'
  if (ms < 500) return 'bg-warning'
  return 'bg-destructive'
}

function averageLatency(slots: (HourlySample | null)[]) {
  let total = 0
  let count = 0
  for (const sample of slots) {
    if (sample?.milliseconds != null) {
      total += sample.milliseconds
      count += 1
    }
  }
  return count > 0 ? total / count : null
}

function averageTone(ms: number | null) {
  if (ms == null) return 'text-muted-foreground'
  if (ms < 100) return 'text-success-foreground'
  if (ms < 500) return 'text-warning-foreground'
  return 'text-destructive'
}

function sampleDetail(sample: HourlySample | null) {
  if (!sample) return t('No sample')
  return sample.milliseconds == null ? sample.error || t('Failed') : formatLatency(sample.milliseconds)
}

function sleep(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

const CATEGORY_ORDER = ['communication', 'social', 'entertainment', 'news', 'shopping', 'tools', 'other', 'custom']
const CATEGORY_LABELS: Record<string, string> = {
  communication: 'Communication',
  social: 'Social',
  entertainment: 'Entertainment',
  news: 'News',
  shopping: 'Shopping',
  tools: 'Tools',
  other: 'Other',
  custom: 'Custom',
}

/** Split the merged catalog and custom list into its own reading order. */
function groupSites(sites: GatewayLatency['custom']) {
  const groups = new Map<string, GatewayLatency['custom']>()
  for (const site of sites) {
    const key = site.builtin ? site.category ?? 'other' : 'custom'
    const group = groups.get(key)
    if (group) group.push(site)
    else groups.set(key, [site])
  }
  const rank = (key: string) => {
    const index = CATEGORY_ORDER.indexOf(key)
    return index === -1 ? CATEGORY_ORDER.indexOf('other') : index
  }
  return [...groups.entries()]
    .sort(([a], [b]) => rank(a) - rank(b))
    .map(([key, group]) => ({ key, label: CATEGORY_LABELS[key] ?? key.charAt(0).toUpperCase() + key.slice(1), sites: group }))
}

/**
 * One hour of one site. While its probe runs the point is a spinner; when the
 * result publishes, the spinner cross-fades into the new square, so the eye
 * follows a single point rather than a table repainting under it.
 */
function HourlyPoint({ sample, hour, date, testing, current }: { sample: HourlySample | null; hour: number; date: string; testing: boolean; current: boolean }) {
  const [open, setOpen] = useState(false)
  const detail = testing ? t('Testing…') : sampleDetail(sample)
  const label = `${date} ${hourRange(hour)} · ${detail}`
  return <Tooltip open={open} onOpenChange={setOpen}>
    <TooltipTrigger delay={0} aria-label={label} data-hour={hour} onClick={() => setOpen(true)}
      className="flex h-8 w-6 shrink-0 items-center justify-center rounded-sm focus-visible:outline-2 focus-visible:outline-ring">
      <span className="relative block size-5 shrink-0">
        <span className={`absolute inset-0 rounded-[3px] transition-all duration-200 ease-out motion-reduce:transition-none ${tone(sample)} ${testing ? 'scale-75 opacity-0' : 'scale-100 opacity-100'}`} />
        <span className={`absolute inset-0 flex items-center justify-center rounded-[3px] bg-muted transition-all duration-200 ease-out motion-reduce:transition-none ${testing ? 'scale-100 opacity-100' : 'scale-75 opacity-0'}`}>
          {/* Kept mounted for the current hour so it fades out with its square
              instead of vanishing a frame before the result arrives. */}
          {current && <LoaderCircle aria-hidden className={cn('size-3.5 text-primary', testing ? 'animate-spin motion-reduce:animate-none' : '[animation-play-state:paused]')} />}
        </span>
      </span>
    </TooltipTrigger>
    <TooltipContent className="flex-col items-start animate-none!"><span>{date} {hourRange(hour)}</span><span>{detail}</span></TooltipContent>
  </Tooltip>
}

export function LatencyPage() {
  useLocale()
  const latency = useGatewayLatency()
  const measure = useMeasureLatencySite()
  const sites = latency.data?.custom ?? []
  const [run, setRun] = useState<{ index: number; total: number } | null>(null)
  const [runError, setRunError] = useState('')
  const running = useRef(false)
  const testing = run !== null || sites.some((site) => site.measuring)

  // A closed tab must stop the queue, not leave it probing in the background.
  useEffect(() => () => { running.current = false }, [])

  // Test all walks the list one site at a time. The daemon's batch endpoint
  // starts every probe at once; this page asks for them in order so each row
  // gets its own spinner and result, which is the only way the progress stays
  // readable across twenty-six probes.
  async function testAll() {
    if (running.current || !latency.data) return
    const list = latency.data.custom ?? []
    if (!list.length) return
    running.current = true
    setRunError('')
    for (let index = 0; index < list.length && running.current; index += 1) {
      const site = list[index]
      setRun({ index, total: list.length })
      try {
        await measure.mutateAsync(site.name)
        // Give the spinner time to arrive before the first poll can end it.
        await sleep(420)
        const deadline = Date.now() + 20_000
        while (running.current && Date.now() < deadline) {
          const result = await latency.refetch()
          const row = result.data?.custom.find((entry) => entry.name === site.name)
          if (!row?.measuring) break
          await sleep(200)
        }
      } catch (error) {
        setRunError(error instanceof Error ? error.message : String(error))
      }
      await sleep(140)
    }
    if (running.current) {
      running.current = false
      setRun(null)
    }
  }

  return <div className="space-y-8">
    <PageHeading title={t('Latency')} action={<Button variant="secondary" onClick={() => { void testAll() }} disabled={testing} aria-busy={testing}>
      {run ? t('Testing {current}/{total}', { current: run.index + 1, total: run.total }) : testing ? t('Testing…') : t('Test all')}
    </Button>} />
    {latency.isError && <Alert variant="destructive">
      <AlertTitle>{t('Could not read measurements')}</AlertTitle>
      <AlertDescription className="space-y-3">
        <p>{latency.error.message}</p>
        <Button variant="secondary" onClick={() => { void latency.refetch() }}>{t('Retry')}</Button>
      </AlertDescription>
    </Alert>}
    {latency.data ? <LatencySites latency={latency.data} />
      : !latency.isError && <Skeleton className="h-64 w-full" />}
    {runError && <p role="alert" className="text-sm text-destructive">{runError}</p>}
  </div>
}

function LatencySites({ latency }: { latency: GatewayLatency }) {
  useLocale()
  const sites = latency.custom ?? []
  const day = latencyDay(latency.local_time)
  const pin = usePinLatencySite()

  return <div className="space-y-3">
    {pin.isError && <p role="alert" className="text-sm text-destructive">{t('Could not update pin for {name}. Try again.', { name: pin.variables?.name ?? '' })}</p>}
    <div className="overflow-x-auto rounded-xl border bg-card" role="region" aria-label={t('Latency')} tabIndex={0}>
      <div className="min-w-[872px] py-2 sm:min-w-[912px]">
        {groupSites(sites).map((group, index) => <section key={group.key} aria-labelledby={`latency-group-${group.key}`} className={index === 0 ? undefined : 'mt-4'}>
          <div className="flex h-10 items-center px-3 sm:px-4">
            <h2 id={`latency-group-${group.key}`} className="sticky left-3 z-10 flex h-10 shrink-0 items-center bg-card pr-2 text-base font-semibold text-foreground sm:left-4">
              {t(group.label)}
            </h2>
          </div>
          <div role="list">
            {group.sites.map((site) => {
              const slots = hourlyLatency(site, day)
              const average = averageLatency(slots)
              return <div key={site.name} role="listitem" data-site={site.name}
                className="group/row grid min-h-14 grid-cols-[176px_minmax(576px,1fr)_80px] items-center gap-2 px-3 sm:grid-cols-[192px_minmax(576px,1fr)_88px] sm:gap-3 sm:px-4">
                <div className="sticky left-0 z-10 -ml-3 flex min-h-14 min-w-0 items-center gap-2 bg-card pl-3 pr-2 sm:-ml-4 sm:pl-4"
                  title={`${site.name} · ${site.url}`}>
                  <button type="button" data-pin={Boolean(site.pinned)} aria-pressed={Boolean(site.pinned)} disabled={pin.isPending}
                    aria-label={t(site.pinned ? 'Unpin {name} from Overview' : 'Pin {name} to Overview', { name: site.name })}
                    aria-busy={pin.isPending && pin.variables?.name === site.name}
                    onClick={() => pin.mutate({ name: site.name, pinned: !site.pinned })}
                    className={cn('flex size-6 shrink-0 items-center justify-center rounded-md transition hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring disabled:cursor-wait',
                      site.pinned || (pin.isPending && pin.variables?.name === site.name) ? 'opacity-100' : 'opacity-0 group-hover/row:opacity-100 focus-visible:opacity-100 pointer-coarse:opacity-100',
                      site.pinned ? 'text-primary' : 'text-muted-foreground/50 hover:text-muted-foreground')}
                    title={t(site.pinned ? 'Unpin from Overview' : 'Pin to Overview')}>
                    {pin.isPending && pin.variables?.name === site.name
                      ? <LoaderCircle aria-hidden className="size-3.5 animate-spin text-muted-foreground" />
                      : <Pin aria-hidden className={cn('size-3.5', site.pinned && 'fill-primary/20')} />}
                  </button>
                  <span className="flex size-8 shrink-0 items-center justify-center"><SiteIcon name={site.name} url={site.url} icon={site.icon} /></span>
                  <span className="min-w-0 flex-1 truncate text-sm font-medium">{site.name}</span>
                </div>
                <div className="flex items-center" role="group" aria-label={t('{name}: hourly latency for {date}', { name: site.name, date: day.date })}>
                  {slots.map((sample, hour) => <HourlyPoint key={hour} sample={sample} hour={hour} date={day.date}
                    testing={Boolean(site.measuring) && hour === day.hour} current={hour === day.hour} />)}
                </div>
                <span className={cn('text-right text-sm font-medium tabular-nums', averageTone(average))}
                  aria-label={average == null ? t('No measurements today') : t('Average {value} today', { value: formatLatency(average) })}
                  title={average == null ? t('No measurements today') : t('Average {value} today', { value: formatLatency(average) })}>
                  {average == null ? '—' : formatLatency(average)}
                </span>
              </div>
            })}
          </div>
        </section>)}
      </div>
    </div>
  </div>
}
