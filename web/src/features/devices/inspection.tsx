import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import encodeQR from '@paulmillr/qr'
import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { QrCode } from 'lucide-react'
import { Switch } from '@/components/ui/switch'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Disclosure } from '@/components/ui/disclosure'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
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
  ips?: string[]
  expires?: string
  events: Event[]
  ca_present: boolean
  ca_sha256?: string
  redirected_packets?: number
  proxy_accepted: number
  exit_routed: boolean
}

export function DeviceInspection({ mac, addresses }: { mac: string; addresses: string[] }) {
  const client = useQueryClient()
  const ipv4 = addresses.filter((address) => /^\d+\.\d+\.\d+\.\d+$/.test(address))
  const status = useQuery({
    queryKey: ['inspection', 'status'],
    queryFn: () => api.get<Status>('/api/inspection/status'),
    refetchInterval: 2000,
  })
  const start = useMutation({
    mutationFn: () => api.post<Status>('/api/inspection/session', { mac, ips: ipv4 }),
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
  const [preparing, setPreparing] = useState(false)
  const [viewerReady, setViewerReady] = useState(false)
  const caURL = `${window.location.origin}/inspection-setup.html`
  const caQR = useMemo(() => {
    try { return encodeQR(caURL, 'svg') } catch { return null }
  }, [caURL])
  const active = status.data?.active
  const mine = active && status.data?.mac?.toLowerCase() === mac.toLowerCase()
  const requests = mine ? status.data?.events.filter((e) => e.kind === 'request') ?? [] : []
  const failed = mine ? status.data?.events.filter((e) => e.kind === 'failed') ?? [] : []

  useEffect(() => {
    if (!mine) return
    let cancelled = false
    api.post('/api/inspection/viewer-access').then(() => {
      if (!cancelled) setViewerReady(true)
    }).catch((error) => {
      if (!cancelled) toast.error(`Could not open full inspection viewer: ${String(error)}`)
    })
    return () => { cancelled = true; setViewerReady(false) }
  }, [mine, status.data?.expires])

  return <section className="overflow-hidden rounded-2xl border bg-card" aria-labelledby="inspection-title">
    <div className="px-4 pt-4 sm:px-6">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b pb-3">
        <div>
          <h2 id="inspection-title" className="text-base font-semibold">Connections</h2>
        </div>
        <div className="flex items-center gap-3">
          <label htmlFor="device-inspection" className="text-sm">Inspection</label>
          <Switch id="device-inspection" checked={Boolean(mine)} disabled={start.isPending || stop.isPending || status.isPending || status.isError || Boolean(active && !mine) || (!mine && (ipv4.length === 0 || ipv4.length > 8))} onCheckedChange={(checked) => { if (checked) setPreparing(true); else stop.mutate() }} />
          {mine && <Button variant="ghost" size="icon" aria-label="Inspection setup QR code" onClick={() => setShowQR(true)}><QrCode className="size-5" /></Button>}
          {status.isPending && <span className="text-xs text-muted-foreground">Checking…</span>}
        </div>
      </div>
    </div>
    <div className="space-y-3 p-4 sm:px-6">
      {status.isError ? <p role="alert" className="text-sm text-destructive">Inspection status is unavailable. Do not assume interception has stopped.</p> : null}
      {active && !mine ? <p className="text-sm text-muted-foreground">Another device is being inspected. Stop that session before starting this one.</p> : null}
      {ipv4.length === 0 && !active && <p className="text-sm text-muted-foreground">OLR has not observed an IPv4 address for this device yet. Connect it to the network and refresh this page to enable inspection.</p>}
      {ipv4.length > 8 && !active && <p className="text-sm text-muted-foreground">Too many addresses are associated with this device to start inspection safely.</p>}
      {mine && <p className="text-sm">{status.data?.expires ? `Stops at ${new Date(status.data.expires).toLocaleTimeString()}` : 'Stop time unavailable'} · Upstream {status.data?.exit_routed ? 'via device exit' : 'via router default'}</p>}
      <Dialog open={preparing} onOpenChange={setPreparing}>
        <DialogContent>
          <DialogHeader><DialogTitle>Start device inspection?</DialogTitle><DialogDescription>Captured requests and bodies may contain credentials. Inspection stops after 15 minutes.</DialogDescription></DialogHeader>
          <div className="space-y-3 text-sm">
            <p>Install mitmproxy (mitmweb) on this router first. Only supported IPv4 HTTP/HTTPS traffic is intercepted.</p>
            <p>For HTTPS, explicitly trust the public CA on this device after starting. Remove that trust when finished; stopping inspection does not remove it.</p>
            <p>The private CA key stays on the router. Session data is cleared on stop.</p>
          </div>
          <DialogFooter><Button variant="outline" onClick={() => setPreparing(false)}>Cancel</Button><Button disabled={start.isPending || status.isError || status.isPending || Boolean(active)} onClick={() => { setPreparing(false); start.mutate() }}>Start inspection</Button></DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog open={showQR} onOpenChange={setShowQR}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Inspection setup</DialogTitle>
            <DialogDescription>Scan on the device you are inspecting to open the five-step certificate setup guide. Connect it to a network that can reach this router.</DialogDescription>
          </DialogHeader>
          {caQR && <div className="mx-auto rounded-md bg-white p-3 [&>svg]:size-56" dangerouslySetInnerHTML={{ __html: caQR }} role="img" aria-label="Inspection setup guide QR code" />}
          <a href={caURL} target="_blank" rel="noreferrer" className="break-all text-sm text-primary underline">Open setup guide</a>
        </DialogContent>
      </Dialog>
      {mine && <p className="text-xs text-muted-foreground">
        Diagnostic: {status.data?.redirected_packets ?? 'unavailable'} redirected TCP packets · {status.data?.proxy_accepted ?? 0} proxy connections accepted. These are session totals, not a complete connection history. If the first count stays at zero after an IPv4 HTTP test, traffic did not match the redirect; if it grows but the second stays at zero, it did not reach mitmdump.
      </p>}
      {mine && <div className="grid gap-4 lg:grid-cols-2">
        <div className="space-y-2">
          <h3 className="text-sm font-semibold">Inspected requests <Badge variant="secondary">{requests.length}</Badge></h3>
          <ul className="max-h-80 space-y-1 overflow-y-auto">
            {requests.length === 0 && <li className="p-3 text-sm text-muted-foreground">No supported requests captured yet.</li>}
            {requests.map((event, i) => <li key={`${event.at}-${i}`}><button className="w-full p-3 text-left text-sm hover:bg-muted/50" onClick={() => setSelected(event)}>
              <span className="font-mono text-xs text-muted-foreground">{new Date(event.at * 1000).toLocaleTimeString()} · {event.method} · {event.status}</span>
              <span className="block break-all">{event.url}</span>
            </button></li>)}
          </ul>
        </div>
        <div className="space-y-2">
          <h3 className="text-sm font-semibold">Could not inspect <Badge variant="secondary">{failed.length}</Badge></h3>
          <ul className="max-h-80 space-y-1 overflow-y-auto">
            {failed.length === 0 && <li className="p-3 text-sm text-muted-foreground">No proxy failures reported. Other connections are not tracked here.</li>}
            {failed.map((event, i) => <li key={`${event.at}-${i}`} className="p-3 text-sm">
              <span className="block break-all">{event.target}</span><span className="text-xs text-muted-foreground">{event.reason}</span>
            </li>)}
          </ul>
        </div>
      </div>}
      {mine && <Disclosure summary="Full request and response viewer">
        <div className="space-y-3 rounded-lg border p-3 text-sm">
        <p className="font-medium">Full request and response inspection</p>
        {viewerReady ? <iframe title="Full request and response inspection" src="/inspection-web/" className="h-[75vh] min-h-[480px] w-full rounded border bg-white" />
          : <p className="text-muted-foreground">Opening the inspection viewer…</p>}
      </div>
      </Disclosure>}
      {mine && selected && <div className="space-y-2 rounded-xl border p-4 text-sm">
        <div className="flex justify-between gap-2"><h3 className="font-medium">Request details</h3><Button variant="ghost" size="sm" onClick={() => setSelected(null)}>Close</Button></div>
        <p className="break-all">{selected.method} {selected.url} · {selected.status}</p>
        <p className="text-xs text-muted-foreground">Headers may contain credentials. They are held only in memory for this session.</p>
        <h4 className="font-medium">Headers</h4>
        <pre className="max-h-52 overflow-auto rounded-lg bg-muted p-3 text-xs">{JSON.stringify({ request: selected.request_headers, response: selected.response_headers }, null, 2)}</pre>
        <p className="text-xs text-muted-foreground">For full request and response bodies, open the built-in mitmweb viewer while inspection is active.</p>
      </div>}
    </div>
  </section>
}
