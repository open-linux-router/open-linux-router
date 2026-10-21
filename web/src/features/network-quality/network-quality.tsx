import { t, useLocale } from '@/lib/i18n'
import { useRef, useState } from 'react'
import { ChevronRight, LoaderCircle, RotateCw, X } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Disclosure } from '@/components/ui/disclosure'
import { Heading } from '@/components/ui/heading'
import { MetricValue } from '@/components/ui/metric-value'
import { PropertyRow, TechnicalValue } from '@/components/ui/property-row'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'
import { categoryEvidence, serviceEvidence, type CategoryEvidence, type ServiceEvidence } from './measurements'
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
const grades: { min: number; label: string; range: string; variant: Tone }[] = [
  { min: 90, label: 'Excellent', range: '90–100', variant: 'success' },
  { min: 75, label: 'Good', range: '75–89', variant: 'success' },
  { min: 60, label: 'Fair', range: '60–74', variant: 'warning' },
  { min: 0, label: 'Poor', range: 'below 60', variant: 'destructive' },
]

function gradeFor(score: number) {
  return grades.find(grade => score >= grade.min) ?? grades[grades.length - 1]
}

// The ring and the bars carry the grade; the score itself stays foreground so a
// page of amber scores does not read as a page of warnings.
const toneStroke: Record<Tone, string> = {
  success: 'text-success',
  warning: 'text-warning',
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
          {/* The report keeps the shell's content width: a fullscreen view that
              narrows to article width leaves the page looking like a stray
              column rather than this router's own reading surface. */}
          <div className="mx-auto w-full max-w-[80rem] space-y-6 px-4 py-6 sm:px-6 sm:py-8 xl:px-8">
            <QualityReport data={data} readError={query.isError} loading={query.isPending} retry={() => query.refetch()} />
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

function QualityReport({ data, readError, loading, retry }: {
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
  const services = serviceEvidence(data)
  const copy = heroCopy(data, loading)
  const status = scopedRunning && data ? runningLabel(data) : copy.detail
  const updated = data?.state === 'completed' ? data.completed_at : null
  const measured = categories.filter(category => category.score != null).length
  const canRerun = data?.state === 'completed'
  const hasRun = Boolean(data?.started_at) && data?.state !== 'idle'

  return <>
    <DialogHeader className="sm:flex-row sm:items-start sm:justify-between sm:gap-6">
      <div className="min-w-0 space-y-2">
        <DialogTitle className="text-2xl leading-8 tracking-[-0.015em]">{t('Network quality')}</DialogTitle>
        <DialogDescription className="max-w-prose text-sm leading-relaxed">
          {t("From this router's default route.")}
          {updated && <> · {t('Updated {time}', { time: formatWhen(updated, locale) })}</>}
        </DialogDescription>
        <p className="sr-only" role="status" aria-live="polite">{[copy.title, status].filter(Boolean).join('. ')}</p>
      </div>
      <div className="flex shrink-0 flex-col items-start gap-1.5 sm:items-end">
        {running
          ? <Button variant="outline" size="lg" disabled={action.isPending} onClick={() => action.mutate({ cancel: true })}>{t('Cancel test')}</Button>
          : <Button size="lg" disabled={action.isPending || loading} onClick={() => action.mutate({ scope: 'all' })}>
              {action.isPending ? t('Starting…') : hasRun ? t('Run all tests') : t('Run assessment')}
            </Button>}
        <span className="text-xs text-muted-foreground">{t('Bandwidth test included')}</span>
      </div>
    </DialogHeader>

    {readError && <Alert variant="destructive">
      <AlertTitle>{t('Could not read the assessment.')}</AlertTitle>
      <AlertDescription className="space-y-3">
        <p>{t('Check the router connection, then try again.')}</p>
        <Button variant="outline" size="sm" onClick={retry}>{t('Retry')}</Button>
      </AlertDescription>
    </Alert>}

    {action.isError && <Alert variant="destructive">
      <AlertTitle>{t('Could not update the assessment')}</AlertTitle>
      <AlertDescription className="break-words">{action.error.message}</AlertDescription>
    </Alert>}

    {data?.speed_error && <Alert variant="destructive">
      <AlertTitle>{t('Bandwidth test failed')}</AlertTitle>
      <AlertDescription className="break-words">{data.speed_error}</AlertDescription>
    </Alert>}

    <div className="grid items-start gap-4 lg:grid-cols-12 lg:gap-6">
      <div className="space-y-4 lg:col-span-4">
        <ScorePanel
          score={score}
          grade={grade}
          measured={measured}
          total={categories.length}
          coverage={data?.overall.coverage_percent ?? 0}
          copy={copy}
          busy={fullRunning || Boolean(loading && !data)}
        />
        {hasRun && <TransferPanel data={data} busy={running && data?.phase === 'speed'} />}
      </div>
      <div className="space-y-4 lg:col-span-8">
        {data || readError
          ? <CategoryList
              categories={categories}
              data={data}
              services={services}
              hasRun={hasRun}
              runningScope={runningScope}
              runningTarget={data?.running_target}
              anyRunning={running || action.isPending}
              canRerun={canRerun}
              onRerun={next => action.mutate(next)}
            />
          : <Skeleton className="h-72 w-full rounded-xl" />}
        {data && hasRun && <MeasurementDetails data={data} services={services} />}
      </div>
    </div>
  </>
}

function ScorePanel({ score, grade, measured, total, coverage, copy, busy }: {
  score: number | null
  grade: { label: string; range: string; variant: Tone } | null
  measured: number
  total: number
  coverage: number
  copy: { title: string; detail: string }
  busy: boolean
}) {
  useLocale()
  return <section aria-labelledby="quality-result" className="panel-surface p-4 sm:p-5">
    <div className="flex items-center justify-between gap-3">
      <Heading as="h2" size="sm" id="quality-result" className="text-muted-foreground">{t('Overall score')}</Heading>
      {grade && <Badge variant={grade.variant}>{t(grade.label)}</Badge>}
    </div>
    <div className="mt-4 flex items-center gap-4">
      <ScoreRing score={score} busy={busy} />
      <div className="min-w-0 flex-1 space-y-1">
        {score != null
          ? <>
              <p className="text-sm leading-5 text-foreground">{t('{count} of {total} categories scored', { count: measured, total })}</p>
              <p className="text-xs leading-5 text-muted-foreground">{t('{grade} covers {range} of 100', { grade: t(grade!.label), range: grade!.range })}</p>
            </>
          : <>
              <p className="text-sm leading-5 font-medium text-foreground">{copy.title}</p>
              {copy.detail && <p className="text-xs leading-5 text-muted-foreground">{copy.detail}</p>}
            </>}
      </div>
    </div>
    {score != null && coverage > 0 && <div className="mt-4">
      <div className="flex items-baseline justify-between gap-2 text-xs text-muted-foreground">
        <span>{t('Measured coverage')}</span>
        <span className="font-medium text-foreground tabular-nums">{Math.round(coverage)}%</span>
      </div>
      <div role="progressbar" aria-label={t('Measured coverage')} aria-valuenow={Math.round(coverage)} aria-valuemin={0} aria-valuemax={100}
        className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-muted">
        <div className="h-full rounded-full bg-muted-foreground/45 transition-[width] duration-500" style={{ width: `${Math.min(100, Math.max(0, coverage))}%` }} />
      </div>
    </div>}
  </section>
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

  return <div className="relative size-28 shrink-0" role="img" aria-label={label}>
    <svg viewBox="0 0 120 120" className="size-full -rotate-90" aria-hidden="true">
      <circle cx="60" cy="60" r={radius} className="fill-none stroke-border" strokeWidth="8" />
      {score != null && <circle
        cx="60" cy="60" r={radius}
        className={cn('fill-none stroke-current transition-[stroke-dashoffset] duration-700', grade && toneStroke[grade.variant])}
        strokeWidth="8" strokeLinecap="round"
        strokeDasharray={circumference}
        strokeDashoffset={circumference * (1 - clamped / 100)}
      />}
    </svg>
    <div className="absolute inset-0 flex flex-col items-center justify-center" aria-hidden="true">
      {busy
        ? <LoaderCircle className="size-7 animate-spin text-primary" aria-hidden="true" />
        : score == null
          ? <span className="text-3xl leading-none font-semibold text-muted-foreground">—</span>
          : <span className="text-4xl leading-none font-semibold tracking-tight text-foreground tabular-nums">{points(score)}</span>}
      <span className="mt-1.5 text-[10px] font-medium tracking-wider text-muted-foreground uppercase">
        {busy ? t('Testing…') : score == null ? t('No score') : '/ 100'}
      </span>
    </div>
  </div>
}

function TransferRow({ arrow, label, value }: { arrow: '↓' | '↑'; label: string; value: number | null }) {
  return <div className="flex items-center justify-between gap-3 py-0.5">
    <span className="flex min-w-0 items-center gap-1.5 text-sm text-muted-foreground">
      <span aria-hidden="true" className="text-base">{arrow}</span>{label}
    </span>
    <MetricValue size="md" value={value == null ? null : value.toFixed(1)} unit="Mbps" />
  </div>
}

function TransferPanel({ data, busy }: { data?: QualitySnapshot; busy: boolean }) {
  useLocale()
  const speed = data?.speed ?? null
  const where = speed ? [speed.location, speed.server].filter(Boolean).join(' · ') : ''
  return <section aria-labelledby="quality-transfer" className="panel-surface p-4 sm:p-5">
    <div className="flex items-baseline justify-between gap-3">
      <Heading as="h2" size="sm" id="quality-transfer" className="text-muted-foreground">{t('Transfer')}</Heading>
      {where && <span className="min-w-0 truncate text-xs text-muted-foreground">{where}</span>}
    </div>
    <div className="mt-2">
      <TransferRow arrow="↓" label={t('Download')} value={speed?.download_mbps ?? null} />
      <TransferRow arrow="↑" label={t('Upload')} value={speed?.upload_mbps ?? null} />
    </div>
    <p className="mt-2 text-xs leading-5 text-muted-foreground">
      {busy ? t('Measuring download and upload') : speed ? t('One test server, not a service-wide benchmark.') : t('Not measured')}
    </p>
  </section>
}

function categorySummary(evidence: CategoryEvidence, category: QualityCategory, hasRun: boolean): string | null {
  if (!hasRun) return t('Weight {weight} of 100', { weight: Math.round(category.budget) })
  if (category.state === 'unavailable') return t('Not measured')
  switch (evidence.kind) {
    case 'speed':
      // Throughput leads in the transfer panel beside the score. Repeating it
      // here would spend a row on a number the same screen already shows, so
      // this line carries the limit of what the category score covers.
      return evidence.speed ? t('Reference transfer only') : t('Not measured')
    case 'latency': {
      const parts: string[] = []
      if (evidence.latency.response != null) parts.push(t('{value} ms median', { value: evidence.latency.response.toFixed(1) }))
      if (evidence.latency.variation != null) parts.push(t('{value} ms variation', { value: evidence.latency.variation.toFixed(1) }))
      return parts.length > 0 ? parts.join(' · ') : t('Not measured')
    }
    case 'stability':
      return evidence.stability.attempted === 0
        ? t('Not measured')
        : t('{count} of {total} expected responses', { count: evidence.stability.responded, total: evidence.stability.attempted })
    case 'functionality':
      return evidence.functionality.measured === 0
        ? t('Not measured')
        : t('{count} of {total} services reachable', { count: evidence.functionality.reachable, total: evidence.functionality.measured })
    default:
      return null
  }
}

function CategoryList({ categories, data, services, hasRun, runningScope, runningTarget, anyRunning, canRerun, onRerun }: {
  categories: QualityCategory[]
  data?: QualitySnapshot
  services: ServiceEvidence[]
  hasRun: boolean
  runningScope: QualityScope | ''
  runningTarget?: string
  anyRunning: boolean
  canRerun: boolean
  onRerun: (action: QualityAction) => void
}) {
  useLocale()
  if (categories.length === 0) return <div className="panel-surface px-5 py-8 text-center text-sm text-muted-foreground">{t('No measurements yet.')}</div>
  return <section aria-label={t('Categories')} className="panel-surface divide-y divide-border">
    {categories.map(category => <CategoryRow
      key={category.id}
      category={category}
      data={data}
      services={services}
      hasRun={hasRun}
      runningScope={runningScope}
      runningTarget={runningTarget}
      anyRunning={anyRunning}
      canRerun={canRerun}
      onRerun={onRerun}
    />)}
  </section>
}

function CategoryRow({ category, data, services, hasRun, runningScope, runningTarget, anyRunning, canRerun, onRerun }: {
  category: QualityCategory
  data?: QualitySnapshot
  services: ServiceEvidence[]
  hasRun: boolean
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
  const tests = data ? categoryTests(category, services, data) : []
  const busy = (scope === 'responses' && runningScope === 'responses') || (scope === 'speed' && runningScope === 'speed')
  const label = t(categoryLabel(category.label))
  const listID = `quality-${category.id}-tests`
  const summary = categorySummary(categoryEvidence(category, services, data), category, hasRun)
  // "Partial" would repeat on every row: the score is complete only when its
  // categories name the checks that did run. Only a state a reader can act on
  // earns a word here, and an unmeasured category says so in its own line.
  const stateWord = hasRun && category.state === 'stale' ? t('Outdated') : null

  return <div className="px-4 py-2.5 sm:px-5">
    <div className="flex items-center gap-2">
      <Heading as="h3" size="md" className="min-w-0 flex-1">
        <button
          type="button"
          aria-expanded={open}
          aria-controls={open ? listID : undefined}
          onClick={() => setOpen(value => !value)}
          className="flex min-h-11 w-full items-center gap-2 rounded-md text-left hover:text-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring sm:min-h-9"
        >
          <ChevronRight className={cn('size-4 shrink-0 text-muted-foreground transition-transform', open && 'rotate-90')} aria-hidden="true" />
          <span className="min-w-0 truncate">{label}</span>
          {stateWord && <span className="shrink-0 text-xs font-normal text-muted-foreground">{stateWord}</span>}
        </button>
      </Heading>
      <span className="w-24 shrink-0 text-right whitespace-nowrap tabular-nums">
        {displayed == null
          ? <span className="text-lg leading-6 font-normal text-muted-foreground">—</span>
          : <><span className="text-lg leading-6 font-semibold text-foreground">{displayed}</span><span className="text-xs font-normal text-muted-foreground"> / 100</span></>}
      </span>
      {/* The action slot is reserved even when a category has no rerun, so the
          scores stay in one column instead of stepping sideways. */}
      <div className="flex w-8 shrink-0 justify-end">
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
    </div>
    {(score != null || summary) && <div className="mt-0.5 flex flex-col gap-1.5 pl-6 sm:flex-row sm:flex-wrap sm:items-center sm:gap-x-3">
      {summary && <span className="min-w-0 flex-1 text-xs text-muted-foreground tabular-nums">{summary}</span>}
      {score != null && <div
        role="progressbar"
        aria-label={label}
        aria-valuenow={displayed ?? undefined}
        aria-valuemin={0}
        aria-valuemax={100}
        // One track width for every category: bars that stretch to their own
        // text cannot be read against each other.
        className="h-1.5 w-full shrink-0 overflow-hidden rounded-full bg-muted sm:w-40"
      >
        <div className={cn('h-full rounded-full transition-[width] duration-500', grade && toneFill[grade.variant])} style={{ width: `${width}%` }} />
      </div>}
    </div>}
    {open && <ul id={listID} className="mt-2 divide-y divide-border/60 border-t border-border/60 pl-6">
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
  /** What the group measured, in its own units, so the row can be checked. */
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

const timingDetail = (label: string, value: number | null | undefined) => value == null ? undefined : `${label} ${value.toFixed(1)} ms`

function categoryTests(category: QualityCategory, services: ServiceEvidence[], data?: QualitySnapshot): TestGroup[] {
  const groups: TestGroup[] = []
  const targets = new Map<string, TestGroup>()
  const details = new Map<string, string[]>()
  const byId = new Map(services.map(service => [service.id, service]))
  const addDetail = (key: string, part: string | undefined) => {
    if (!part) return
    details.set(key, [...details.get(key) ?? [], part])
  }

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
      const evidence = byId.get(component.target)
      switch (component.kind) {
        case 'tcp': addDetail(component.target, timingDetail(t('TCP'), evidence?.tcp)); break
        case 'dns': addDetail(component.target, timingDetail(t('DNS'), evidence?.dns)); break
        case 'response': addDetail(component.target, timingDetail(t('HTTPS'), evidence?.response)); break
        case 'continuity':
          if (evidence) addDetail(component.target, t('{count} of {total} connected', { count: evidence.responded, total: evidence.samples }))
          break
        case 'variation':
          addDetail(component.target, evidence?.variation == null ? undefined : `${evidence.variation.toFixed(1)} ms ${t('variation')}`)
          break
        case 'access':
          if (evidence) addDetail(component.target, t('{count} of {total} expected responses', { count: evidence.responded, total: evidence.samples }))
          break
      }
      continue
    }
    if (component.id === 'reference') {
      const speed = data?.speed
      groups.push({
        id: 'reference',
        label: t('Cloudflare download and upload'),
        scope: 'speed',
        score: component.score,
        metrics: [],
        detail: speed ? `↓ ${speed.download_mbps.toFixed(1)} Mbps · ↑ ${speed.upload_mbps.toFixed(1)} Mbps` : undefined,
      })
      continue
    }
    groups.push({ id: component.id, label: t(component.label), scope: null, score: component.score, metrics: [] })
  }

  for (const group of groups) {
    if (group.target) {
      group.score = weightedScore(group.metrics)
      const parts = details.get(group.target)
      if (parts && parts.length > 0) group.detail = parts.join(' · ')
    }
  }
  return groups
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

  return <li className="flex items-start justify-between gap-3 py-2.5">
    <div className="min-w-0">
      <p className="text-sm text-foreground">{test.label}</p>
      {test.detail && <p className="mt-0.5 text-xs leading-5 text-muted-foreground tabular-nums">{test.detail}</p>}
    </div>
    <div className="flex shrink-0 items-start gap-2">
      {/* Same trailing columns as the category above, so a service score lands
          under the category score it explains. */}
      <span className="w-24 text-right text-sm font-semibold tabular-nums">
        {test.score != null
          ? Math.round(test.score)
          : <span className="text-xs font-normal text-muted-foreground">{test.scope ? '—' : t('Not measured')}</span>}
      </span>
      <div className="flex w-8 justify-end">
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
    </div>
  </li>
}

function MeasurementDetails({ data, services }: { data: QualitySnapshot; services: ServiceEvidence[] }) {
  useLocale()
  const medians = new Map(services.map(service => [service.id, service]))
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
          {data.targets.map(target => {
            const service = medians.get(target.id)
            return <Disclosure key={`${target.id}-${target.family}`} summary={
              <span className="flex min-w-0 flex-1 flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
                <span className="font-medium text-foreground">{target.name} · {target.family.toUpperCase()}</span>
                <span className="text-xs tabular-nums">
                  {service?.family === target.family && service?.response != null && `${service.response.toFixed(1)} ms · `}
                  {target.samples.filter(sample => sample.state === 'ok').length}/{target.samples.length} {t('expected responses')}
                </span>
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
            </Disclosure>
          })}
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
