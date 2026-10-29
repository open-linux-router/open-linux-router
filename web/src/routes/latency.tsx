import { t, useLocale } from '@/lib/i18n'
import { LoaderCircle } from 'lucide-react'

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

function formatLatency(ms: number) {
  return ms < 1 ? '<1 ms' : `${Math.round(ms)} ms`
}

function siteValue(site: GatewayLatency['custom'][number]) {
  if (site.milliseconds == null) return '—'
  return formatLatency(site.milliseconds)
}

export function LatencyPage() {
  useLocale()
  const latency = useGatewayLatency()
  const measure = useMeasureLatencySite()
  const testAll = useMeasureAllLatency()
  const sites = latency.data?.custom ?? []
  const testing = testAll.isPending || sites.some((site) => site.measuring)

  return <div className="space-y-10">
    <div className="flex flex-wrap items-start justify-between gap-4">
      <PageHeading title={t('Latency')} description={t('Measure HTTPS latency from this router.')} />
      <Button variant="secondary" onClick={() => testAll.mutate()} disabled={testing}>{testing ? t('Testing…') : t('Test all')}</Button>
    </div>
    {latency.isError && <Alert variant="destructive">
      <AlertTitle>{t('Could not read measurements')}</AlertTitle>
      <AlertDescription className="space-y-3">
        <p>{latency.error.message}</p>
        <Button variant="secondary" onClick={() => { void latency.refetch() }}>{t('Retry')}</Button>
      </AlertDescription>
    </Alert>}
    {latency.data ? <LatencySites latency={latency.data} onMeasure={(name) => measure.mutate(name)} />
      : !latency.isError && <Skeleton className="h-64 w-full" />}
    {testAll.isError && <p role="alert" className="text-sm text-destructive">{testAll.error.message}</p>}
    {measure.isError && <p role="alert" className="text-sm text-destructive">{measure.error.message}</p>}
  </div>
}

function LatencySites({ latency, onMeasure }: { latency: GatewayLatency; onMeasure: (name: string) => void }) {
  useLocale()
  const sites = latency.custom ?? []
  const groups = CATEGORY_ORDER
    .map((category) => ({ category, label: CATEGORY_LABELS[category], sites: sites.filter((site) => site.builtin && site.category === category) }))
    .filter((group) => group.sites.length > 0)
  const custom = sites.filter((site) => !site.builtin)

  const tile = (site: GatewayLatency['custom'][number]) => <button key={site.name} type="button" onClick={() => onMeasure(site.name)}
    aria-busy={Boolean(site.measuring)} aria-label={`${site.name}: ${siteValue(site)}`}
    title={`${site.name} · ${site.url} via ${site.exit || 'router default'}${site.error ? ` · ${site.error}` : ''}`}
    className="group flex min-w-0 flex-col items-center gap-2 rounded-2xl p-2 text-center focus-visible:outline-2 focus-visible:outline-ring">
    <span className="flex size-20 items-center justify-center rounded-[22px] bg-muted/70 transition-colors group-hover:bg-muted">
      <SiteIcon name={site.name} url={site.url} icon={site.icon} big />
    </span>
    <span className="w-full truncate text-sm font-medium">{site.name}</span>
    <span className="flex h-4 items-center gap-1 text-xs tabular-nums text-muted-foreground">
      {site.measuring ? <><LoaderCircle className="size-3.5 animate-spin" aria-hidden />{t('Measuring…')}</> : siteValue(site)}
    </span>
  </button>

  return <div className="space-y-8">
    {groups.map((group) => <section key={group.category} aria-label={t(group.label)} className="space-y-3">
      <Heading as="h2" size="lg">{t(group.label)}</Heading>
      <div className="grid grid-cols-3 gap-x-4 gap-y-6 sm:grid-cols-4 lg:grid-cols-5">{group.sites.map(tile)}</div>
    </section>)}
    {custom.length > 0 && <section aria-label={t('Custom')} className="space-y-3">
      <Heading as="h2" size="lg">{t('Custom')}</Heading>
      <div className="grid grid-cols-3 gap-x-4 gap-y-6 sm:grid-cols-4 lg:grid-cols-5">{custom.map(tile)}</div>
    </section>}
  </div>
}
