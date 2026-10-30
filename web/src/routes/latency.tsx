import { t, useLocale } from '@/lib/i18n'
import { RefreshCw } from 'lucide-react'
import { useState } from 'react'

import { PageHeading } from '@/components/layout/page-heading'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Heading } from '@/components/ui/heading'
import { MetricValue } from '@/components/ui/metric-value'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { hourRange, hourlyLatency, latencyDay, type HourlySample, type LatencySite } from '@/features/gateway/hourly-latency'
import { SiteIcon } from '@/features/gateway/latency-site-icon'
import { useGatewayLatency, useMeasureAllLatency, useMeasureLatencySite } from '@/features/gateway/queries'
import type { GatewayLatency } from '@/lib/api-types'

const CATEGORY_ORDER = ['communication', 'social', 'entertainment', 'news', 'shopping', 'tools']
const CATEGORY_LABELS: Record<string, string> = {
  communication: 'Communication',
  social: 'Social',
  entertainment: 'Entertainment',
  news: 'News',
  shopping: 'Shopping',
  tools: 'Tools',
}
const HOURS = Array.from({ length: 24 }, (_, hour) => hour)
const ROW = 'grid grid-cols-[128px_360px_80px_36px] items-center gap-3 px-3 sm:grid-cols-[minmax(160px,1fr)_minmax(360px,2fr)_96px_36px] sm:gap-4 sm:px-4'

function formatLatency(ms: number) {
  return ms < 1 ? '<1 ms' : `${Math.round(ms)} ms`
}

function tone(sample: HourlySample | null) {
  if (!sample) return 'bg-muted'
  const ms = sample.milliseconds
  if (ms == null) return 'bg-destructive/70'
  if (ms < 100) return 'bg-success'
  if (ms < 300) return 'bg-warning'
  return 'bg-destructive'
}

function latencyTextTone(ms: number | null | undefined) {
  if (ms == null) return 'text-muted-foreground'
  if (ms < 100) return 'text-success-foreground'
  if (ms < 300) return 'text-warning-foreground'
  return 'text-destructive'
}

function sampleDetail(sample: HourlySample | null) {
  if (!sample) return t('No sample')
  return sample.milliseconds == null ? sample.error || t('Failed') : formatLatency(sample.milliseconds)
}

function HourlyPoint({ sample, hour, date }: { sample: HourlySample | null; hour: number; date: string }) {
  const [open, setOpen] = useState(false)
  const label = `${date} ${hourRange(hour)} · ${sampleDetail(sample)}`
  return <Tooltip open={open} onOpenChange={setOpen}>
    <TooltipTrigger delay={0} aria-label={label} data-hour={hour} onClick={() => setOpen(true)}
      className="flex h-8 min-w-0 items-center justify-center rounded-sm focus-visible:outline-2 focus-visible:outline-ring">
      <span className={`size-2.5 shrink-0 rounded-[3px] ${tone(sample)}`} />
    </TooltipTrigger>
    <TooltipContent className="flex-col items-start animate-none!"><span>{date} {hourRange(hour)}</span><span>{sampleDetail(sample)}</span></TooltipContent>
  </Tooltip>
}

export function LatencyPage() {
  useLocale()
  const latency = useGatewayLatency()
  const measure = useMeasureLatencySite()
  const testAll = useMeasureAllLatency()
  const sites = latency.data?.custom ?? []
  const testing = testAll.isPending || sites.some((site) => site.measuring)
  const pendingCategory = testAll.isPending && typeof testAll.variables === 'string' ? testAll.variables : undefined
  const day = latencyDay(latency.data?.local_time)

  return <div className="space-y-8">
    <PageHeading title={t('Latency')} action={<Button variant="secondary" onClick={() => testAll.mutate(undefined)} disabled={testing}>{testing ? t('Testing…') : t('Test all')}</Button>} />
    <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 text-sm text-muted-foreground">
      <span>{t('Router time')}: {day.date}{day.zone && ` · ${day.zone}`}</span>
      <span>{t('Auto test at :30 each hour. Manual tests update this hour.')}</span>
    </div>
    {latency.isError && <Alert variant="destructive">
      <AlertTitle>{t('Could not read measurements')}</AlertTitle>
      <AlertDescription className="space-y-3">
        <p>{latency.error.message}</p>
        <Button variant="secondary" onClick={() => { void latency.refetch() }}>{t('Retry')}</Button>
      </AlertDescription>
    </Alert>}
    {latency.data ? <LatencySites latency={latency.data} onMeasure={(name) => measure.mutate(name)}
      onRefresh={(category) => testAll.mutate(category)} pendingCategory={pendingCategory} />
      : !latency.isError && <Skeleton className="h-64 w-full" />}
    {testAll.isError && <p role="alert" className="text-sm text-destructive">{testAll.error.message}</p>}
    {measure.isError && <p role="alert" className="text-sm text-destructive">{measure.error.message}</p>}
  </div>
}

function LatencySites({ latency, onMeasure, onRefresh, pendingCategory }: {
  latency: GatewayLatency
  onMeasure: (name: string) => void
  onRefresh: (category: string) => void
  pendingCategory?: string
}) {
  useLocale()
  const sites = latency.custom ?? []
  const day = latencyDay(latency.local_time)
  const groups = CATEGORY_ORDER
    .map((category) => ({ category, label: CATEGORY_LABELS[category], sites: sites.filter((site) => site.builtin && site.category === category) }))
    .filter((group) => group.sites.length > 0)
  const custom = sites.filter((site) => !site.builtin)

  const row = (site: LatencySite) => {
    const slots = hourlyLatency(site, day)
    const latest = [...slots].reverse().find((sample) => sample != null)
    return <div key={site.name} role="listitem" data-site={site.name} className={`${ROW} min-h-14`}>
      <div className="sticky left-0 z-10 -ml-3 flex min-h-14 min-w-0 items-center gap-3 bg-card pl-3 pr-2 sm:-ml-4 sm:pl-4" title={`${site.name} · ${site.url}`}>
        <span className="flex size-8 shrink-0 items-center justify-center"><SiteIcon name={site.name} url={site.url} icon={site.icon} /></span>
        <span className="truncate text-sm font-medium">{site.name}</span>
      </div>
      <div className="grid grid-cols-24 items-center" role="group" aria-label={t('{name}: hourly latency for {date}', { name: site.name, date: day.date })}>
        {slots.map((sample, hour) => <HourlyPoint key={hour} sample={sample} hour={hour} date={day.date} />)}
      </div>
      <MetricValue size="md" className={`justify-self-end ${latencyTextTone(latest?.milliseconds)}`} value={latest?.milliseconds == null ? null : formatLatency(latest.milliseconds)} />
      <Button variant="quiet" size="icon-sm" aria-label={t('Test {name} this hour', { name: site.name })}
        title={t('Test {name} this hour', { name: site.name })} disabled={Boolean(site.measuring)} aria-busy={Boolean(site.measuring)} onClick={() => onMeasure(site.name)}>
        <RefreshCw className={site.measuring ? 'animate-spin' : undefined} aria-hidden />
      </Button>
    </div>
  }

  const section = (category: string, label: string, list: LatencySite[]) => {
    const busy = list.some((site) => site.measuring) || pendingCategory === category
    return <section key={category} aria-label={t(label)} className="space-y-4">
      <div className="flex items-center justify-between gap-2">
        <Heading as="h2" size="lg">{t(label)}</Heading>
        <Button variant="quiet" size="icon-sm" aria-label={`${t('Refresh')} ${t(label)}`} disabled={busy} onClick={() => onRefresh(category)}>
          <RefreshCw className={busy ? 'animate-spin' : undefined} aria-hidden />
        </Button>
      </div>
      <div className="overflow-x-auto rounded-xl border bg-card" role="region" aria-label={t('{category}: hourly latency', { category: t(label) })} tabIndex={0}>
        <div className="min-w-[680px] py-2 sm:min-w-[740px]">
          <div className={`${ROW} h-8 text-xs text-muted-foreground`} aria-hidden>
            <span />
            <div className="grid grid-cols-24 text-center tabular-nums">
              {HOURS.map((hour) => <span key={hour} className={hour === day.hour ? 'font-semibold text-primary' : undefined}>{String(hour).padStart(2, '0')}</span>)}
            </div>
            <span className="text-right">{t('Latest')}</span><span />
          </div>
          <div role="list">{list.map(row)}</div>
        </div>
      </div>
    </section>
  }

  return <div className="space-y-8">
    {groups.map((group) => section(group.category, group.label, group.sites))}
    {custom.length > 0 && section('custom', 'Custom', custom)}
  </div>
}
