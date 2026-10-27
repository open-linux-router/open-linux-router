import { t, useLocale } from '@/lib/i18n'
import { Activity, ArrowDown, ArrowUp, Check, Copy, LoaderCircle, Radio, Route } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { PageHeading } from '@/components/layout/page-heading'
import { MetricValue } from '@/components/ui/metric-value'
import { TOOL_DEFINITIONS, type ToolSlug } from '@/features/tools/catalog'
import { createLinkTestEngine, linkTestResult, loadLinkTestEngine, loadLinkTestSettings, startLinkTest, type LinkTestEngine, type LinkTestResult, type LinkTestSample } from '@/features/tools/link-test'
import { useMutation, useQuery } from '@tanstack/react-query'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { StatusDot, textClass } from '@/components/ui/status'
import { useGatewayConfig } from '@/features/gateway/queries'
import { ApiError, api } from '@/lib/api'
import { cn } from '@/lib/utils'

interface TestError {
  title: string
  guidance: string
  detail: string
}

function explainFailure(err: unknown): TestError {
  const detail = err instanceof Error ? err.message : String(err)
  if (err instanceof ApiError && err.status === 409) {
    return { get title() { return t('A test is already running') }, guidance: 'Wait for it to finish before starting another.', detail }
  }
  if (err instanceof ApiError && err.status === 401) {
    return { get title() { return t('Authentication required') }, guidance: 'Sign in to the router and try again.', detail }
  }
  if (err instanceof ApiError && err.status === 503) {
    return {
      get title() { return t('Speed test unavailable') },
      guidance: 'The router could not complete a test with the selected service. Check its internet connection, then try again later. This does not mean your network is down.',
      detail,
    }
  }
  return {
    get title() { return t('Could not reach the speed test') },
    guidance: 'Check that this page can still reach the router, then try again. Other router settings are unaffected.',
    detail,
  }
}

interface LANTest {
  addresses: string[]
  address?: string
  expires_at?: string
  port: number
  external?: Record<string, string>
  conflicts?: Record<string, string>
}

interface SpeedResult {
  server: string
  exit?: string
  location: string
  latency_ms: number
  jitter_ms: number | null
  download_mbps: number
  upload_mbps: number
}

/**
 * What the running test has observed so far, as the daemon reports it.
 *
 * `live_mbps` is the rate the transfer in flight is reaching, and the daemon
 * drops it the moment that transfer ends, so a number here is either a settled
 * measurement or one still moving — never a guess at where it will land.
 */
interface SpeedProgress {
  running: boolean
  provider?: string
  exit?: string
  phase?: string
  server?: string
  location?: string
  elapsed_ms: number
  latency_ms: number | null
  download_mbps: number | null
  upload_mbps: number | null
  live_mbps?: number
  error?: string
}

interface SpeedProvider {
  id: string
  name: string
  available: boolean
  detail?: string
}

interface ServerOption {
  id: string
  sponsor: string
  location: string
}

interface ServerList {
  servers: ServerOption[]
  total: number
}

interface NATResult {
  server: string
  mapped: string
  mapping: string
  filtering: string
}

interface PingResult {
  target: string
  address: string
  sent: number
  received: number
  loss_percent: number
  min_ms: number
  avg_ms: number
  max_ms: number
}

interface TraceResult {
  target: string
  output: string
  map_url?: string
}

function TraceView({ trace }: { trace: TraceResult }) {
  useLocale()
  const [mapAvailable, setMapAvailable] = useState(false)
  const [checkingMap, setCheckingMap] = useState(!!trace.map_url)

  useEffect(() => {
    if (!trace.map_url) return
    const controller = new AbortController()
    const timeout = window.setTimeout(() => controller.abort(), 8000)
    fetch(trace.map_url, { signal: controller.signal, mode: 'cors' })
      .then(async response => {
        if (!response.ok || !response.headers.get('content-type')?.includes('text/html')) return false
        const page = await response.text()
        // The viewer also serves its generic search page for expired trace IDs.
        return page.includes('property="og:title"') && !page.includes('Global Route Trace — MTR')
      })
      .then(setMapAvailable)
      .catch(() => setMapAvailable(false))
      .finally(() => {
        window.clearTimeout(timeout)
        setCheckingMap(false)
      })
    return () => {
      window.clearTimeout(timeout)
      controller.abort()
    }
  }, [trace.map_url])

  return <div className="mt-6" aria-live="polite">
    {checkingMap && <p role="status" className="text-sm text-muted-foreground">{t('Checking route map availability…')}</p>}
    {mapAvailable ? <div className="overflow-hidden rounded-xl border">
      <iframe title={`Route map to ${trace.target}`} src={trace.map_url} referrerPolicy="no-referrer" onError={() => setMapAvailable(false)} className="h-[60vh] min-h-[440px] w-full bg-white sm:h-[650px]" />
    </div> : !checkingMap && <>
      {trace.map_url && <p className="mb-3 text-xs text-muted-foreground">{t('Route map unavailable here.')} <a className="text-primary underline underline-offset-4" href={trace.map_url} target="_blank" rel="noopener noreferrer">{t('Open it in a new tab ↗')}</a></p>}
      <pre className="overflow-x-auto rounded-xl border bg-muted/40 p-4 font-mono text-xs leading-relaxed select-text">{trace.output}</pre>
    </>}
  </div>
}

export function ToolPage({ tool }: { tool: ToolSlug }) {
  useLocale()
  const definition = TOOL_DEFINITIONS.find(item => item.slug === tool)!
  const Content = {
    speedtest: OoklaSpeedTest,
    cloudflare: CloudflareSpeedTest,
    fastcom: FastComSpeedTest,
    librespeed: LibreSpeed,
    iperf3: IPerf3,
    nat: NATTest,
    ping: PingTest,
    traceroute: TracerouteTest,
  }[tool]
  return <div className="space-y-8">
    <PageHeading id="tool-title" title={definition.label} />
    <Content key={tool} />
  </div>
}

/** An internet speed test service that has a page of its own. */
type SpeedService = 'ookla' | 'cloudflare' | 'fastcom'

/** The service's own name, for the states its page reports about it. */
const SPEED_SERVICE_NAMES: Record<SpeedService, string> = {
  ookla: 'Speedtest by Ookla',
  cloudflare: 'Cloudflare Speed Test',
  fastcom: 'Fast.com',
}

function OoklaSpeedTest() { return <SpeedTest service="ookla" /> }
function CloudflareSpeedTest() { return <SpeedTest service="cloudflare" /> }
function FastComSpeedTest() { return <SpeedTest service="fastcom" /> }

/** Where the numbers came from: the server the service chose, and the way out. */
function resultSummary(service: SpeedService, result: SpeedResult): string {
  const server = service === 'ookla'
    ? `${t('Test server:')} ${result.server} · ${result.location}`
    : service === 'cloudflare' && result.location ? `${t('Cloudflare edge:')} ${result.location}` : ''
  const exit = `${t('Way out:')} ${result.exit || t('Router default route')}`
  return server ? `${server} · ${exit}` : exit
}

/** What the run is doing, named for the operator rather than for the library. */
function speedPhaseLabel(phase: string | undefined): string {
  switch (phase) {
    case 'server': return t('Finding a test server…')
    case 'latency': return t('Measuring latency…')
    case 'download': return t('Measuring download…')
    case 'upload': return t('Measuring upload…')
    // The command-line services measure both directions inside one process and
    // announce no stage of their own, so this is as specific as the page can be.
    case 'transfer': return t('Measuring download and upload…')
    default: return t('Starting…')
  }
}

/**
 * The stages an Ookla test runs, in the order it runs them.
 *
 * A stage shows its settled measurement, the rate it is reaching while it runs,
 * or nothing at all — an unmeasured stage is not a zero, and the row says which
 * of the three it is with a check, a spinner or a plain dot.
 */
function SpeedStages({ progress }: { progress?: SpeedProgress }) {
  useLocale()
  const stages = [
    { id: 'latency', label: t('Latency'), unit: 'ms', measured: progress?.latency_ms ?? null },
    { id: 'download', label: t('Download'), unit: 'Mbps', measured: progress?.download_mbps ?? null },
    { id: 'upload', label: t('Upload'), unit: 'Mbps', measured: progress?.upload_mbps ?? null },
  ]
  return <ul className="max-w-[15rem] space-y-1.5">
    {stages.map((stage) => {
      const active = stage.measured == null && progress?.phase === stage.id
      const value = stage.measured ?? (active ? progress?.live_mbps ?? null : null)
      return <li key={stage.id} className="flex items-center justify-between gap-4 text-sm">
        <span className="flex items-center gap-2">
          {stage.measured != null
            ? <Check className={cn('size-4', textClass('ok'))} aria-hidden />
            : active
              ? <LoaderCircle className="size-4 animate-spin text-muted-foreground" aria-hidden />
              : <span className="flex size-4 items-center justify-center"><StatusDot tone="unknown" /></span>}
          <span className={stage.measured != null || active ? 'text-foreground' : 'text-muted-foreground'}>{stage.label}</span>
        </span>
        {value != null && <MetricValue size="sm" value={value.toFixed(1)} unit={stage.unit} />}
      </li>
    })}
  </ul>
}

/**
 * One internet speed test service per page.
 *
 * The three measure different paths — Ookla's nearest servers, the closest
 * Cloudflare edge, the Netflix Open Connect route a stream takes — so the tile
 * and the heading name the service, and the page never has to ask which one to
 * run. Ookla alone offers a server to pick; the other two are command-line
 * tools the daemon starts under the chosen way out, and either may not be
 * installed at all.
 */
function SpeedTest({ service }: { service: SpeedService }) {
  useLocale()
  const gateway = useGatewayConfig()
  const [exit, setExit] = useState('')
  const [serverID, setServerID] = useState('')
  const [search, setSearch] = useState('')
  const [appliedSearch, setAppliedSearch] = useState('')
  const exits = gateway.data?.enabled ? (gateway.data.exits ?? []).filter((item) => item.via.kind !== 'blocked') : []
  const exitUnavailable = exit !== '' && !!gateway.data && !exits.some((item) => item.name === exit)
  const selectedExit = exit
  // Ookla ships inside the daemon; only the two command-line services can be
  // absent, and the daemon is the one that can look for them on PATH.
  const services = useQuery({
    queryKey: ['tools', 'speedtest', 'services'],
    queryFn: () => api.get<{ providers: SpeedProvider[] }>('/api/tools/speedtest/providers'),
    enabled: service !== 'ookla',
    staleTime: 60_000,
    retry: false,
  })
  const serviceInfo = services.data?.providers.find((item) => item.id === service)
  const serviceUnavailable = !!serviceInfo && !serviceInfo.available
  // Typing refetches the list, so the term is settled first: every keystroke
  // would otherwise cost a round trip to Ookla.
  useEffect(() => {
    const timer = setTimeout(() => setAppliedSearch(search.trim()), 400)
    return () => clearTimeout(timer)
  }, [search])
  const servers = useQuery({
    queryKey: ['tools', 'speedtest', 'servers', selectedExit, appliedSearch],
    queryFn: () => api.get<ServerList>(`/api/tools/speedtest/servers?${new URLSearchParams({ exit: selectedExit, search: appliedSearch })}`),
    enabled: service === 'ookla',
    staleTime: 60_000,
    retry: false,
  })
  const options = servers.data?.servers ?? []
  const serverUnavailable = serverID !== '' && (servers.isPending || (servers.isSuccess && !options.some((item) => item.id === serverID)))
  const selectedServer = serverID

  const [result, setResult] = useState<SpeedResult | null>(null)
  const [running, setRunning] = useState(false)
  const [error, setError] = useState<TestError | null>(null)
  const [copied, setCopied] = useState(false)
  const [copyFailed, setCopyFailed] = useState(false)
  // The POST stays open until the measurement is done, so the stage it has
  // reached arrives on a second request. It polls rather than streams because
  // EventSource cannot send this API's Authorization header; only a snapshot
  // taken during *this* run is shown, so a previous run's numbers never pass
  // for a fresh one while the first poll is still in flight.
  const progress = useQuery({
    queryKey: ['tools', 'speedtest', 'progress'],
    queryFn: () => api.get<SpeedProgress>('/api/tools/speedtest/progress'),
    enabled: running,
    refetchInterval: 1_000,
    retry: false,
  })
  const live = running && progress.data?.running ? progress.data : undefined

  async function run() {
    if (exitUnavailable || serviceUnavailable || (service === 'ookla' && serverUnavailable)) return
    setRunning(true)
    setResult(null)
    setError(null)
    setCopied(false)
    setCopyFailed(false)
    try {
      const query = new URLSearchParams({ exit: selectedExit, provider: service })
      if (service === 'ookla') {
        query.set('server_id', selectedServer)
        query.set('search', appliedSearch)
      }
      setResult(await api.post<SpeedResult>(`/api/tools/speedtest?${query}`))
    } catch (err) {
      setError(explainFailure(err))
    } finally {
      setRunning(false)
    }
  }

  async function copyDiagnostics() {
    if (!error) return
    try {
      await navigator.clipboard.writeText(`Open Linux Router ${__APP_VERSION__}\nSpeed test: ${error.detail}`)
      setCopied(true)
      setCopyFailed(false)
    } catch {
      setCopyFailed(true)
    }
  }

  // What this service measures; the bandwidth caveat under it is the same for
  // all three, and two lines keep each language's own sentence spacing.
  const intro = service === 'ookla'
    ? t('Measures this router’s own connection through Ookla’s nearby servers.')
    : service === 'cloudflare'
      ? t('Measures this router’s own connection to the nearest Cloudflare edge.')
      : t('Measures the Netflix Open Connect path a streaming session uses.')

  return <div className="space-y-6">
      <div className="panel-surface relative p-4 sm:p-6">
        <div className="relative space-y-5">
          <div className="max-w-xl space-y-1 text-sm text-muted-foreground">
            <p>{intro}</p>
            <p>{t('Uses your internet bandwidth and may affect other devices while it runs.')}</p>
          </div>
          {serviceUnavailable
            ? <Alert variant="destructive">
                <AlertTitle>{t('{name} is unavailable', { name: SPEED_SERVICE_NAMES[service] })}</AlertTitle>
                <AlertDescription className="space-y-3">
                  {serviceInfo?.detail && <p className="break-words font-mono text-xs select-text">{serviceInfo.detail}</p>}
                  <p>{t('Install that command on the router’s PATH, then reload this page.')}</p>
                </AlertDescription>
              </Alert>
            : <>
                <div className="max-w-sm space-y-1.5">
                  <Label htmlFor="speed-exit">{t('Way out')}</Label>
                  <Select value={selectedExit || 'default'} onValueChange={(value) => { setExit(value === 'default' ? '' : value ?? ''); setServerID('') }} disabled={running || gateway.isPending}>
                    <SelectTrigger id="speed-exit" className="w-full"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="default">{t('Router default route')}</SelectItem>
                      {exits.map((item) => <SelectItem key={item.name} value={item.name}>{item.name}</SelectItem>)}
                    </SelectContent>
                  </Select>
                  <p className="text-xs text-muted-foreground">{t('Applies to this test only.')}</p>
                  {exitUnavailable && <p className="text-xs text-destructive">{t('This way out is no longer available. Choose another before testing.')}</p>}
                  {gateway.isError && <p className="text-xs text-destructive">{t('Could not load ways out. The router default route is still available.')}</p>}
                </div>
                {service === 'ookla' && <div className="max-w-2xl space-y-1.5">
                  <Label htmlFor="speed-server">{t('Test server')}</Label>
                  <Input id="speed-server-search" type="search" value={search} onChange={(event) => { setSearch(event.target.value); setServerID('') }} placeholder={t('Country, city or provider')} aria-label={t('Narrow the server list')} maxLength={64} disabled={running} />
                  <Select value={selectedServer || 'auto'} onValueChange={(value) => setServerID(value === 'auto' ? '' : value ?? '')} disabled={running || servers.isPending}>
                    <SelectTrigger id="speed-server" className="w-full"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="auto">{t('Automatic (first working server)')}</SelectItem>
                      {options.map((item) => <SelectItem key={item.id} value={item.id}>{item.sponsor} · {item.location}</SelectItem>)}
                    </SelectContent>
                  </Select>
                  {servers.isPending && <p className="text-xs text-muted-foreground">{t('Finding nearby servers…')}</p>}
                  {serverUnavailable && <p className="text-xs text-destructive">{t('Refresh the server list or choose another server before testing.')}</p>}
                  {servers.isError && <p className="text-xs text-destructive">{t('Could not list servers:')} {servers.error.message}. Automatic selection is still available.</p>}
                  {servers.isSuccess && options.length === 0 && <p className="text-xs text-destructive">{t('No server matched that search. Clear it to see nearby servers.')}</p>}
                  {servers.isSuccess && options.length > 0 && <p className="text-xs text-muted-foreground">{t('{count} servers listed near this way out; latency is measured when the test runs.', { count: servers.data.total })}</p>}
                  {servers.isSuccess && servers.data.total > options.length && <p className="text-xs text-muted-foreground">{t('Showing the {shown} closest.', { shown: options.length })}</p>}
                </div>}
                <Button onClick={run} disabled={running || gateway.isPending || exitUnavailable || (service === 'ookla' && serverUnavailable)}>
                  {running ? <LoaderCircle className="animate-spin" aria-hidden /> : <Activity aria-hidden />}
                  {running ? 'Testing connection…' : result ? 'Run again' : 'Start speed test'}
                </Button>
                {running && <div className="max-w-md space-y-2">
                  <p className="text-sm">
                    {/* The elapsed count changes every second, so it sits
                        outside the live region: a screen reader should hear the
                        stage change, not tick along with the clock. */}
                    <span role="status">{speedPhaseLabel(live?.phase)}</span>
                    {live && <span aria-hidden className="ml-2 text-xs tabular-nums text-muted-foreground">{Math.floor(live.elapsed_ms / 1_000)}s</span>}
                  </p>
                  {service === 'ookla' && <SpeedStages progress={live} />}
                  <p className="text-xs text-muted-foreground">{t('This test can take up to 90 seconds.')}</p>
                </div>}
              </>}
        </div>
      </div>

      {error && (
        <Alert variant="destructive">
          <AlertTitle>{error.title}</AlertTitle>
          <AlertDescription className="space-y-3">
            <p>{error.guidance}</p>
            <details className="text-foreground">
              <summary className="cursor-pointer text-xs">{t('Technical details')}</summary>
              <p className="mt-2 break-words font-mono text-xs select-text">{error.detail}</p>
            </details>
            <div className="flex flex-wrap items-center gap-3">
              <Button variant="secondary" size="sm" onClick={copyDiagnostics}>
                <Copy aria-hidden />{copied ? t('Copied') : 'Copy diagnostics'}
              </Button>
              <a className="text-xs underline underline-offset-4" href="https://github.com/open-linux-router/open-linux-router/issues/new" target="_blank" rel="noreferrer">{t('Report an issue')}</a>
            </div>
            {copyFailed && <p className="text-xs">{t('Could not copy automatically. Select the technical details above to copy them.')}</p>}
            <p className="text-xs">Nothing is sent automatically. Paste the diagnostics into an issue only if you choose to report it.</p>
          </AlertDescription>
        </Alert>
      )}

      {result && (
        <div className="space-y-4" aria-live="polite">
          <div className="grid gap-3 sm:grid-cols-3">
            <Metric icon={Activity} label={t('Latency')} value={result.latency_ms} unit="ms" detail={result.jitter_ms == null ? undefined : `${t('Jitter')} ${result.jitter_ms.toFixed(1)} ms`} />
            <Metric icon={ArrowDown} label={t('Download')} value={result.download_mbps} unit="Mbps" />
            <Metric icon={ArrowUp} label={t('Upload')} value={result.upload_mbps} unit="Mbps" />
          </div>
          <p className="text-sm text-muted-foreground">{resultSummary(service, result)}</p>
        </div>
      )}
  </div>
}

/**
 * The device-to-router test.
 *
 * The measurement is LibreSpeed's engine, but the page is OLR's: a start
 * control, one running state, and the same result tiles the internet speed
 * tests use. The engine's background, title and gauges are gone on purpose, and
 * so are its moving numbers — a test that takes half a minute does not need a
 * reader to watch it fluctuate, only the answer at the end.
 */
function LibreSpeed() {
  useLocale()
  const [granted, setGranted] = useState(false)
  const [grantError, setGrantError] = useState<string | null>(null)
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState<LinkTestResult | null>(null)
  const [failure, setFailure] = useState<string | null>(null)
  const engine = useRef<LinkTestEngine | null>(null)
  const deadline = useRef<number | undefined>(undefined)
  const asked = useRef(false)
  const grant = useMutation({
    mutationFn: () => api.post<void>('/api/tools/link-test/session'),
    onSuccess: () => setGranted(true),
    onError: error => setGrantError(error instanceof Error ? error.message : String(error)),
  })
  const requestSession = grant.mutate
  // The test surface is not part of the API: the upload test posts raw bytes,
  // which the API refuses by design. So it takes its credential as a cookie
  // rather than as the header this page puts on every API call. Asking for one
  // on arrival keeps the test a single click, and the grant is scoped to
  // /link-test/ and dies with the browser session.
  useEffect(() => {
    if (asked.current) return
    asked.current = true
    requestSession()
  }, [requestSession])

  // Leaving the page mid-test stops the transfer; the router would otherwise
  // keep serving blocks to a browser that is no longer reading them. `gone`
  // also covers a start that is still waiting for the engine to load.
  const gone = useRef(false)
  useEffect(() => () => {
    gone.current = true
    window.clearTimeout(deadline.current)
    const test = engine.current
    engine.current = null
    if (!test) return
    test.onend = null
    try { test.abort() } catch { /* the test had already finished */ }
  }, [])

  async function run() {
    if (running || !granted) return
    setRunning(true)
    setResult(null)
    setFailure(null)
    const latest: { sample: LinkTestSample | null } = { sample: null }
    let settled = false
    const settle = (value: LinkTestResult | null, message: string | null) => {
      if (settled) return
      settled = true
      window.clearTimeout(deadline.current)
      engine.current = null
      setRunning(false)
      setResult(value)
      setFailure(message)
    }
    try {
      // A page left open may have outlived its viewer grant, so every run mints
      // a fresh one: the test should not depend on how long this tab has been
      // sitting here.
      await api.post<void>('/api/tools/link-test/session')
      const [Engine, settings] = await Promise.all([loadLinkTestEngine(), loadLinkTestSettings()])
      if (gone.current) return
      const test = createLinkTestEngine(Engine, settings)
      engine.current = test
      // The worker reports on every tick; the page keeps the last sample for the
      // result and shows none of them while the test runs.
      test.onupdate = data => { latest.sample = data }
      test.onend = aborted => {
        const finished = aborted || !latest.sample ? null : linkTestResult(latest.sample)
        settle(finished, finished ? null
          : aborted ? t('The test was stopped before it finished.')
            : t('The transfer test finished without usable numbers.'))
      }
      startLinkTest(test)
      // The engine ends itself; this only covers a worker that goes quiet.
      deadline.current = window.setTimeout(() => {
        try { test.abort() } catch { /* the test had already finished */ }
        settle(null, t('The test did not finish in time.'))
      }, 60_000)
    } catch (error) {
      settle(null, error instanceof Error ? error.message : String(error))
    }
  }

  return <div className="space-y-6">
    <div className="panel-surface relative p-4 sm:p-6">
      <div className="relative space-y-5">
        <div className="max-w-2xl space-y-1 text-sm text-muted-foreground">
          <p>{t('Measure how fast this device can send and receive data on your local network, in the browser.')}</p>
          <p>{t('The test measures this device’s link to the router, not the router’s internet speed.')}</p>
        </div>
        {grantError
          ? <Alert variant="destructive">
              <AlertTitle>{t('LibreSpeed is unavailable')}</AlertTitle>
              <AlertDescription className="space-y-3">
                <p className="break-words">{grantError}</p>
                <Button variant="secondary" size="sm" onClick={() => { setGrantError(null); requestSession() }}>{t('Try again')}</Button>
              </AlertDescription>
            </Alert>
          : <>
              <Button onClick={run} disabled={running || !granted}>
                {running ? <LoaderCircle className="animate-spin" aria-hidden /> : <Activity aria-hidden />}
                {running ? t('Testing…') : result ? t('Run again') : t('Start test')}
              </Button>
              {running && <p role="status" className="text-sm text-muted-foreground">{t('Testing this device’s link to the router. It usually takes about 30 seconds.')}</p>}
            </>}
      </div>
    </div>

    {failure && (
      <Alert variant="destructive">
        <AlertTitle>{t('The test could not finish')}</AlertTitle>
        <AlertDescription className="space-y-3">
          <p className="break-words">{failure}</p>
          <Button variant="secondary" size="sm" onClick={run}>{t('Try again')}</Button>
        </AlertDescription>
      </Alert>
    )}

    {result && (
      <div className="space-y-4" aria-live="polite">
        <div className="grid gap-3 sm:grid-cols-3">
          <Metric icon={Activity} label={t('Latency')} value={result.latency} unit="ms" detail={result.jitter == null ? undefined : `${t('Jitter')} ${result.jitter.toFixed(1)} ms`} />
          <Metric icon={ArrowDown} label={t('Download')} value={result.download} unit="Mbps" />
          <Metric icon={ArrowUp} label={t('Upload')} value={result.upload} unit="Mbps" />
        </div>
      </div>
    )}
  </div>
}

function IPerf3() {
  useLocale()
  const lan = useQuery({
    queryKey: ['tools', 'lan-test'],
    queryFn: () => api.get<LANTest>('/api/tools/lan-test'),
    refetchInterval: 5_000,
    retry: false,
  })
  const [lanAddress, setLANAddress] = useState('')
  const [lanBusy, setLANBusy] = useState(false)
  const [lanError, setLANError] = useState<string | null>(null)
  const [lanCopied, setLANCopied] = useState<string | null>(null)
  const addresses = lan.data?.addresses ?? []
  const selectedLAN = addresses.includes(lanAddress) ? lanAddress : addresses[0] ?? ''
  const external = selectedLAN ? lan.data?.external?.[selectedLAN] : undefined
  const conflict = selectedLAN ? lan.data?.conflicts?.[selectedLAN] : undefined
  const testAddress = lan.data?.address || (external ? selectedLAN : '')

  async function changeLAN(start: boolean) {
    setLANBusy(true)
    setLANError(null)
    try {
      if (start) await api.post<LANTest>('/api/tools/lan-test', { address: selectedLAN })
      else await api.send<void>('DELETE', '/api/tools/lan-test')
      await lan.refetch()
    } catch (err) {
      setLANError(err instanceof Error ? err.message : String(err))
    } finally {
      setLANBusy(false)
    }
  }

  async function copyLAN(command: string) {
    try {
      await navigator.clipboard.writeText(command)
      setLANCopied(command)
    } catch {
      setLANError(t('Could not copy automatically. Select the command to copy it.'))
    }
  }

  return <div className="space-y-6">
    <section className="panel-surface p-4 sm:p-6" aria-labelledby="tool-title">
      <p className="max-w-2xl text-sm text-muted-foreground">{t('iPerf3 measures sustained TCP streams with its own client on the other device. It is the honest choice for a wired link faster than a browser can measure, and it needs iperf3 installed on the router and on that device. It tests the link and the router’s own receive/send capacity, not LAN-to-WAN forwarding or NAT.')}</p>
      {lan.isError && <Alert variant="destructive" className="mt-4"><AlertTitle>{t('LAN addresses unavailable')}</AlertTitle><AlertDescription>{lan.error.message}</AlertDescription></Alert>}
      {lan.data && <div className="mt-5 space-y-4">
        <div className="max-w-sm space-y-1.5">
          <Label htmlFor="lan-address">{t('Router LAN address')}</Label>
          <Select value={lan.data.address || selectedLAN} onValueChange={value => setLANAddress(value ?? '')} disabled={!!lan.data.address || lanBusy || !addresses.length}>
            <SelectTrigger id="lan-address"><SelectValue placeholder={t('No configured LAN IPv4 address')} /></SelectTrigger>
            <SelectContent>{addresses.map(address => <SelectItem key={address} value={address}>{address}</SelectItem>)}</SelectContent>
          </Select>
        </div>
        {conflict && !lan.data.address && <Alert variant="destructive"><AlertTitle>{t('Port unavailable')}</AlertTitle><AlertDescription>{conflict}</AlertDescription></Alert>}
        {external && !lan.data.address && <p role="status" className="text-sm text-muted-foreground">{t('Existing iperf3 server:')} {external}. OLR did not start it and will not stop it.</p>}
        {(!external || lan.data.address) && <Button onClick={() => changeLAN(!lan.data?.address)} disabled={lanBusy || (!lan.data.address && (!selectedLAN || !!conflict))} variant={lan.data.address ? 'secondary' : 'primary'}>
          {lanBusy && <LoaderCircle className="animate-spin" aria-hidden />}
          {lan.data.address ? t('Stop server') : t('Start server')}
        </Button>}
        {testAddress && <div className="space-y-3 rounded-xl border bg-muted/30 p-4">
          <p className="text-sm font-medium">{lan.data.address
            ? t('OLR is listening on {address} until {time}', { address: `${testAddress}:${lan.data.port}`, time: new Date(lan.data.expires_at!).toLocaleTimeString() })
            : t('An existing server is running on {address}; its lifetime is managed outside OLR', { address: `${testAddress}:${lan.data.port}` })}</p>
          <p className="text-xs text-muted-foreground">{t('Anyone able to reach that address and port can use the server while it runs, and a configured firewall may need a temporary TCP 5201 opening. Run these on the other device’s terminal; the first sends data to OLR, the second makes OLR send data back.')}</p>
          {[
            `iperf3 -c ${testAddress} -P 4 -t 10`,
            `iperf3 -c ${testAddress} -P 4 -t 10 -R`,
          ].map((command, index) => <div key={command} className="space-y-1">
            <p className="text-xs font-medium">{index === 0 ? t('Device → router') : t('Router → device')}</p>
            <div className="flex flex-wrap items-center gap-2">
              <code className="min-w-0 flex-1 overflow-x-auto rounded-lg bg-background px-3 py-2 text-sm">{command}</code>
              <Button variant="secondary" size="sm" onClick={() => copyLAN(command)}><Copy aria-hidden />{lanCopied === command ? t('Copied') : t('Copy')}</Button>
            </div>
          </div>)}
        </div>}
      </div>}
      {lanError && <Alert variant="destructive" className="mt-4"><AlertTitle>{t('iPerf3 is unavailable')}</AlertTitle><AlertDescription>{lanError}</AlertDescription></Alert>}
    </section>
  </div>
}

function NATTest() {
  useLocale()
  const [natResult, setNATResult] = useState<NATResult | null>(null)
  const [natRunning, setNATRunning] = useState(false)
  const [natError, setNATError] = useState<string | null>(null)

  async function runNAT() {
    setNATRunning(true)
    setNATResult(null)
    setNATError(null)
    try {
      setNATResult(await api.post<NATResult>('/api/tools/nat'))
    } catch (err) {
      setNATError(err instanceof Error ? err.message : String(err))
    } finally {
      setNATRunning(false)
    }
  }


  return <div className="space-y-6">
      <section className="panel-surface p-4 sm:p-6" aria-labelledby="tool-title">
        <div className="flex items-start gap-4">
          <div className="space-y-1">
            <p className="text-sm text-muted-foreground">Send UDP probes from this router over its default route to public STUN servers. This does not test a device behind the router or whether a game port is open.</p>
          </div>
        </div>
        <Button className="mt-5" onClick={runNAT} disabled={natRunning}>
          {natRunning && <LoaderCircle className="animate-spin" aria-hidden />}
          {natRunning ? 'Testing…' : 'Run NAT test'}
        </Button>
        {natError && <Alert variant="destructive" className="mt-5"><AlertTitle>{t('NAT test unavailable')}</AlertTitle><AlertDescription className="break-words">{natError}</AlertDescription></Alert>}
        {natResult && <div className="mt-5 space-y-4 text-sm" aria-live="polite">
          <div className="rounded-xl border bg-muted/40 p-4">
            <p className="font-medium">{t('What this test found')}</p>
            <p className="mt-1 text-muted-foreground">{t('The STUN server saw this router\'s UDP traffic as')} <span className="font-mono text-foreground">{natResult.mapped}</span>. This is the address and temporary port observed for this test, not proof that incoming connections can reach the router.</p>
            {(natResult.mapping === 'not tested' || natResult.filtering === 'not tested') && <p className="mt-2 text-muted-foreground">The server did not provide enough usable replies to determine {natResult.mapping === 'not tested' && natResult.filtering === 'not tested' ? 'mapping or filtering' : natResult.mapping === 'not tested' ? 'mapping' : 'filtering'}. Your NAT behavior is unknown, not necessarily restrictive.</p>}
          </div>
          <dl className="grid gap-3 sm:grid-cols-2">
            <div className="rounded-xl border p-4">
              <dt className="font-medium">{t('Mapping')}</dt>
              <dd className="mt-1 font-mono text-sm">{natResult.mapping}</dd>
              <p className="mt-2 text-xs text-muted-foreground">{t('Whether the public UDP address and port change when this router contacts different destinations.')} {mappingExplanation(natResult.mapping)}</p>
            </div>
            <div className="rounded-xl border p-4">
              <dt className="font-medium">{t('Filtering')}</dt>
              <dd className="mt-1 font-mono text-sm">{natResult.filtering}</dd>
              <p className="mt-2 text-xs text-muted-foreground">{t('Whether replies from another server IP or port get through.')} {filteringExplanation(natResult.filtering)}</p>
            </div>
          </dl>
          <p className="text-xs text-muted-foreground">{t('STUN server:')} <span className="font-mono">{natResult.server}</span>. Results do not determine an Xbox or PlayStation NAT type.</p>
        </div>}
      </section>
  </div>
}

function PingTest() {
  useLocale()
  const [target, setTarget] = useState('1.1.1.1')
  const [pingResult, setPingResult] = useState<PingResult | null>(null)
  const [pingRunning, setPingRunning] = useState(false)
  const [pingError, setPingError] = useState<string | null>(null)

  async function runPing(event: React.SubmitEvent<HTMLFormElement>) {
    event.preventDefault()
    setPingRunning(true)
    setPingResult(null)
    setPingError(null)
    try {
      setPingResult(await api.post<PingResult>('/api/tools/ping', { target: target.trim() }))
    } catch (err) {
      setPingError(err instanceof Error ? err.message : String(err))
    } finally {
      setPingRunning(false)
    }
  }


  return <div className="space-y-6">
      <section className="panel-surface p-4 sm:p-6" aria-labelledby="tool-title">
        <div className="flex items-start gap-4">
          <div className="space-y-1">
            <p className="text-sm text-muted-foreground">Send four ICMP probes from the router, not your browser, to check reachability and latency.</p>
          </div>
        </div>
        <form onSubmit={runPing} className="mt-6 flex flex-col gap-3 sm:flex-row sm:items-end">
          <label className="flex-1 space-y-2 text-sm font-medium" htmlFor="ping-target"> {t('Hostname or IP address')} <Input id="ping-target" value={target} onChange={event => setTarget(event.target.value)} placeholder="example.com or 192.168.1.1" maxLength={253} required disabled={pingRunning} />
          </label>
          <Button type="submit" disabled={pingRunning || !target.trim()}>
            {pingRunning && <LoaderCircle className="animate-spin" aria-hidden />}
            {pingRunning ? 'Pinging…' : 'Run ping'}
          </Button>
        </form>
        {pingRunning && <p role="status" className="mt-4 text-sm text-muted-foreground">{t('Sending four probes. This may take up to 10 seconds.')}</p>}
        {pingError && <Alert variant="destructive" className="mt-5"><AlertTitle>Ping unavailable</AlertTitle><AlertDescription className="break-words">{pingError}</AlertDescription></Alert>}
        {pingResult && (
          <div className="mt-6 space-y-4" aria-live="polite">
            <p className="text-sm text-muted-foreground">{pingResult.target} resolved to <span className="font-mono text-foreground">{pingResult.address}</span></p>
            <div className="grid gap-3 sm:grid-cols-3">
              <Metric surface="inset" icon={Radio} label={t('Packet loss')} value={pingResult.loss_percent} unit="%" detail={`${pingResult.received} of ${pingResult.sent} replies`} />
              <Metric surface="inset" icon={Activity} label={t('Average RTT')} value={pingResult.received ? pingResult.avg_ms : null} unit="ms" detail={pingResult.received ? `Min ${pingResult.min_ms.toFixed(1)} · Max ${pingResult.max_ms.toFixed(1)} ms` : 'No replies received'} />
              <div className="rounded-lg bg-muted/50 p-4 text-sm text-muted-foreground">
                {pingResult.received === 0 ? 'No replies. The destination may be unreachable or block ICMP.' : 'Replies received from the destination.'}
              </div>
            </div>
          </div>
        )}
      </section>
  </div>
}

function TracerouteTest() {
  useLocale()
  const [target, setTarget] = useState('1.1.1.1')
  const [trace, setTrace] = useState<TraceResult | null>(null)
  const [traceRunning, setTraceRunning] = useState(false)
  const [traceError, setTraceError] = useState<string | null>(null)

  async function runTraceroute(event: React.SubmitEvent<HTMLFormElement>) {
    event.preventDefault()
    setTraceRunning(true)
    setTrace(null)
    setTraceError(null)
    try {
      setTrace(await api.post<TraceResult>('/api/tools/traceroute', { target: target.trim() }))
    } catch (err) {
      setTraceError(err instanceof Error ? err.message : String(err))
    } finally {
      setTraceRunning(false)
    }
  }


  return <div className="space-y-6">
      <section className="panel-surface p-4 sm:p-6" aria-labelledby="tool-title">
        <div className="flex items-start gap-4">
          <div>
            <p className="mt-1 text-sm text-muted-foreground">{t('Follow the path from this router to a domain or IP address. Powered by NextTrace; sends network probes and may query its IP location service.')}</p>
          </div>
        </div>
        <form onSubmit={runTraceroute} className="mt-6 flex flex-col gap-3 sm:flex-row sm:items-end">
          <label className="flex-1 space-y-2 text-sm font-medium" htmlFor="trace-target">
            <span>{t('Destination')}</span>
            <Input id="trace-target" value={target} onChange={(event) => setTarget(event.target.value)} placeholder="example.com or 1.1.1.1" required maxLength={253} disabled={traceRunning} />
          </label>
          <Button type="submit" disabled={traceRunning || !target.trim()}>
            {traceRunning ? <LoaderCircle className="animate-spin" aria-hidden /> : <Route aria-hidden />}
            {traceRunning ? 'Tracing…' : 'Trace route'}
          </Button>
        </form>
        {traceRunning && <p role="status" className="mt-4 text-sm text-muted-foreground">Probing up to 20 hops. This may take up to 75 seconds.</p>}
        {traceError && <Alert variant="destructive" className="mt-5"><AlertTitle>{t('Traceroute unavailable')}</AlertTitle><AlertDescription className="break-words">{traceError}</AlertDescription></Alert>}
        {trace && <TraceView key={trace.map_url ?? trace.output} trace={trace} />}
      </section>
  </div>
}
function Metric({ icon: Icon, label, value, unit, detail, surface = 'panel' }: {
  icon: typeof Activity
  label: string
  value: number | null
  unit: string
  detail?: string
  surface?: 'panel' | 'inset'
}) {
  useLocale()
  return (
    <div className={surface === 'inset' ? 'rounded-lg bg-muted/50 p-4' : 'panel-surface p-4 sm:p-6'}>
      <div className="flex items-center gap-2 text-sm text-muted-foreground"><Icon className="size-4" aria-hidden />{label}</div>
      <MetricValue size="lg" value={value?.toFixed(1)} unit={unit} className="mt-4" />
      {detail && <p className="mt-2 text-xs text-muted-foreground">{detail}</p>}
    </div>
  )
}

function mappingExplanation(value: string): string {
  switch (value) {
    case 'endpoint-independent': return 'The same address and port were observed for the tested destinations.'
    case 'address-dependent': return 'The mapping changed with the destination IP.'
    case 'address-and-port-dependent': return 'The mapping changed with the destination IP and port.'
    default: return 'No conclusion from this test.'
  }
}

function filteringExplanation(value: string): string {
  switch (value) {
    case 'endpoint-independent': return 'A reply from a different IP and port arrived.'
    case 'address-dependent': return 'A reply from the same IP but a different port arrived; the different-IP reply did not.'
    case 'no changed-source reply': return 'No reply from a changed source arrived. This could be filtering, server behavior, or packet loss.'
    default: return 'No conclusion from this test.'
  }
}
