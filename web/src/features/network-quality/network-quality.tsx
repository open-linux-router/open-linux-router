import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Disclosure } from '@/components/ui/disclosure'
import { Heading } from '@/components/ui/heading'
import { MetricValue } from '@/components/ui/metric-value'
import { PropertyRow, TechnicalValue } from '@/components/ui/property-row'
import { Switch } from '@/components/ui/switch'
import { useNetworkQuality, useQualityAction } from './queries'
import type { QualityCategory, QualitySample, QualitySnapshot } from './types'

const axes = ['Speed', 'Latency', 'Stability', 'Wi-Fi', 'Functionality']

function point(index: number, score: number, radius = 86) {
  const angle = -Math.PI / 2 + index * Math.PI * 2 / axes.length
  return [150 + Math.cos(angle) * radius * score / 100, 116 + Math.sin(angle) * radius * score / 100]
}

function polygon(score: number) {
  return axes.map((_, index) => point(index, score).join(',')).join(' ')
}

const points = (value: number) => Number(value.toFixed(1)).toString()
const categoryState = { ready: 'Measured', partial: 'Limited measurements', stale: 'Stale', unavailable: 'Not measured' }

function stateLabel(data: QualitySnapshot | undefined) {
  if (!data) return 'Loading…'
  if (data.state === 'running') return 'Testing…'
  if (data.overall.state === 'stale') return 'Stale'
  if (data.state === 'cancelled') return 'Cancelled · Partial'
  if (data.state === 'timed_out') return 'Timed out · Partial'
  if (data.overall.state === 'ready') return `${Math.round(data.overall.score!)} / 100`
  if (data.overall.available_points > 0) return 'Partial'
  return data.state === 'idle' ? 'Not tested' : 'No scored results'
}

export function NetworkQuality() {
  const query = useNetworkQuality()
  const [open, setOpen] = useState(false)
  // A failed read must not keep a cached healthy result looking current.
  const data = query.isError ? undefined : query.data
  const categories = axes.map(label => data?.categories.find(category => category.label === label))
  const freshScore = (category: QualityCategory | undefined) => category?.state === 'stale' ? null : category?.score ?? null
  const complete = categories.every(category => freshScore(category) != null)

  return <>
    <section aria-label="Quality" className="flex w-full min-w-0 flex-col gap-2 panel-surface p-3 sm:p-4 lg:min-h-60 lg:aspect-[4/3] lg:h-fit">
      <div className="flex h-6 items-center justify-between gap-2">
        <Heading size="sm"><button type="button" className="relative z-10 -my-2.5 min-h-11 py-2 text-left hover:text-primary focus-visible:outline-2 focus-visible:outline-ring" onClick={() => setOpen(true)} aria-label="Network quality details">Quality</button></Heading>
        <span className="text-xs text-muted-foreground">{query.isError ? 'Unavailable' : stateLabel(data)}</span>
      </div>
      <button type="button" onClick={() => setOpen(true)} aria-label="Open network quality assessment" className="-mx-1.5 -mb-1.5 flex min-h-0 min-w-0 flex-1 items-center justify-center rounded-lg focus-visible:outline-2 focus-visible:outline-ring sm:-mx-2 sm:-mb-2">
        <svg viewBox="0 -12 340 245" className="min-h-0 w-full flex-1" role="img" aria-label={`Network quality scores out of 100. ${axes.map((label, i) => `${label}: ${freshScore(categories[i]) == null ? 'not measured or stale' : Math.round(freshScore(categories[i])!)}${categories[i]?.state === 'partial' ? ' (partial)' : ''}`).join('. ')}. Higher is better.`}>
          <g transform="translate(20 0)">
            {[25, 50, 75, 100].map(score => <polygon key={score} points={polygon(score)} className="fill-none stroke-border" strokeWidth="1" />)}
            {axes.map((label, index) => {
              const [x, y] = point(index, 100)
              return <line key={label} x1="150" y1="116" x2={x} y2={y} className="stroke-border" />
            })}
            {complete && <polygon points={categories.map((category, index) => point(index, freshScore(category)!).join(',')).join(' ')} className="fill-primary/15 stroke-primary" strokeWidth="2" strokeLinejoin="round" />}
            {axes.map((label, index) => {
              const score = freshScore(categories[index])
              const [labelX, labelY] = point(index, 100, 108)
              const [x, y] = point(index, score ?? 0)
              return <g key={label}>
                {score != null && <circle cx={x} cy={y} r="3" className="fill-primary stroke-card" strokeWidth="1.5" />}
                <text x={labelX} y={labelY} textAnchor="middle" className="fill-muted-foreground text-[15px]">{label} <tspan className="fill-foreground font-semibold tabular-nums">{score == null ? '—' : Math.round(score)}</tspan></text>
              </g>
            })}
          </g>
        </svg>
      </button>
      <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
        <span>{data?.overall.state === 'ready' ? `Based on ${data.categories.filter(category => category.score != null).length} measured categories` : 'Router default route'}</span>
        <Button variant="ghost" size="sm" onClick={() => setOpen(true)}>Test</Button>
      </div>
    </section>
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Network quality</DialogTitle>
          <DialogDescription>Tests run from this router over its default route. The score reflects the measurements available in this assessment.</DialogDescription>
        </DialogHeader>
        <QualityDetails data={data} readError={query.isError} retry={() => query.refetch()} />
      </DialogContent>
    </Dialog>
  </>
}

function QualityDetails({ data, readError, retry }: { data?: QualitySnapshot; readError: boolean; retry: () => void }) {
  const [speed, setSpeed] = useState(false)
  const action = useQualityAction()
  const running = data?.state === 'running'
  const bandwidth = running ? data.include_speed : speed
  const phase = { responses: 'Checking service responses', speed: 'Measuring download and upload', nat: 'Observing NAT behavior', '': '' }

  return <div className="space-y-5">
    {readError && <div role="alert" className="text-sm text-destructive">Could not read the assessment. <Button variant="link" onClick={retry}>Retry</Button></div>}
    <div className="flex flex-wrap items-center justify-between gap-3">
      <label className="flex min-h-11 items-center gap-3 text-sm"><Switch checked={bandwidth} onCheckedChange={setSpeed} disabled={running || action.isPending} />Include bandwidth test</label>
      {running
        ? <Button variant="outline" disabled={action.isPending} onClick={() => action.mutate({ cancel: true })}>Cancel test</Button>
        : <Button disabled={action.isPending || readError} onClick={() => action.mutate({ speed })}>{action.isPending ? 'Starting…' : 'Run assessment'}</Button>}
    </div>
    <p className="text-sm text-muted-foreground">{bandwidth ? 'Download and upload testing can use substantial data and affect other traffic. Allow up to 150 seconds.' : 'Checks small HTTPS responses and STUN. No bulk download or upload. Allow up to 60 seconds.'} Results remain in memory for this daemon session and become stale after 30 minutes.</p>
    {action.isError && <p role="alert" className="text-sm text-destructive">{action.error.message}</p>}
    <div className="flex flex-wrap items-baseline justify-between gap-2" role="status">
      <Heading as="h3" size="sm">{data?.overall.state === 'ready' ? 'Overall score' : data ? stateLabel(data) : readError ? 'Unavailable' : 'Loading…'}</Heading>
      {running && <span className="text-sm text-muted-foreground">{phase[data.phase]}</span>}
      {data?.overall.score != null && <MetricValue size="md" value={points(data.overall.score)} unit="/ 100" />}
    </div>
    {data && <>
      <p className="text-sm text-muted-foreground">Measured components and categories share the full 100 points. Unimplemented or unmeasured tests are excluded; confirmed failures still count. Wi-Fi requires client or AP measurements.</p>
      {data.started_at && <p className="text-xs text-muted-foreground">{data.completed_at ? 'Completed' : 'Started'} {new Date(data.completed_at ?? data.started_at).toLocaleString()} · {data.scoring_version}</p>}
      <div className="space-y-2">
        {data.categories.map(category => <Disclosure key={category.id} className="border-b last:border-0" summary={<span className="flex min-w-0 flex-1 flex-wrap items-center justify-between gap-2 py-2 text-foreground">
            <span className="font-medium">{category.label} <span className="font-normal text-muted-foreground">· {categoryState[category.state]}</span></span>
            <span className="tabular-nums">{category.score == null ? '—' : `${Math.round(category.score)} / 100`} <span className="text-muted-foreground">· {category.available_points > 0 ? `${points(category.earned_points)} points · ${points(category.available_points)}% weight` : 'Excluded from total'}</span></span>
          </span>}>
          <ul className="pb-3">
            {category.components.map(component => <li key={component.id} className="py-2 text-sm">
              <div className="flex items-start justify-between gap-4"><span>{component.label}</span><span className="shrink-0 tabular-nums">{component.score == null ? 'Not measured' : `${Math.round(component.score)} / 100`}</span></div>
              <p className="text-xs text-muted-foreground">{component.reason} · {component.score == null ? 'Excluded from category score' : `${points(component.effective_weight * 100)}% of category score`}</p>
            </li>)}
          </ul>
        </Disclosure>)}
      </div>
      {data.speed && <div>
        <Heading as="h3" size="sm">Reference transfer</Heading>
        <p className="text-sm text-muted-foreground">{data.speed.server} · {data.speed.location}. One test server, not a service-wide benchmark.</p>
        <dl><PropertyRow label="Download"><MetricValue value={data.speed.download_mbps.toFixed(1)} unit="Mbps" /></PropertyRow><PropertyRow label="Upload"><MetricValue value={data.speed.upload_mbps.toFixed(1)} unit="Mbps" /></PropertyRow></dl>
      </div>}
      {data.speed_error && <p className="text-sm text-destructive">Bandwidth test: {data.speed_error}</p>}
      {data.targets.length > 0 && <div className="space-y-2">
        <Heading as="h3" size="sm">Service resource measurements</Heading>
        <p className="text-sm text-muted-foreground">Each family uses six fresh connections to one resource. This checks that resource, not all features of the service. TCP timing is connection establishment; HTTPS timing includes server processing.</p>
        {data.targets.map(target => <Disclosure key={`${target.id}-${target.family}`} summary={`${target.name} · ${target.family.toUpperCase()} · ${target.samples.filter(sample => sample.state === 'ok').length}/${target.samples.length} expected responses`}>
          <p className="break-all text-xs text-muted-foreground">{target.url}</p>
          <ol className="space-y-3 py-3">
            {target.samples.map((sample, index) => <li key={index} className="text-sm">
              <span>{index + 1}. {sample.state} · {sample.stage}{sample.status ? ` · HTTP ${sample.status}` : ''}</span>
              <p className="text-xs text-muted-foreground">DNS {duration(sample, 'dns_ms')} · TCP {duration(sample, 'tcp_ms')} · HTTPS {duration(sample, 'response_ms')}</p>
              {sample.address && <TechnicalValue>{sample.address}</TechnicalValue>}
              {sample.error && <p className="break-words text-xs text-muted-foreground">{sample.error}</p>}
            </li>)}
          </ol>
        </Disclosure>)}
      </div>}
      {(data.nat || data.nat_error) && <Disclosure summary="NAT observations">
        {data.nat && <>
          <dl><PropertyRow label="STUN server"><TechnicalValue>{data.nat.server}</TechnicalValue></PropertyRow><PropertyRow label="Observed mapping"><TechnicalValue>{data.nat.mapped}</TechnicalValue></PropertyRow><PropertyRow label="Mapping">{data.nat.mapping}</PropertyRow><PropertyRow label="Filtering">{data.nat.filtering}</PropertyRow></dl>
          <p className="text-sm text-muted-foreground">An observed mapping does not prove public IPv4 assignment, inbound access or P2P success. These observations do not add points.</p>
        </>}
        {data.nat_error && <p className="break-words text-sm text-muted-foreground">{data.nat_error}</p>}
      </Disclosure>}
      <p className="text-xs text-muted-foreground">Total = sum of measured category scores × base weights ÷ sum of their base weights. Base weights: Speed 25, Latency 25, Stability 25, Wi-Fi 15, Functionality 10. Weights within each category are normalized in the same way. New tests may change the score in future scoring versions.</p>
    </>}
  </div>
}

function duration(sample: QualitySample, key: 'dns_ms' | 'tcp_ms' | 'response_ms') {
  const value = sample[key]
  return value == null ? '—' : value < 1 ? '<1 ms' : `${value.toFixed(1)} ms`
}
