import { t, useLocale } from '@/lib/i18n'
import { LoaderCircle, RefreshCw } from 'lucide-react'

import { PageHeading } from '@/components/layout/page-heading'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Heading } from '@/components/ui/heading'
import { Skeleton } from '@/components/ui/skeleton'
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
const SAMPLE_COUNT = 10

type Site = GatewayLatency['custom'][number]
type Sample = NonNullable<Site['samples']>[number]

function formatLatency(ms: number) {
  return ms < 1 ? '<1 ms' : `${Math.round(ms)} ms`
}

// Older daemons and the first round after a restart have no history; fall back
// to the one completed sample so a card is never blank while it has a value.
function history(site: Site): Sample[] {
  if (site.samples?.length) return site.samples.slice(-SAMPLE_COUNT)
  if (site.milliseconds != null) return [{ milliseconds: site.milliseconds, checked_at: site.checked_at }]
  return []
}

function average(samples: Sample[]) {
  const values = samples.flatMap((sample) => sample.milliseconds == null ? [] : [sample.milliseconds])
  if (values.length === 0) return null
  return values.reduce((sum, value) => sum + value, 0) / values.length
}

function tone(sample: Sample | null) {
  if (!sample) return 'bg-muted'
  const ms = sample.milliseconds
  if (ms == null) return 'bg-destructive/70'
  if (ms < 100) return 'bg-success'
  if (ms < 300) return 'bg-warning'
  return 'bg-destructive'
}

function sampleTitle(sample: Sample | null) {
  if (!sample) return t('No sample')
  if (sample.milliseconds == null) return sample.error || t('Failed')
  return `${formatLatency(sample.milliseconds)} · ${new Date(sample.checked_at).toLocaleTimeString()}`
}

export function LatencyPage() {
  useLocale()
  const latency = useGatewayLatency()
  const measure = useMeasureLatencySite()
  const testAll = useMeasureAllLatency()
  const sites = latency.data?.custom ?? []
  const testing = testAll.isPending || sites.some((site) => site.measuring)
  const pendingCategory = typeof testAll.variables === 'string' ? testAll.variables : undefined

  return <div className="space-y-10">
    <PageHeading title={t('Latency')} action={<Button variant="secondary" onClick={() => testAll.mutate(undefined)} disabled={testing}>{testing ? t('Testing…') : t('Test all')}</Button>} />
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
  const groups = CATEGORY_ORDER
    .map((category) => ({ category, label: CATEGORY_LABELS[category], sites: sites.filter((site) => site.builtin && site.category === category) }))
    .filter((group) => group.sites.length > 0)
  const custom = sites.filter((site) => !site.builtin)

  const card = (site: Site) => {
    const samples = history(site)
    const slots: (Sample | null)[] = [...Array<Sample | null>(Math.max(0, SAMPLE_COUNT - samples.length)).fill(null), ...samples]
    const averageMs = average(samples)
    return <button key={site.name} type="button" onClick={() => onMeasure(site.name)}
      aria-busy={Boolean(site.measuring)}
      aria-label={`${site.name}: ${averageMs == null ? t('No samples yet') : t('Average {value}', { value: formatLatency(averageMs) })}`}
      title={`${site.name} · ${site.url} via ${site.exit || 'router default'}`}
      className="flex min-w-0 items-start gap-3 rounded-xl border bg-card p-3 text-left transition-colors hover:bg-muted/40 focus-visible:outline-2 focus-visible:outline-ring">
      <span className="flex size-12 shrink-0 items-center justify-center rounded-xl bg-muted/60"><SiteIcon name={site.name} url={site.url} icon={site.icon} big /></span>
      <span className="min-w-0 flex-1 space-y-2">
        <span className="flex items-baseline justify-between gap-2">
          <span className="min-w-0 truncate text-sm font-medium">{site.name}</span>
          <span className="flex shrink-0 items-baseline gap-1 text-xs tabular-nums text-muted-foreground">
            {site.measuring && <LoaderCircle className="size-3 animate-spin" aria-hidden />}
            {averageMs == null ? '—' : <><span>{t('Avg')}</span><span className="text-foreground">{formatLatency(averageMs)}</span></>}
          </span>
        </span>
        <span className="grid grid-cols-10 gap-1" aria-hidden>
          {slots.map((sample, index) => <span key={index} title={sampleTitle(sample)} className={`h-2.5 rounded-[3px] ${tone(sample)}`} />)}
        </span>
      </span>
    </button>
  }

  const section = (category: string, label: string, list: Site[]) => {
    const busy = list.some((site) => site.measuring) || pendingCategory === category
    return <section key={category} aria-label={t(label)} className="space-y-4">
      <div className="flex items-center justify-between gap-2">
        <Heading as="h2" size="lg">{t(label)}</Heading>
        <Button variant="quiet" size="icon-sm" aria-label={`${t('Refresh')} ${t(label)}`} disabled={busy} onClick={() => onRefresh(category)}>
          <RefreshCw className={busy ? 'animate-spin' : undefined} aria-hidden />
        </Button>
      </div>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">{list.map(card)}</div>
    </section>
  }

  return <div className="space-y-10">
    {groups.map((group) => section(group.category, group.label, group.sites))}
    {custom.length > 0 && section('custom', 'Custom', custom)}
  </div>
}
