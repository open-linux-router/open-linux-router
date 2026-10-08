import { Activity, ArrowDown, ArrowUp, Copy, Gauge, LoaderCircle, Radio, Route } from 'lucide-react'
import { useState } from 'react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ApiError, api } from '@/lib/api'

interface TestError {
  title: string
  guidance: string
  detail: string
}

function explainFailure(err: unknown): TestError {
  const detail = err instanceof Error ? err.message : String(err)
  if (err instanceof ApiError && err.status === 409) {
    return { title: 'A test is already running', guidance: 'Wait for it to finish before starting another.', detail }
  }
  if (err instanceof ApiError && err.status === 401) {
    return { title: 'Authentication required', guidance: 'Sign in to the router and try again.', detail }
  }
  if (err instanceof ApiError && err.status === 503) {
    return {
      title: 'Speed test unavailable',
      guidance: 'The router could not complete a test with the available servers. Check its internet connection, then try again later. This does not mean your network is down.',
      detail,
    }
  }
  return {
    title: 'Could not reach the speed test',
    guidance: 'Check that this page can still reach the router, then try again. Other router settings are unaffected.',
    detail,
  }
}

interface SpeedResult {
  server: string
  location: string
  latency_ms: number
  jitter_ms: number
  download_mbps: number
  upload_mbps: number
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
}

export function ToolsPage() {
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

  const [result, setResult] = useState<SpeedResult | null>(null)
  const [running, setRunning] = useState(false)
  const [error, setError] = useState<TestError | null>(null)
  const [copied, setCopied] = useState(false)
  const [copyFailed, setCopyFailed] = useState(false)

  async function run() {
    setRunning(true)
    setResult(null)
    setError(null)
    setCopied(false)
    setCopyFailed(false)
    try {
      setResult(await api.post<SpeedResult>('/api/tools/speedtest'))
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

  return (
    <div className="mx-auto max-w-4xl space-y-6">
      <div className="relative overflow-hidden rounded-2xl border bg-card p-6 sm:p-10">
        <div className="pointer-events-none absolute -right-16 -top-24 size-72 rounded-full bg-primary/8 blur-3xl" />
        <div className="relative space-y-5">
          <div className="flex size-12 items-center justify-center rounded-xl bg-primary/10 text-primary">
            <Gauge className="size-6" aria-hidden />
          </div>
          <div className="space-y-2">
            <h2 className="text-2xl font-semibold tracking-tight">Internet speed test</h2>
            <p className="max-w-xl text-sm text-muted-foreground">
              Measure the router's own connection to a nearby test server. This uses your internet bandwidth and may affect other devices while it runs.
            </p>
          </div>
          <Button onClick={run} disabled={running}>
            {running ? <LoaderCircle className="animate-spin" aria-hidden /> : <Activity aria-hidden />}
            {running ? 'Testing connection…' : result ? 'Run again' : 'Start speed test'}
          </Button>
          {running && <p role="status" className="text-sm text-muted-foreground">Finding a server, then measuring ping, download and upload. This may take up to 90 seconds.</p>}
        </div>
      </div>

      {error && (
        <Alert variant="destructive">
          <AlertTitle>{error.title}</AlertTitle>
          <AlertDescription className="space-y-3">
            <p>{error.guidance}</p>
            <details className="text-foreground">
              <summary className="cursor-pointer text-xs">Technical details</summary>
              <p className="mt-2 break-words font-mono text-xs select-text">{error.detail}</p>
            </details>
            <div className="flex flex-wrap items-center gap-3">
              <Button variant="outline" size="sm" onClick={copyDiagnostics}>
                <Copy aria-hidden />{copied ? 'Copied' : 'Copy diagnostics'}
              </Button>
              <a className="text-xs underline underline-offset-4" href="https://github.com/open-linux-router/open-linux-router/issues/new" target="_blank" rel="noreferrer">Report an issue</a>
            </div>
            {copyFailed && <p className="text-xs">Could not copy automatically. Select the technical details above to copy them.</p>}
            <p className="text-xs">Nothing is sent automatically. Paste the diagnostics into an issue only if you choose to report it.</p>
          </AlertDescription>
        </Alert>
      )}

      {result && (
        <div className="space-y-4" aria-live="polite">
          <div className="grid gap-3 sm:grid-cols-3">
            <Metric icon={Activity} label="Latency" value={result.latency_ms} unit="ms" detail={`Jitter ${result.jitter_ms.toFixed(1)} ms`} />
            <Metric icon={ArrowDown} label="Download" value={result.download_mbps} unit="Mbps" />
            <Metric icon={ArrowUp} label="Upload" value={result.upload_mbps} unit="Mbps" />
          </div>
          <p className="text-sm text-muted-foreground">Test server: {result.server} · {result.location}</p>
        </div>
      )}
      <section className="rounded-2xl border bg-card p-6 sm:p-8" aria-labelledby="ping-heading">
        <div className="flex items-start gap-4">
          <div className="flex size-11 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary"><Radio className="size-5" aria-hidden /></div>
          <div className="space-y-1">
            <h2 id="ping-heading" className="text-xl font-semibold tracking-tight">Ping a destination</h2>
            <p className="text-sm text-muted-foreground">Send four ICMP probes from the router, not your browser, to check reachability and latency.</p>
          </div>
        </div>
        <form onSubmit={runPing} className="mt-6 flex flex-col gap-3 sm:flex-row sm:items-end">
          <label className="flex-1 space-y-2 text-sm font-medium" htmlFor="ping-target">
            Hostname or IP address
            <Input id="ping-target" value={target} onChange={event => setTarget(event.target.value)} placeholder="example.com or 192.168.1.1" maxLength={253} required disabled={pingRunning} />
          </label>
          <Button type="submit" disabled={pingRunning || !target.trim()}>
            {pingRunning && <LoaderCircle className="animate-spin" aria-hidden />}
            {pingRunning ? 'Pinging…' : 'Run ping'}
          </Button>
        </form>
        {pingRunning && <p role="status" className="mt-4 text-sm text-muted-foreground">Sending four probes. This may take up to 10 seconds.</p>}
        {pingError && <Alert variant="destructive" className="mt-5"><AlertTitle>Ping unavailable</AlertTitle><AlertDescription className="break-words">{pingError}</AlertDescription></Alert>}
        {pingResult && (
          <div className="mt-6 space-y-4" aria-live="polite">
            <p className="text-sm text-muted-foreground">{pingResult.target} resolved to <span className="font-mono text-foreground">{pingResult.address}</span></p>
            <div className="grid gap-3 sm:grid-cols-3">
              <Metric icon={Radio} label="Packet loss" value={pingResult.loss_percent} unit="%" detail={`${pingResult.received} of ${pingResult.sent} replies`} />
              <Metric icon={Activity} label="Average RTT" value={pingResult.avg_ms} unit="ms" detail={pingResult.received ? `Min ${pingResult.min_ms.toFixed(1)} · Max ${pingResult.max_ms.toFixed(1)} ms` : 'No replies received'} />
              <div className="flex items-center rounded-xl border bg-card p-5 text-sm text-muted-foreground">
                {pingResult.received === 0 ? 'No replies. The destination may be unreachable or block ICMP.' : 'Replies received from the destination.'}
              </div>
            </div>
          </div>
        )}
      </section>
      <section className="rounded-2xl border bg-card p-6 sm:p-8" aria-labelledby="trace-title">
        <div className="flex items-start gap-4">
          <div className="flex size-12 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary"><Route className="size-6" aria-hidden /></div>
          <div>
            <h2 id="trace-title" className="text-xl font-semibold tracking-tight">Traceroute</h2>
            <p className="mt-1 text-sm text-muted-foreground">Follow the path from this router to a domain or IP address. Powered by NextTrace; sends network probes and may query its IP location service.</p>
          </div>
        </div>
        <form onSubmit={runTraceroute} className="mt-6 flex flex-col gap-3 sm:flex-row sm:items-end">
          <label className="flex-1 space-y-2 text-sm font-medium" htmlFor="trace-target">
            <span>Destination</span>
            <Input id="trace-target" value={target} onChange={(event) => setTarget(event.target.value)} placeholder="example.com or 1.1.1.1" required maxLength={253} disabled={traceRunning} />
          </label>
          <Button type="submit" disabled={traceRunning || !target.trim()}>
            {traceRunning ? <LoaderCircle className="animate-spin" aria-hidden /> : <Route aria-hidden />}
            {traceRunning ? 'Tracing…' : 'Trace route'}
          </Button>
        </form>
        {traceRunning && <p role="status" className="mt-4 text-sm text-muted-foreground">Probing up to 20 hops. This may take up to 75 seconds.</p>}
        {traceError && <Alert variant="destructive" className="mt-5"><AlertTitle>Traceroute unavailable</AlertTitle><AlertDescription className="break-words">{traceError}</AlertDescription></Alert>}
        {trace && <div className="mt-6" aria-live="polite">
          <p className="mb-3 text-sm text-muted-foreground">Route to {trace.target}</p>
          <pre className="overflow-x-auto rounded-xl border bg-muted/40 p-4 font-mono text-xs leading-relaxed select-text">{trace.output}</pre>
        </div>}
      </section>
      <p className="text-xs text-muted-foreground">Traceroute requires <a className="underline underline-offset-4" href="https://github.com/nxtrace/NTrace-core" target="_blank" rel="noreferrer">NextTrace</a> installed on the router.</p>

    </div>
  )
}

function Metric({ icon: Icon, label, value, unit, detail }: {
  icon: typeof Activity
  label: string
  value: number
  unit: string
  detail?: string
}) {
  return (
    <div className="rounded-xl border bg-card p-5">
      <div className="flex items-center gap-2 text-sm text-muted-foreground"><Icon className="size-4" aria-hidden />{label}</div>
      <div className="mt-4 flex items-baseline gap-2 font-mono tabular-nums">
        <span className="text-3xl font-semibold tracking-tight">{value.toFixed(1)}</span>
        <span className="text-sm text-muted-foreground">{unit}</span>
      </div>
      {detail && <p className="mt-2 text-xs text-muted-foreground">{detail}</p>}
    </div>
  )
}
