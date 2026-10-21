import { t, useLocale } from '@/lib/i18n'
import { useRef, useState } from 'react'
import { ChevronRight, LoaderCircle, RotateCw, X } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Disclosure } from '@/components/ui/disclosure'
import { Heading } from '@/components/ui/heading'
import { PropertyRow, TechnicalValue } from '@/components/ui/property-row'
import { cn } from '@/lib/utils'
import { useNetworkQuality, useQualityAction } from './queries'
import type { QualityAction } from './queries'
import type { QualityCategory, QualitySample, QualitySnapshot, QualityScope } from './types'

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

const phaseText: Record<string, string> = {
  responses: 'Checking service responses',
  speed: 'Measuring download and upload',
  nat: 'Checking NAT behavior',
  '': 'Starting…',
}

function categoryScope(id: string): 'responses' | 'speed' | null {
  switch (id) {
    case 'speed': return 'speed'
    case 'latency':
    case 'stability':
    case 'functionality': return 'responses'
    default: return null
  }
}

function categoryState(category: QualityCategory): string | null {
  switch (category.state) {
    case 'partial': return 'Partial'
    case 'unavailable': return 'Not measured'
    case 'stale': return 'Outdated'
    default: return null
  }
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
          <div className="mx-auto w-full max-w-3xl space-y-8 px-4 py-6 sm:px-6 sm:py-8">
            <DialogHeader className="pr-14">
              <DialogTitle>{t('Network quality')}</DialogTitle>
              <DialogDescription>{t("From this router's default route.")}</DialogDescription>
            </DialogHeader>
            <QualityDetails data={data} readError={query.isError} loading={query.isPending} retry={() => query.refetch()} />
          </div>
        </div>
      </DialogContent>
    </Dialog>
  </>
}

function formatWhen(value: string, locale: string) {
  return new Date(value).toLocaleString(locale, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })
}

function targetName(data: QualitySnapshot | undefined, id: string | undefined) {
  if (!id) return t('service')
  return data?.targets.find(target => target.id === id)?.name ?? id
}

function runningLabel(data: QualitySnapshot | undefined) {
  switch (data?.running_scope) {
    case 'responses': return t('Updating service tests')
    case 'speed': return t('Updating the transfer test')
    case 'nat': return t('Updating NAT observations')
    case 'target': return t('Updating {target} tests', { target: targetName(data, data.running_target) })
    default: return t('Testing network quality')
  }
}

function heroCopy(data: QualitySnapshot | undefined, loading: boolean | undefined) {
  if (!data && loading) return { title: t('Loading…'), detail: t('Reading the latest assessment.') }
  if (!data || data.state === 'idle') return { title: t('Not tested'), detail: t('Run all tests to score speed, latency, stability and functionality.') }
  if (data.state === 'running') return { title: t('Testing network quality'), detail: t(phaseText[data.phase] ?? 'Starting…') }
  if (data.state === 'cancelled') return { title: t('Assessment cancelled'), detail: t('The assessment did not finish, so no overall score is published. Partial measurements are kept.') }
  if (data.state === 'timed_out') return { title: t('Assessment timed out'), detail: t('The assessment did not finish, so no overall score is published. Partial measurements are kept.') }
  if (data.overall.state === 'stale') return { title: t('Results expired'), detail: t('Run all tests to score the current connection.') }
  if (data.overall.state === 'ready' && data.overall.score != null) return { title: t('Overall score'), detail: '' }
  if (data.overall.available_points > 0) return { title: t('Partial result'), detail: t('No overall score is published for this assessment.') }
  return { title: t('No scored results'), detail: t('No usable measurements. Run all tests to try again.') }
}

function QualityDetails({ data, readError, loading, retry }: {
  data?: QualitySnapshot
  readError: boolean
  loading?: boolean
  retry: () => void
}) {
  const locale = useLocale()
  const action = useQualityAction()
  const fullRunning = data?.state === 'running'
  const runningScope: QualityScope | '' = data?.running_scope ?? (fullRunning ? 'all' : '')
  const scopedRunning = !fullRunning && Boolean(data?.running_scope)
  const running = fullRunning || scopedRunning
  const score = data && data.state === 'completed' && data.overall.state === 'ready' ? data.overall.score : null
  const grade = score == null ? null : gradeFor(score)
  const categories = data?.categories ?? []
  const copy = heroCopy(data, loading)
  const status = scopedRunning && data ? runningLabel(data) : copy.detail
  const updated = data?.state === 'completed' ? data.completed_at : null
  const showCategories = Boolean(data && !fullRunning && data.started_at && categories.length > 0)
  const canRerun = data?.state === 'completed'

  if (readError) {
    return <Alert variant="destructive">
      <AlertTitle>{t('Could not read the assessment.')}</AlertTitle>
      <AlertDescription className="space-y-3">
        <p>{t('Check the router connection, then try again.')}</p>
        <Button variant="outline" size="sm" onClick={retry}>{t('Retry')}</Button>
      </AlertDescription>
    </Alert>
  }

  return <div className="space-y-8">
    <section className="panel-surface p-4 sm:p-6" aria-labelledby="quality-result">
      <p className="sr-only" role="status" aria-live="polite">{[copy.title, status].filter(Boolean).join('. ')}</p>
      <div className="flex flex-col gap-5 sm:flex-row sm:items-center">
        <div className="flex items-center gap-4 sm:min-w-0 sm:flex-1">
          <ScoreRing score={score} busy={fullRunning || Boolean(loading && !data)} />
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <Heading as="h2" size="md" id="quality-result">{copy.title}</Heading>
              {grade && <Badge variant={grade.variant}>{t(grade.label)}</Badge>}
            </div>
            {status && <p className="mt-1 text-sm text-muted-foreground">{status}</p>}
          </div>
        </div>
        <div className="flex shrink-0 flex-col gap-2 sm:items-end">
          {running
            ? <Button variant="outline" className="w-full sm:w-auto" disabled={action.isPending} onClick={() => action.mutate({ cancel: true })}>{t('Cancel test')}</Button>
            : <Button className="w-full sm:w-auto" disabled={action.isPending || loading} onClick={() => action.mutate({ scope: 'all' })}>
                {action.isPending ? t('Starting…') : data?.started_at ? t('Run all tests') : t('Run assessment')}
              </Button>}
          <span className="text-xs text-muted-foreground sm:text-right">
            {updated && <>{t('Updated {time}', { time: formatWhen(updated, locale) })} · </>}
            {t('Bandwidth test included')}
          </span>
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
    {showCategories && data && <section className="space-y-8" aria-label={t('Categories')}>
      <h2 className="sr-only">{t('Categories')}</h2>
      {categories.map(category => <CategorySection
        key={category.id}
        category={category}
        data={data}
        runningScope={runningScope}
        runningTarget={data.running_target}
        anyRunning={running || action.isPending}
        canRerun={canRerun}
        onRerun={next => action.mutate(next)}
      />)}
    </section>}
    {data && !fullRunning && data.started_at && <MeasurementDetails data={data} />}
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

interface TestMetric {
  id: string
  label: string
  score: number | null
  weight: number
}

interface TestGroup {
  id: string
  label: string
  target?: string
  scope: 'target' | 'speed' | null
  score: number | null
  metrics: TestMetric[]
  detail?: string
}

function weightedScore(metrics: TestMetric[]) {
  let weighted = 0
  let weight = 0
  for (const metric of metrics) {
    if (metric.score == null) continue
    weighted += metric.score * metric.weight
    weight += metric.weight
  }
  return weight > 0 ? weighted / weight : null
}

function categoryTests(category: QualityCategory, data: QualitySnapshot): TestGroup[] {
  const groups: TestGroup[] = []
  const targets = new Map<string, TestGroup>()
  for (const component of category.components) {
    if (component.target) {
      let group = targets.get(component.target)
      if (!group) {
        const separator = component.label.indexOf(' · ')
        group = {
          id: component.target,
          label: separator < 0 ? component.target : component.label.slice(0, separator),
          target: component.target,
          scope: 'target',
          score: null,
          metrics: [],
        }
        targets.set(component.target, group)
        groups.push(group)
      }
      const separator = component.label.indexOf(' · ')
      group.metrics.push({
        id: component.id,
        label: t(separator < 0 ? component.label : component.label.slice(separator + 3)),
        score: component.score,
        weight: component.weight,
      })
      continue
    }
    if (component.id === 'reference') {
      groups.push({
        id: 'reference',
        label: t('Cloudflare download and upload'),
        scope: 'speed',
        score: component.score,
        metrics: [],
        detail: data.speed
          ? `${t('Download')} ${data.speed.download_mbps.toFixed(1)} Mbps · ${t('Upload')} ${data.speed.upload_mbps.toFixed(1)} Mbps`
          : undefined,
      })
      continue
    }
    groups.push({ id: component.id, label: t(component.label), scope: null, score: component.score, metrics: [] })
  }
  for (const group of groups) {
    if (group.target) group.score = weightedScore(group.metrics)
  }
  return groups
}

function CategorySection({ category, data, runningScope, runningTarget, anyRunning, canRerun, onRerun }: {
  category: QualityCategory
  data: QualitySnapshot
  runningScope: QualityScope | ''
  runningTarget?: string
  anyRunning: boolean
  canRerun: boolean
  onRerun: (action: QualityAction) => void
}) {
  useLocale()
  const [open, setOpen] = useState(false)
  const score = category.score
  const displayed = score == null ? null : Math.round(score)
  const grade = displayed == null ? null : gradeFor(displayed)
  const width = score == null ? 0 : Math.max(0, Math.min(100, score))
  const scope = categoryScope(category.id)
  const state = categoryState(category)
  const tests = categoryTests(category, data)
  const busy = (scope === 'responses' && runningScope === 'responses') || (scope === 'speed' && runningScope === 'speed')
  const label = t(categoryLabel(category.label))
  const listID = `quality-${category.id}-tests`

  return <section className="space-y-2" aria-label={label}>
    <div className="flex items-start gap-2">
      <Heading as="h3" size="lg" className="min-w-0 flex-1">
        <button
          type="button"
          aria-expanded={open}
          aria-controls={open ? listID : undefined}
          onClick={() => setOpen(value => !value)}
          className="flex min-h-11 w-full items-start gap-2 rounded-md text-left hover:text-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          <ChevronRight className={cn('mt-1.5 size-5 shrink-0 text-muted-foreground transition-transform', open && 'rotate-90')} aria-hidden="true" />
          <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
            <span>{label}</span>
            {state && <span className="text-xs font-normal text-muted-foreground">{t(state)}</span>}
          </span>
        </button>
      </Heading>
      <span className="shrink-0 pt-0.5 text-xl leading-8 font-semibold tabular-nums sm:text-2xl">
        {displayed == null
          ? <span className="text-muted-foreground">—</span>
          : <>{displayed}<span className="text-sm font-normal text-muted-foreground"> / 100</span></>}
      </span>
      {scope && canRerun && <Button
        variant="ghost"
        size="icon-sm"
        className={cn('shrink-0', busy && 'disabled:opacity-100')}
        disabled={anyRunning}
        onClick={() => onRerun(scope === 'speed' ? { scope: 'speed' } : { scope: 'responses' })}
        aria-label={t('Rerun {category} tests', { category: label })}
        title={t('Rerun {category} tests', { category: label })}
      >
        {busy ? <LoaderCircle className="animate-spin" aria-hidden="true" /> : <RotateCw aria-hidden="true" />}
      </Button>}
    </div>
    <div className="h-1.5 overflow-hidden rounded-full bg-muted" aria-hidden="true">
      {score != null && <div className={cn('h-full rounded-full transition-[width] duration-500', grade && toneFill[grade.variant])} style={{ width: `${width}%` }} />}
    </div>
    {open && <ul id={listID} className="mt-3 divide-y divide-border/60 border-y border-border/60">
      {tests.map(test => <QualityTestRow
        key={test.id}
        test={test}
        runningScope={runningScope}
        runningTarget={runningTarget}
        anyRunning={anyRunning}
        canRerun={canRerun}
        onRerun={onRerun}
      />)}
    </ul>}
  </section>
}

function QualityTestRow({ test, runningScope, runningTarget, anyRunning, canRerun, onRerun }: {
  test: TestGroup
  runningScope: QualityScope | ''
  runningTarget?: string
  anyRunning: boolean
  canRerun: boolean
  onRerun: (action: QualityAction) => void
}) {
  useLocale()
  const busy = test.scope === 'target' ? runningScope === 'target' && runningTarget === test.target : test.scope === 'speed' && runningScope === 'speed'
  const rerun: QualityAction | null = test.scope === 'target' && test.target
    ? { scope: 'target', target: test.target }
    : test.scope === 'speed'
      ? { scope: 'speed' }
      : null
  const showMetrics = test.metrics.length > 1 && test.metrics.some(metric => metric.score != null)

  return <li className="flex items-start justify-between gap-3 py-3">
    <div className="min-w-0">
      <p className="text-sm font-medium text-foreground">{test.label}</p>
      {test.detail && <p className="mt-1 text-xs text-muted-foreground">{test.detail}</p>}
      {showMetrics && <p className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
        {test.metrics.map(metric => <span key={metric.id}>{metric.label} <span className="font-medium text-foreground tabular-nums">{metric.score == null ? '—' : Math.round(metric.score)}</span></span>)}
      </p>}
    </div>
    <div className="flex shrink-0 items-center gap-1">
      <span className="text-sm font-semibold tabular-nums">
        {test.score != null
          ? Math.round(test.score)
          : <span className="font-normal text-muted-foreground">{test.scope ? '—' : t('Not measured')}</span>}
      </span>
      {rerun && canRerun && <Button
        variant="ghost"
        size="icon-sm"
        className={busy ? 'disabled:opacity-100' : undefined}
        disabled={anyRunning}
        onClick={() => onRerun(rerun)}
        aria-label={t('Rerun {name} tests', { name: test.label })}
        title={t('Rerun {name} tests', { name: test.label })}
      >
        {busy ? <LoaderCircle className="animate-spin" aria-hidden="true" /> : <RotateCw aria-hidden="true" />}
      </Button>}
    </div>
  </li>
}

function MeasurementDetails({ data }: { data: QualitySnapshot }) {
  useLocale()
  return <section aria-label={t('Measurement details')} className="panel-surface divide-y px-4 sm:px-6">
    <Disclosure summary={t('How the score works')}>
      <div className="space-y-2 pb-4 text-sm text-muted-foreground">
        <p>{t('Categories with measured results share the full 100 points. Unmeasured tests are excluded; confirmed failures still count. Wireless is excluded until client or AP measurements exist.')}</p>
        <p>{t('Overall = sum of category scores × base weights ÷ sum of measured base weights. Base weights: Speed 25, Latency 25, Stability 25, Wireless 15, Functionality 10.')}</p>
        <p>{t('Grades: Excellent 90–100, Good 75–89, Fair 60–74, Poor below 60. Scores are references, not predictions for every client.')}</p>
        <p>{t('The page always runs the reference transfer. It measures one Cloudflare server, not every service, and uses substantial data.')}</p>
        <p>{t('Tests run on demand. Results stay available until the next assessment or daemon restart.')}</p>
        <p className="text-xs">{data.scoring_version} · {data.profile_id} {data.profile_version}</p>
      </div>
    </Disclosure>
    {data.targets.length > 0 && <Disclosure summary={t('Service measurements')}>
      <div className="pb-3">
        <p className="text-sm text-muted-foreground">{t('Six fresh HTTPS connections per service and address family. These samples feed Latency, Stability and Functionality; they check those resources, not every feature of the service.')}</p>
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
  </section>
}

function duration(sample: QualitySample, key: 'dns_ms' | 'tcp_ms' | 'response_ms') {
  const value = sample[key]
  return value == null ? '—' : value < 1 ? '<1 ms' : `${value.toFixed(1)} ms`
}
