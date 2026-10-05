import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import encodeQR from '@paulmillr/qr'
import { useMemo, useState } from 'react'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { api } from '@/lib/api'

interface Event {
  kind: 'request' | 'failed'
  at: number
  method?: string
  url?: string
  status?: number
  target?: string
  reason?: string
  request_headers?: Record<string, string>
  response_headers?: Record<string, string>
}
interface Status {
  active: boolean
  mac?: string
  expires?: string
  events: Event[]
  ca_present: boolean
}

export function DeviceInspection({ mac }: { mac: string }) {
  const client = useQueryClient()
  const status = useQuery({
    queryKey: ['inspection', 'status'],
    queryFn: () => api.get<Status>('/api/inspection/status'),
    refetchInterval: 2000,
  })
  const start = useMutation({
    mutationFn: () => api.post<Status>('/api/inspection/session', { mac }),
    onSuccess: () => client.invalidateQueries({ queryKey: ['inspection'] }),
    onError: (error) => toast.error(String(error)),
  })
  const stop = useMutation({
    mutationFn: () => api.send<Status>('DELETE', '/api/inspection/session'),
    onSuccess: () => client.invalidateQueries({ queryKey: ['inspection'] }),
    onError: (error) => toast.error(String(error)),
  })
  const [selected, setSelected] = useState<Event | null>(null)
  const [showQR, setShowQR] = useState(false)
  const caURL = `${window.location.origin}/download/inspection-ca.crt`
  const caQR = useMemo(() => {
    try { return encodeQR(caURL, 'svg') } catch { return null }
  }, [caURL])
  const active = status.data?.active
  const mine = active && status.data?.mac?.toLowerCase() === mac.toLowerCase()
  const requests = mine ? status.data?.events.filter((e) => e.kind === 'request') ?? [] : []
  const failed = mine ? status.data?.events.filter((e) => e.kind === 'failed') ?? [] : []

  async function downloadCA() {
    try {
      const { pem } = await api.get<{ pem: string }>('/api/inspection/ca')
      const url = URL.createObjectURL(new Blob([pem], { type: 'application/x-pem-file' }))
      const link = document.createElement('a')
      link.href = url
      link.download = 'olr-debugging-ca.pem'
      link.click()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
    } catch (error) { toast.error(String(error)) }
  }

  return <section className="overflow-hidden rounded-2xl border bg-card" aria-labelledby="inspection-title">
    <div className="border-b bg-muted/40 px-5 py-5 sm:px-7">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="mb-1 text-xs font-semibold uppercase tracking-widest text-muted-foreground">Advanced · private by default</p>
          <h2 id="inspection-title" className="text-xl font-semibold tracking-tight">Inspect network requests</h2>
        </div>
        <Badge variant={mine ? 'warning' : 'outline'}>{mine ? 'On' : 'Off'}</Badge>
      </div>
      <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
        Turn inspection on or off for this device. It automatically turns off after 15 minutes as a safety limit. Request URLs and headers can contain private data. Bodies are not collected. Nothing is saved to disk.
      </p>
    </div>
    <div className="space-y-5 p-5 sm:p-7">
      {status.isError ? <p role="alert" className="text-sm text-destructive">Inspection status is unavailable. Do not assume interception has stopped.</p> : null}
      {active && !mine ? <p className="text-sm text-muted-foreground">Another device is being inspected. Stop that session before starting this one.</p> : null}
      <div className="grid gap-4 text-sm text-muted-foreground md:grid-cols-3">
        <p><strong className="block text-foreground">1. Prepare</strong>Install mitmproxy (mitmdump) on the router. Inspection uses the device's currently observed, uniquely owned private IPv4 addresses. IPv6 traffic is not intercepted.</p>
        <p><strong className="block text-foreground">2. Trust CA for HTTPS</strong>Start once to generate the CA, then install and explicitly trust the downloaded certificate on your device. Do not install its private key. HTTP works without a CA.</p>
        <p><strong className="block text-foreground">3. Stop and remove trust</strong>Stopping or timeout clears the session and restores normal forwarding. Your device still trusts the CA until you remove it in device settings.</p>
      </div>
      <div className="flex flex-wrap gap-2">
        {mine ? <Button variant="destructive" disabled={stop.isPending} onClick={() => stop.mutate()}>Turn off</Button>
          : <Button disabled={Boolean(active) || start.isPending || status.isPending || status.isError} onClick={() => start.mutate()}>Turn on</Button>}
        <Button variant="outline" disabled={!status.data?.ca_present} onClick={downloadCA}>Download public CA</Button>
        <Button variant="outline" disabled={!status.data?.ca_present} onClick={() => setShowQR(true)}>Show CA QR</Button>
      </div>
      <p className="text-xs leading-relaxed text-muted-foreground">
        TCP ports 80/443 to public destinations only. QUIC/HTTP/3 (UDP/443), IPv6, non-HTTP traffic, pinned certificates and apps that reject user CAs are not inspected. No UDP blocking is applied. An empty list does not mean no connections occurred. The CA private key stays on this router.
      </p>
      <Dialog open={showQR} onOpenChange={setShowQR}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Download public CA on a phone</DialogTitle>
            <DialogDescription>Scan with your phone on a network that can reach this router. Install and explicitly trust the certificate for HTTPS inspection. Remove that trust when finished.</DialogDescription>
          </DialogHeader>
          {caQR && <div className="mx-auto rounded-md bg-white p-3 [&>svg]:size-56" dangerouslySetInnerHTML={{ __html: caQR }} role="img" aria-label="Public CA download QR code" />}
          <p className="break-all text-xs text-muted-foreground">{caURL}</p>
          <p className="text-xs text-muted-foreground">This link serves only the public certificate, without an admin token. Anyone with the link can download it; never scan a QR from an untrusted router.</p>
        </DialogContent>
      </Dialog>
      {mine && <div className="grid gap-4 lg:grid-cols-2">
        <div className="space-y-2">
          <h3 className="text-sm font-semibold">Inspected requests <Badge variant="secondary">{requests.length}</Badge></h3>
          <ul className="max-h-80 divide-y overflow-y-auto rounded-xl border">
            {requests.length === 0 && <li className="p-3 text-sm text-muted-foreground">No supported requests captured yet.</li>}
            {requests.map((event, i) => <li key={`${event.at}-${i}`}><button className="w-full p-3 text-left text-sm hover:bg-muted/50" onClick={() => setSelected(event)}>
              <span className="font-mono text-xs text-muted-foreground">{new Date(event.at * 1000).toLocaleTimeString()} · {event.method} · {event.status}</span>
              <span className="block break-all">{event.url}</span>
            </button></li>)}
          </ul>
        </div>
        <div className="space-y-2">
          <h3 className="text-sm font-semibold">Could not inspect <Badge variant="secondary">{failed.length}</Badge></h3>
          <ul className="max-h-80 divide-y overflow-y-auto rounded-xl border">
            {failed.length === 0 && <li className="p-3 text-sm text-muted-foreground">No proxy failures reported. Other connections are not tracked here.</li>}
            {failed.map((event, i) => <li key={`${event.at}-${i}`} className="p-3 text-sm">
              <span className="block break-all">{event.target}</span><span className="text-xs text-muted-foreground">{event.reason}</span>
            </li>)}
          </ul>
        </div>
      </div>}
      {mine && selected && <div className="space-y-2 rounded-xl border p-4 text-sm">
        <div className="flex justify-between gap-2"><h3 className="font-medium">Request details</h3><Button variant="ghost" size="sm" onClick={() => setSelected(null)}>Close</Button></div>
        <p className="break-all">{selected.method} {selected.url} · {selected.status}</p>
        <p className="text-xs text-muted-foreground">Headers may contain credentials. They are held only in memory for this session.</p>
        <pre className="max-h-52 overflow-auto rounded-lg bg-muted p-3 text-xs">{JSON.stringify({ request: selected.request_headers, response: selected.response_headers }, null, 2)}</pre>
      </div>}
    </div>
  </section>
}
