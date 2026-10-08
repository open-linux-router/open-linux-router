import { Activity, ArrowDown, ArrowUp, Gauge, LoaderCircle } from 'lucide-react'
import { useState } from 'react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'

interface SpeedResult {
  server: string
  location: string
  latency_ms: number
  jitter_ms: number
  download_mbps: number
  upload_mbps: number
}

export function ToolsPage() {
  const [result, setResult] = useState<SpeedResult | null>(null)
  const [running, setRunning] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function run() {
    setRunning(true)
    setResult(null)
    setError(null)
    try {
      setResult(await api.post<SpeedResult>('/api/tools/speedtest'))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Speed test failed')
    } finally {
      setRunning(false)
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
          {running && <p role="status" className="text-sm text-muted-foreground">Finding a server, then measuring ping, download and upload. This can take up to a minute.</p>}
        </div>
      </div>

      {error && (
        <Alert variant="destructive">
          <AlertTitle>Could not complete the test</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
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
      <p className="text-xs text-muted-foreground">More network tools, including ping and traceroute, will appear here in the future.</p>
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
