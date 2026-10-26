import { t, useLocale } from '@/lib/i18n'
import { Heading } from '@/components/ui/heading'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import encodeQR from '@paulmillr/qr'
import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { Settings } from 'lucide-react'
import { Switch } from '@/components/ui/switch'
import { ClientQueries } from '@/features/dns/client-queries'
import { SiteActivity } from '@/features/devices/site-activity'

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
  useLocale()
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
  // A reason the switch cannot be used, in the space the session would fill.
  const notice = status.isError || Boolean(active && !mine) || (!active && (ipv4.length === 0 || ipv4.length > 8))

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

  return <section className="overflow-hidden rounded-xl border bg-card shadow-xs [overflow-anchor:none]" aria-labelledby="inspection-title">
    <div className="px-4 sm:px-6">
      <div className="flex h-18 items-center justify-between gap-3">
        <div>
          <Heading id="inspection-title">{t('Connections')}</Heading>
        </div>
        {/* The label and switch come last, so their trailing edge is pinned to
            the card's right edge: the setup gear and the pending note join them
            from the left without moving the control the operator came for. */}
        <div className="flex flex-wrap items-center justify-end gap-3">
          {status.isPending && <span className="text-xs text-muted-foreground">{t('Checking…')}</span>}
          {mine && <Button variant="quiet" size="icon" aria-label={t('Inspection setup')} title={t('Inspection setup')} onClick={() => setShowSetup(true)}><Settings className="size-5" /></Button>}
          <label htmlFor="device-inspection" className="text-sm">{t('Inspection')}</label>
          <Switch id="device-inspection" checked={Boolean(mine)} busy={start.isPending || stop.isPending} disabled={status.isPending || status.isError || Boolean(active && !mine) || (!mine && (ipv4.length === 0 || ipv4.length > 8))} onCheckedChange={(checked) => { if (checked) start.mutate(); else stop.mutate() }} />
        </div>
      </div>
    </div>
    <div>
      <Dialog open={showSetup} onOpenChange={setShowSetup}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t('Inspection setup')}</DialogTitle>
            <DialogDescription>Scan on the device you are inspecting to open the five-step certificate setup guide. Connect it to a network that can reach this router.</DialogDescription>
          </DialogHeader>
          {caQR && <div className="mx-auto rounded-md bg-white p-3 [&>svg]:size-56" dangerouslySetInnerHTML={{ __html: caQR }} role="img" aria-label={t('Inspection setup guide QR code')} />}
          <a href={caURL} target="_blank" rel="noreferrer" className="break-all text-sm text-primary underline">{t('Open setup guide')}</a>
        </DialogContent>
      </Dialog>
      {/* Everything the switch turned on lives here, so there is one card, one
          switch and one answer to "is this device being inspected". One divider
          per section, drawn full width by the section itself: the chart is the
          week before this session, DNS is where the names in it came from, and
          the last section is the content the session can see. */}
      {notice && <div className="space-y-3 border-t px-4 py-4 sm:px-6 sm:py-6">
        {status.isError ? <p role="alert" className="text-sm text-destructive">{t('Inspection status is unavailable. Do not assume interception has stopped.')}</p> : null}
        {active && !mine ? <p className="text-sm text-muted-foreground">{t('Another device is being inspected. Stop that session before starting this one.')}</p> : null}
        {ipv4.length === 0 && !active && <p className="text-sm text-muted-foreground">OLR has not observed an IPv4 address for this device yet. Connect it to the network and refresh this page to enable inspection.</p>}
        {ipv4.length > 8 && !active && <p className="text-sm text-muted-foreground">{t('Too many addresses are associated with this device to start inspection safely.')}</p>}
      </div>}
      {mine && <div className="border-t">
        <h3 className="px-4 pt-4 text-sm font-medium sm:px-6">{t('Activity')}</h3>
        <SiteActivity addresses={ipv4} />
      </div>}
      {mine && <div className="border-t"><ClientQueries addresses={ipv4} section /></div>}
      {mine && <div className="border-t pt-4">
        <h3 className="px-4 pb-3 text-sm font-medium sm:px-6">{t('Requests')}</h3>
        <div className="px-4 pb-4 sm:px-6 sm:pb-6">
          {viewerReady
            ? <iframe title={t('Full request and response inspection')} src="/inspection-web/" className="h-[75vh] min-h-[480px] w-full rounded border bg-white" />
            : <div className="text-sm text-muted-foreground">{t('Opening the inspection viewer…')}</div>}
        </div>
      </div>}
    </div>
  </section>
}
