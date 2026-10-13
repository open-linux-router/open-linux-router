import { useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useQosApply, useQosConfig, useQosStatus } from './queries'

export function QosLinkRates() {
  const config = useQosConfig()
  const status = useQosStatus()
  const apply = useQosApply()
  const [downloadDraft, setDownload] = useState<string | null>(null)
  const [uploadDraft, setUpload] = useState<string | null>(null)
  const [confirming, setConfirming] = useState(false)
  const download = downloadDraft ?? (config.data?.download_mbps ? String(config.data.download_mbps) : '')
  const upload = uploadDraft ?? (config.data?.upload_mbps ? String(config.data.upload_mbps) : '')
  const valid = [download, upload].every((value) => value === '' || (Number.isFinite(Number(value)) && Number(value) >= 0.001 && Number(value) <= 100000))
  async function save() {
    if (!config.data || !valid) return
    try {
      const result = await apply.mutateAsync({ ...config.data, download_mbps: Number(download), upload_mbps: Number(upload) })
      setConfirming(false)
      setDownload(null)
      setUpload(null)
      if (result.status.active) toast.success('Link rates applied')
      else toast.success('Link rates saved', { description: 'They apply when a device priority or speed limit is configured.' })
    } catch (error) {
      toast.error(String(error))
    }
  }
  return <Card>
    <CardHeader><CardTitle>Traffic priority</CardTitle></CardHeader>
    <CardContent className="space-y-4">
      <p className="text-sm text-muted-foreground">Device speed limits apply automatically. To prioritize traffic during Internet congestion, enter measured download and upload speeds slightly below your connection’s maximum. Leave blank for no link limit. Rates apply per managed LAN interface.</p>
      <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); setConfirming(true) }}>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-2"><Label htmlFor="qos-download">Download (Mbps)</Label><Input id="qos-download" type="number" min="0.001" max="100000" step="any" placeholder="Unlimited" value={download} onChange={(event) => setDownload(event.target.value)} /></div>
          <div className="space-y-2"><Label htmlFor="qos-upload">Upload (Mbps)</Label><Input id="qos-upload" type="number" min="0.001" max="100000" step="any" placeholder="Unlimited" value={upload} onChange={(event) => setUpload(event.target.value)} /></div>
        </div>
        <Button type="submit" disabled={!config.data || !valid || apply.isPending}>Save</Button>
      </form>
      {config.isError && <p className="text-sm text-destructive">Could not load traffic settings.</p>}
      {status.isError && <p className="text-sm text-destructive">Could not check traffic control.</p>}
      {status.data && <p className="text-sm text-muted-foreground">{status.data.active ? 'Device policies active.' : 'No active device policies.'} {status.data.reason} {status.data.pending?.length ? `${status.data.pending.length} devices pending.` : ''}</p>}
      <Dialog open={confirming} onOpenChange={setConfirming}><DialogContent>
        <DialogHeader><DialogTitle>Apply link rates?</DialogTitle><DialogDescription>Updates traffic queues for configured devices. Queued packets may briefly be interrupted.</DialogDescription></DialogHeader>
        <DialogFooter><Button variant="ghost" onClick={() => setConfirming(false)}>Cancel</Button><Button disabled={apply.isPending} onClick={() => void save()}>Apply</Button></DialogFooter>
      </DialogContent></Dialog>
    </CardContent>
  </Card>
}
