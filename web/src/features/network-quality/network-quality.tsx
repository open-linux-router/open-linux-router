import { t, useLocale } from '@/lib/i18n'
import { useRef, useState } from 'react'
import { ArrowDown, ArrowUp, LoaderCircle, X } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Disclosure } from '@/components/ui/disclosure'
import { Heading } from '@/components/ui/heading'
import { MetricValue } from '@/components/ui/metric-value'
import { PropertyRow, TechnicalValue } from '@/components/ui/property-row'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'
import { useNetworkQuality, useQualityAction } from './queries'
import type { QualityCategory, QualitySample, QualitySnapshot } from './types'

const axes = ['Speed', 'Latency', 'Stability', 'Wireless', 'Functionality']

const categoryLabel = (label: string) => label === 'Wi-Fi' ? 'Wireless' : label

function point(index: number, score: number, radius = 86) {
  const angle = -Math.PI / 2 + index * Math.PI * 2 / axes.length
  return [150 + Math.cos(angle) * radius * score / 100, 116 + Math.sin(angle) * radius * score / 100]
}

function polygon(score: number) {
  return axes.map((_, index) => point(index, score).join(',')).join(' ')
}

const points = (value: number) => Number(value.toFixed(1)).toString()

type Tone = 'success' | 'warning' | 'destructive'

// Display bands only: they name the same 0–100 reference score and never change
// the calculation in docs/network-quality.md.
const grades: { min: number; label: string; variant: Tone }[] = [
  { min: 90, label: 'Excellent', variant: 'success' },
  { min: 75, label: 'Good', variant: 'success' },
  { min: 60, label: 'Fair', variant: 'warning' },
  { min: 0, label: 'Poor', variant: 'destructive' },
]

function gradeFor(score: number) {
  return grades.find(grade => score >= grade.min) ?? grades[grades.length - 1]
}

const toneText: Record<Tone, string> = {
  success: 'text-success-foreground',
  warning: 'text-warning-foreground',
  destructive: 'text-destructive',
}

const toneFill: Record<Tone, string> = {
  success: 'bg-success',
  warning: 'bg-warning',
  destructive: 'bg-destructive',
}

const categoryState: Record<QualityCategory['state'], string> = {
  ready: 'Measured',
  partial: 'Limited measurements',
  stale: 'Measured earlier',
  unavailable: 'Not measured',
}

const phaseText: Record<string, string> = {
  responses: 'Checking service responses',
  speed: 'Measuring download and upload',
  nat: 'Checking NAT behavior',
  '': 'Starting…',
}

export function NetworkQuality({ open, onOpenChange }: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  useLocale()
  const query = useNetworkQuality()
  const chart = useRef<HTMLButtonElement>(null)
  // A failed read must not keep a cached healthy result looking current.
  const data = query.isError ? undefined : query.data
  const categories = axes.map(label => data?.categories.find(category => categoryLabel(category.label) === label))
  const measuredScore = (category: QualityCategory | undefined) => category?.score ?? null
  // Wireless has a display-only default until client/AP measurements exist.
  const scores = categories.map((category, index) => measuredScore(category) ?? (axes[index] === 'Wireless' ? 80 : null))
  const complete = scores.every(score => score != null)

  return <>
    <section aria-label={t('Quality')} className="flex w-full min-w-0 flex-col gap-2 panel-surface p-3 sm:p-4 lg:min-h-60 lg:aspect-[4/3] lg:h-fit">
      <div className="flex h-6 items-center justify-between gap-2">
        <Heading size="sm"><button type="button" className="relative z-10 -my-2.5 min-h-11 py-2 text-left hover:text-primary focus-visible:outline-2 focus-visible:outline-ring" onClick={() => onOpenChange(true)} aria-label={t('Network quality details')}>{t('Quality')}</button></Heading>
      </div>
      <button ref={chart} type="button" onClick={() => onOpenChange(true)} aria-label={t('Open network quality assessment')} className="-mx-1.5 -mb-1.5 flex min-h-0 min-w-0 flex-1 items-center justify-center rounded-lg focus-visible:outline-2 focus-visible:outline-ring sm:-mx-2 sm:-mb-2">
        <svg viewBox="0 -12 340 245" className="min-h-0 w-full flex-1" role="img" aria-label={`Network quality scores out of 100. ${axes.map((label, i) => `${label}: ${scores[i] == null ? 'not measured' : Math.round(scores[i]!)}${label === 'Wireless' && measuredScore(categories[i]) == null ? ' (default)' : categories[i]?.state === 'partial' ? ' (partial)' : ''}`).join('. ')}. Higher is better.`}>
          <g transform="translate(20 0)">
            {[25, 50, 75, 100].map(score => <polygon key={score} points={polygon(score)} className="fill-none stroke-border" strokeWidth="1" />)}
            {axes.map((label, index) => {
              const [x, y] = point(index, 100)
              return <line key={label} x1="150" y1="116" x2={x} y2={y} className="stroke-border" />
            })}
            {complete && <polygon points={scores.map((score, index) => point(index, score!).join(',')).join(' ')} className="fill-primary/15 stroke-primary" strokeWidth="2" strokeLinejoin="round" />}
            {axes.map((label, index) => {
              const score = scores[index]
              const [labelX, labelY] = point(index, 100, 108)
              const [x, y] = point(index, score ?? 0)
              return <g key={label}>
                {score != null && <circle cx={x} cy={y} r="3" className="fill-primary stroke-card" strokeWidth="1.5" />}
                <text x={labelX} y={labelY} textAnchor="middle" className="fill-muted-foreground text-[15px]">{t(label)} <tspan className="fill-foreground font-semibold tabular-nums">{score == null ? '—' : Math.round(score)}</tspan></text>
              </g>
            })}
          </g>
        </svg>
      </button>
    </section>
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent showCloseButton={false} finalFocus={chart} className="flex h-dvh max-h-dvh w-screen max-w-none flex-col gap-0 overflow-hidden rounded-none p-0 sm:max-w-none sm:p-0">
        <DialogClose render={<Button variant="secondary" size="icon-lg" className="absolute top-3 right-3 z-10 rounded-full bg-card/95 shadow-md ring-1 ring-border backdrop-blur-sm sm:size-9" />}><X aria-hidden="true" /><span className="sr-only">{t('Close')}</span></DialogClose>
        <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
          <div className="mx-auto w-full max-w-3xl space-y-6 px-4 py-6 sm:px-6 sm:py-8">
            <DialogHeader className="pr-14">
              <DialogTitle>{t('Network quality')}</DialogTitle>
              <DialogDescription>{t('Measured from this router over its default route.')}</DialogDescription>
            </DialogHeader>
            <QualityDetails data={data} readError={query.isError} loading={query.isPending} retry={() => query.refetch()} />
          </div>
        </div>
      </DialogContent>
    </Dialog>
  </>
}

function heroCopy(data: QualitySnapshot | undefined, loading: boolean | undefined) {
  if (!data && loading) return { title: t('Loading…'), detail: t('Reading the latest assessment.') }
  if (!data || data.state === 'idle') return { title: t('Not tested'), detail: t('Run an assessment to score speed, latency, stability and functionality.') }
  if (data.state === 'running' || data.overall.state === 'measuring') {
    return { title: t('Testing network quality'), detail: t(phaseText[data.phase] ?? 'Starting…') }
  }
  if (data.state === 'cancelled') return { title: t('Assessment cancelled'), detail: t('The assessment did not finish, so no overall score is published. Partial measurements are kept.') }
  if (data.state === 'timed_out') return { title: t('Assessment timed out'), detail: t('The assessment did not finish, so no overall score is published. Partial measurements are kept.') }
  if (data.overall.state === 'stale') return { title: t('Results expired'), detail: t('Run the assessment again to score the current connection.') }
  if (data.overall.state === 'ready' && data.overall.score != null) {
    const all = data.categories.every(category => category.score != null)
    return {
      title: t('Overall score'),
      detail: all ? t('All categories measured.') : t('Unmeasured categories are excluded from the score.'),
    }
  }
  if (data.overall.available_points > 0) {
    return { title: t('Partial result'), detail: t('Some categories were scored, but no overall score is published for this assessment.') }
  }
  return { title: t('No scored results'), detail: t('The assessment produced no usable measurements. Run it again.') }
}

function QualityDetails({ data, readError, loading, retry }: {
  data?: QualitySnapshot
  readError: boolean
  loading?: boolean
  retry: () => void
}) {
  const locale = useLocale()
  const [speed, setSpeed] = useState(false)
  const action = useQualityAction()
  const running = data?.state === 'running'
  const bandwidth = running ? data.include_speed : speed
  const score = data && data.state === 'completed' && data.overall.state === 'ready' ? data.overall.score : null
  const grade = score == null ? null : gradeFor(score)
  const started = Boolean(data?.started_at)
  const categories = data?.categories ?? []
  const measured = categories.filter(category => category.score != null).length
  const copy = heroCopy(data, loading)

  if (readError) {
    return <Alert variant="destructive">
      <AlertTitle>{t('Could not read the assessment.')}</AlertTitle>
      <AlertDescription className="space-y-3">
        <p>{t('Check the router connection, then try again.')}</p>
        <Button variant="outline" size="sm" onClick={retry}>{t('Retry')}</Button>
      </AlertDescription>
    </Alert>
  }

  return <div className="space-y-6">
    <section className="panel-surface overflow-hidden" aria-labelledby="quality-result">
      <p className="sr-only" role="status" aria-live="polite">{copy.title}. {copy.detail}</p>
      <div className="flex flex-col gap-5 p-4 sm:flex-row sm:items-center sm:p-6">
        <ScoreRing score={score} busy={running || Boolean(loading && !data)} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <Heading as="h2" size="md" id="quality-result">{copy.title}</Heading>
            {grade && <Badge variant={grade.variant}>{t(grade.label)}</Badge>}
          </div>
          <p className="mt-1 text-sm text-muted-foreground">{copy.detail}</p>
          {data?.started_at && <dl className="mt-4 flex flex-wrap gap-x-8 gap-y-3">
            {!running && <div>
              <dt className="text-xs text-muted-foreground">{t('Measured')}</dt>
              <dd className="mt-0.5 font-medium tabular-nums">{t('{count} of {total}', { count: measured, total: categories.length })}</dd>
            </div>}
            <div>
              <dt className="text-xs text-muted-foreground">{t('Bandwidth test')}</dt>
              <dd className="mt-0.5 font-medium">{data.include_speed ? t('Included') : t('Not included')}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">{running ? t('Started') : t('Last run')}</dt>
              <dd className="mt-0.5 font-medium">{new Date(data.completed_at ?? data.started_at).toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' })}</dd>
            </div>
          </dl>}
        </div>
      </div>
      <div className="flex flex-col gap-3 border-t bg-muted/30 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-6">
        <label className="flex items-start gap-3 text-sm">
          <Switch checked={bandwidth} onCheckedChange={setSpeed} disabled={running || loading || action.isPending} aria-describedby="quality-run-scope" />
          <span>
            <span className="block font-medium">{t('Include bandwidth test')}</span>
            <span id="quality-run-scope" className="block text-xs text-muted-foreground">
              {bandwidth
                ? t('Adds download and upload. Uses substantial data and up to 150 seconds.')
                : t('Small HTTPS checks only. Up to 60 seconds.')}
            </span>
          </span>
        </label>
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-end">
          {running
            ? <Button variant="outline" disabled={action.isPending} onClick={() => action.mutate({ cancel: true })}>{t('Cancel test')}</Button>
            : <Button className="w-full sm:w-auto" disabled={action.isPending || loading} onClick={() => action.mutate({ speed })}>
                {action.isPending ? t('Starting…') : started ? t('Run again') : t('Run assessment')}
              </Button>}
        </div>
      </div>
    </section>
    {action.isError && <Alert variant="destructive">
      <AlertTitle>{t('Could not update the assessment')}</AlertTitle>
      <AlertDescription className="break-words">{action.error.message}</AlertDescription>
    </Alert>}
    {data?.speed_error && <Alert variant="destructive">
      <AlertTitle>{t('Bandwidth test failed')}</AlertTitle>
      <AlertDescription className="break-words">{data.speed_error}</AlertDescription>
    </Alert>}
    {data && !running && started && categories.length > 0 && <section className="space-y-3" aria-labelledby="quality-categories">
      <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
        <Heading as="h2" size="sm" id="quality-categories">{t('Categories')}</Heading>
        <span className="text-xs text-muted-foreground">{t('Score out of 100')}</span>
      </div>
      <div className="panel-surface overflow-hidden px-4 sm:px-6">
        {categories.map(category => <CategoryRow key={category.id} category={category} />)}
      </div>
    </section>}
    {data?.speed && !running && <ReferenceTransfer speed={data.speed} />}
    {data && !running && started && <MeasurementDetails data={data} />}
  </div>
}

function ScoreRing({ score, busy }: { score: number | null; busy: boolean }) {
  useLocale()
  const radius = 52
  const circumference = 2 * Math.PI * radius
  const clamped = score == null ? 0 : Math.max(0, Math.min(100, score))
  const grade = score == null ? null : gradeFor(score)
  const label = busy
    ? t('Testing network quality')
    : score == null
      ? t('No overall score')
      : t('Overall score {score} out of 100, {grade}', { score: points(score), grade: t(grade!.label) })

  return <div className="relative size-28 shrink-0 sm:size-32" role="img" aria-label={label}>
    <svg viewBox="0 0 120 120" className="size-full -rotate-90" aria-hidden="true">
      <circle cx="60" cy="60" r={radius} className="fill-none stroke-border" strokeWidth="9" />
      {score != null && <circle
        cx="60" cy="60" r={radius}
        className={cn('fill-none stroke-current transition-[stroke-dashoffset] duration-700', grade && toneText[grade.variant])}
        strokeWidth="9" strokeLinecap="round"
        strokeDasharray={circumference}
        strokeDashoffset={circumference * (1 - clamped / 100)}
      />}
    </svg>
    <div className="absolute inset-0 flex flex-col items-center justify-center" aria-hidden="true">
      {busy
        ? <LoaderCircle className="size-7 animate-spin text-primary" aria-hidden="true" />
        : score == null
          ? <span className="text-3xl leading-none font-semibold text-muted-foreground">—</span>
          : <span className={cn('text-4xl leading-none font-semibold tracking-tight tabular-nums', grade && toneText[grade.variant])}>{points(score)}</span>}
      <span className="mt-1.5 text-[10px] font-medium tracking-wider text-muted-foreground uppercase">
        {busy ? t('Testing…') : score == null ? t('No score') : '/ 100'}
      </span>
    </div>
  </div>
}

function CategoryRow({ category }: { category: QualityCategory }) {
  useLocale()
  const score = category.score
  const displayed = score == null ? null : Math.round(score)
  const grade = displayed == null ? null : gradeFor(displayed)
  const width = score == null ? 0 : Math.max(0, Math.min(100, score))

  return <Disclosure className="border-b last:border-0" summary={
    <span className="block w-full py-3">
      <span className="flex items-baseline justify-between gap-3">
        <span className="min-w-0 font-medium text-foreground">{t(categoryLabel(category.label))}</span>
        <span className="shrink-0 whitespace-nowrap">
          {displayed == null
            ? <span className="text-base text-muted-foreground">—</span>
            : <>
                <span className="text-xl leading-7 font-semibold text-foreground tabular-nums">{displayed}</span>
                <span className="text-xs text-muted-foreground"> / 100</span>
              </>}
        </span>
      </span>
      <span className="mt-2 flex items-center gap-3">
        <span className="h-1.5 min-w-0 flex-1 overflow-hidden rounded-full bg-muted" aria-hidden="true">
          {score != null && <span
            className={cn('block h-full rounded-full transition-[width] duration-500', grade && toneFill[grade.variant])}
            style={{ width: `${width}%` }}
          />}
        </span>
        <span className="shrink-0 text-xs text-muted-foreground">{t(categoryState[category.state])}</span>
      </span>
    </span>
  }>
    <CategoryEvidence category={category} />
  </Disclosure>
}

function CategoryEvidence({ category }: { category: QualityCategory }) {
  useLocale()
  return <div className="pb-4 pl-5">
    <p className="text-xs text-muted-foreground">
      {category.available_points > 0
        ? t('Contributes {earned} of {available} points to the overall score.', { earned: points(category.earned_points), available: points(category.available_points) })
        : t('Excluded from the overall score because this category was not measured.')}
    </p>
    <ul className="mt-1 divide-y divide-border/60">
      {category.components.map(component => <li key={component.id} className="flex items-start justify-between gap-4 py-2.5">
        <div className="min-w-0">
          <p className="text-sm text-foreground">{component.label}</p>
          <p className="text-xs text-muted-foreground">
            {component.reason}{component.score == null
              ? ` · ${t('Excluded from the category score')}`
              : ` · ${t('{weight}% of the category score', { weight: points(component.effective_weight * 100) })}`}
          </p>
        </div>
        <span className="shrink-0 text-sm font-medium text-foreground tabular-nums">
          {component.score == null ? <span className="text-muted-foreground">{t('Not measured')}</span> : `${Math.round(component.score)} / 100`}
        </span>
      </li>)}
    </ul>
  </div>
}

function ReferenceTransfer({ speed }: { speed: NonNullable<QualitySnapshot['speed']> }) {
  useLocale()
  return <section className="space-y-3" aria-labelledby="quality-transfer">
    <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
      <Heading as="h2" size="sm" id="quality-transfer">{t('Reference transfer')}</Heading>
      <span className="text-xs text-muted-foreground">{speed.server} · {speed.location}</span>
    </div>
    <div className="grid gap-3 sm:grid-cols-2">
      <div className="rounded-lg bg-muted/50 p-4">
        <p className="flex items-center gap-1.5 text-xs text-muted-foreground"><ArrowDown className="size-3.5" aria-hidden />{t('Download')}</p>
        <MetricValue size="lg" value={speed.download_mbps.toFixed(1)} unit="Mbps" className="mt-2" />
      </div>
      <div className="rounded-lg bg-muted/50 p-4">
        <p className="flex items-center gap-1.5 text-xs text-muted-foreground"><ArrowUp className="size-3.5" aria-hidden />{t('Upload')}</p>
        <MetricValue size="lg" value={speed.upload_mbps.toFixed(1)} unit="Mbps" className="mt-2" />
      </div>
    </div>
    <p className="text-xs text-muted-foreground">{t('One test server, not a service-wide benchmark.')}</p>
  </section>
}

function MeasurementDetails({ data }: { data: QualitySnapshot }) {
  useLocale()
  return <section className="space-y-3" aria-labelledby="quality-details">
    <Heading as="h2" size="sm" id="quality-details">{t('Measurement details')}</Heading>
    <div className="panel-surface divide-y px-4 sm:px-6">
      <Disclosure summary={t('How the score works')}>
        <div className="space-y-2 pb-4 text-sm text-muted-foreground">
          <p>{t('Categories with measured results share the full 100 points. Unmeasured tests are excluded; confirmed failures still count. Wireless is excluded until client or AP measurements exist.')}</p>
          <p>{t('Overall = sum of category scores × base weights ÷ sum of measured base weights. Base weights: Speed 25, Latency 25, Stability 25, Wireless 15, Functionality 10.')}</p>
          <p>{t('Grades: Excellent 90–100, Good 75–89, Fair 60–74, Poor below 60. Scores are references, not predictions for every client.')}</p>
          <p>{t('Tests run on demand. Results stay available until the next assessment or daemon restart.')}</p>
          <p className="text-xs">{data.scoring_version} · {data.profile_id} {data.profile_version}</p>
        </div>
      </Disclosure>
      {data.targets.length > 0 && <Disclosure summary={t('Service measurements')}>
        <div className="pb-3">
          <p className="text-sm text-muted-foreground">{t('Six fresh HTTPS connections per service and address family. This checks those resources, not every feature of the service.')}</p>
          <div className="mt-1 divide-y divide-border/60">
            {data.targets.map(target => <Disclosure key={`${target.id}-${target.family}`} summary={
              <span className="flex min-w-0 flex-1 flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
                <span className="font-medium text-foreground">{target.name} · {target.family.toUpperCase()}</span>
                <span className="text-xs tabular-nums">{target.samples.filter(sample => sample.state === 'ok').length}/{target.samples.length} {t('expected responses')}</span>
              </span>
            }>
              <div className="pb-3 pl-5">
                <p className="break-all font-mono text-xs text-muted-foreground">{target.url}</p>
                <ol className="mt-2 space-y-2">
                  {target.samples.map((sample, index) => <li key={index} className="rounded-lg bg-muted/40 px-3 py-2">
                    <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 text-xs">
                      <span className="font-medium text-foreground">{index + 1}. {sample.state} · {sample.stage}{sample.status ? ` · HTTP ${sample.status}` : ''}</span>
                      <span className="text-muted-foreground tabular-nums">DNS {duration(sample, 'dns_ms')} · TCP {duration(sample, 'tcp_ms')} · HTTPS {duration(sample, 'response_ms')}</span>
                    </div>
                    {sample.address && <TechnicalValue className="mt-1 block">{sample.address}</TechnicalValue>}
                    {sample.error && <p className="mt-1 break-words text-xs text-muted-foreground">{sample.error}</p>}
                  </li>)}
                </ol>
              </div>
            </Disclosure>)}
          </div>
        </div>
      </Disclosure>}
      {(data.nat || data.nat_error) && <Disclosure summary={t('NAT observations')}>
        {data.nat && <>
          <dl><PropertyRow label={t('STUN server')}><TechnicalValue>{data.nat.server}</TechnicalValue></PropertyRow><PropertyRow label={t('Observed mapping')}><TechnicalValue>{data.nat.mapped}</TechnicalValue></PropertyRow><PropertyRow label={t('Mapping')}>{data.nat.mapping}</PropertyRow><PropertyRow label={t('Filtering')}>{data.nat.filtering}</PropertyRow></dl>
          <p className="pb-3 text-sm text-muted-foreground">{t('An observed mapping does not prove public IPv4 assignment, inbound access or P2P success. These observations do not add points.')}</p>
        </>}
        {data.nat_error && <p className="break-words pb-3 text-sm text-muted-foreground">{data.nat_error}</p>}
      </Disclosure>}
    </div>
  </section>
}

function duration(sample: QualitySample, key: 'dns_ms' | 'tcp_ms' | 'response_ms') {
  const value = sample[key]
  return value == null ? '—' : value < 1 ? '<1 ms' : `${value.toFixed(1)} ms`
}
