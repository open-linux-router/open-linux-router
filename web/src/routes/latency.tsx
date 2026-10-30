import { t, useLocale } from '@/lib/i18n'
import { LoaderCircle, Pin } from 'lucide-react'
import { useState } from 'react'

import { PageHeading } from '@/components/layout/page-heading'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { hourRange, hourlyLatency, latencyDay, type HourlySample } from '@/features/gateway/hourly-latency'
import { SiteIcon } from '@/features/gateway/latency-site-icon'
import { useGatewayLatency, useMeasureAllLatency, usePinLatencySite } from '@/features/gateway/queries'
import type { GatewayLatency } from '@/lib/api-types'
import { cn } from '@/lib/utils'

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
      <span className={`size-4 shrink-0 rounded-[3px] ${tone(sample)}`} />
    </TooltipTrigger>
    <TooltipContent className="flex-col items-start animate-none!"><span>{date} {hourRange(hour)}</span><span>{sampleDetail(sample)}</span></TooltipContent>
  </Tooltip>
}

export function LatencyPage() {
  useLocale()
  const latency = useGatewayLatency()
  const testAll = useMeasureAllLatency()
  const sites = latency.data?.custom ?? []
  const testing = testAll.isPending || sites.some((site) => site.measuring)

  return <div className="space-y-8">
    <PageHeading title={t('Latency')} action={<Button variant="secondary" onClick={() => testAll.mutate(undefined)} disabled={testing}>{testing ? t('Testing…') : t('Test all')}</Button>} />
    {latency.isError && <Alert variant="destructive">
      <AlertTitle>{t('Could not read measurements')}</AlertTitle>
      <AlertDescription className="space-y-3">
        <p>{latency.error.message}</p>
        <Button variant="secondary" onClick={() => { void latency.refetch() }}>{t('Retry')}</Button>
      </AlertDescription>
    </Alert>}
    {latency.data ? <LatencySites latency={latency.data} />
      : !latency.isError && <Skeleton className="h-64 w-full" />}
    {testAll.isError && <p role="alert" className="text-sm text-destructive">{testAll.error.message}</p>}
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
      <div className="min-w-[768px] py-2 sm:min-w-[816px]" role="list">
        {sites.map((site) => <div key={site.name} role="listitem" data-site={site.name}
          className="grid min-h-14 grid-cols-[160px_minmax(576px,1fr)] items-center gap-2 px-3 sm:grid-cols-[192px_minmax(576px,1fr)] sm:gap-3 sm:px-4">
          <button type="button" data-pin={Boolean(site.pinned)} aria-pressed={Boolean(site.pinned)} disabled={pin.isPending}
            aria-label={t(site.pinned ? 'Unpin {name} from Overview' : 'Pin {name} to Overview', { name: site.name })}
            aria-busy={pin.isPending && pin.variables?.name === site.name}
            onClick={() => pin.mutate({ name: site.name, pinned: !site.pinned })}
            className="group/pin sticky left-0 z-10 -ml-3 flex min-h-14 min-w-0 items-center gap-2 bg-card pl-3 pr-2 text-left hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring focus-visible:-outline-offset-2 disabled:cursor-wait sm:-ml-4 sm:pl-4"
            title={`${site.name} · ${site.url} · ${t(site.pinned ? 'Unpin from Overview' : 'Pin to Overview')}`}>
            <span className="flex size-8 shrink-0 items-center justify-center"><SiteIcon name={site.name} url={site.url} icon={site.icon} /></span>
            <span className="min-w-0 flex-1 truncate text-sm font-medium">{site.name}</span>
            {pin.isPending && pin.variables?.name === site.name
              ? <LoaderCircle aria-hidden className="size-3.5 shrink-0 animate-spin text-muted-foreground" />
              : <Pin aria-hidden className={cn('size-3.5 shrink-0', site.pinned ? 'fill-primary/20 text-primary' : 'text-muted-foreground/50 group-hover/pin:text-muted-foreground group-focus-visible/pin:text-muted-foreground')} />}
          </button>
          <div className="grid grid-cols-24 items-center" role="group" aria-label={t('{name}: hourly latency for {date}', { name: site.name, date: day.date })}>
            {hourlyLatency(site, day).map((sample, hour) => <HourlyPoint key={hour} sample={sample} hour={hour} date={day.date} />)}
          </div>
        </div>)}
      </div>
    </div>
  </div>
}
