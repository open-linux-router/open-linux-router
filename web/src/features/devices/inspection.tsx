import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import encodeQR from '@paulmillr/qr'
import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { Settings } from 'lucide-react'
import { Switch } from '@/components/ui/switch'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { api } from '@/lib/api'

interface Status {
  active: boolean
  mac?: string
  ips?: string[]
  expires?: string
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
  const [showSetup, setShowSetup] = useState(false)
  const [viewerReady, setViewerReady] = useState(false)
  const caURL = `${window.location.origin}/inspection-setup.html`
  const caQR = useMemo(() => {
    try { return encodeQR(caURL, 'svg') } catch { return null }
  }, [caURL])
  const active = status.data?.active
  const mine = active && status.data?.mac?.toLowerCase() === mac.toLowerCase()

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
        <div className="flex flex-wrap items-center justify-end gap-3">
          <label htmlFor="device-inspection" className="text-sm">Inspection</label>
          <Switch id="device-inspection" checked={Boolean(mine)} disabled={start.isPending || stop.isPending || status.isPending || status.isError || Boolean(active && !mine) || (!mine && (ipv4.length === 0 || ipv4.length > 8))} onCheckedChange={(checked) => { if (checked) start.mutate(); else stop.mutate() }} />
          {mine && <Button variant="ghost" size="icon" aria-label="Inspection setup" title="Inspection setup" onClick={() => setShowSetup(true)}><Settings className="size-5" /></Button>}
          {status.isPending && <span className="text-xs text-muted-foreground">Checking…</span>}
        </div>
      </div>
    </div>
    <div className="space-y-3 p-4 sm:px-6">
      {status.isError ? <p role="alert" className="text-sm text-destructive">Inspection status is unavailable. Do not assume interception has stopped.</p> : null}
      {active && !mine ? <p className="text-sm text-muted-foreground">Another device is being inspected. Stop that session before starting this one.</p> : null}
      {ipv4.length === 0 && !active && <p className="text-sm text-muted-foreground">OLR has not observed an IPv4 address for this device yet. Connect it to the network and refresh this page to enable inspection.</p>}
      {ipv4.length > 8 && !active && <p className="text-sm text-muted-foreground">Too many addresses are associated with this device to start inspection safely.</p>}
      <Dialog open={showSetup} onOpenChange={setShowSetup}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Inspection setup</DialogTitle>
            <DialogDescription>Scan on the device you are inspecting to open the five-step certificate setup guide. Connect it to a network that can reach this router.</DialogDescription>
          </DialogHeader>
          {caQR && <div className="mx-auto rounded-md bg-white p-3 [&>svg]:size-56" dangerouslySetInnerHTML={{ __html: caQR }} role="img" aria-label="Inspection setup guide QR code" />}
          <a href={caURL} target="_blank" rel="noreferrer" className="break-all text-sm text-primary underline">Open setup guide</a>
        </DialogContent>
      </Dialog>
      {mine && (viewerReady
        ? <iframe title="Full request and response inspection" src="/inspection-web/" className="h-[75vh] min-h-[480px] w-full rounded border bg-white" />
        : <p className="text-sm text-muted-foreground">Opening the inspection viewer…</p>)}
    </div>
  </section>
}
