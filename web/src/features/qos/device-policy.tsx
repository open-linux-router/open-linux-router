import { useState, type ReactNode } from 'react'
import { toast } from 'sonner'
import { Meter } from '@/components/traffic-metrics'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useQosConfig, useQosDeviceApply, type Priority } from '@/features/qos/queries'
import { formatRate } from '@/lib/utils'

export function DevicePolicy({ mac, downRate, upRate, children }: { mac: string; downRate?: number; upRate?: number; children?: ReactNode }) {
  const config = useQosConfig()
  const apply = useQosDeviceApply(mac)
  const current = config.data?.devices?.find((d) => d.mac.toLowerCase() === mac.toLowerCase())
  const priority = current?.priority ?? 'normal'
  const [editing, setEditing] = useState<'download' | 'upload' | null>(null)
  const [limit, setLimit] = useState('')
  const valid = limit === '' || (Number.isFinite(Number(limit)) && Number(limit) > 0 && Number(limit) <= 100000)
  const busy = !config.data || apply.isPending

  async function save(nextPriority = priority) {
    if (!config.data) return
    try {
      const result = await apply.mutateAsync({ mac, priority: nextPriority,
        download_mbps: editing === 'download' ? Number(limit) : current?.download_mbps ?? 0,
        upload_mbps: editing === 'upload' ? Number(limit) : current?.upload_mbps ?? 0 })
      setEditing(null)
      const pending = result.status.pending?.includes(mac.toLowerCase())
      if (pending) toast.warning('Policy saved', { description: 'Waiting for the device’s LAN interface to be identified.' })
      else if (result.status.active) toast.success('Policy applied', { description: result.status.reason })
      else if (!result.config.devices?.some((d) => d.mac === mac.toLowerCase())) toast.success('Priority and limits reset')
      else toast.warning('Policy saved, but not active', { description: result.status.reason })
    } catch (error) {
      toast.error(String(error))
    }
  }

  function begin(direction: 'download' | 'upload') {
    setLimit(String((direction === 'download' ? current?.download_mbps : current?.upload_mbps) || ''))
    setEditing(direction)
  }

  return <section aria-label="Priority and traffic" className="space-y-3">
    <div className="flex min-h-11 flex-wrap items-center justify-between gap-3 text-sm sm:min-h-9">
      <span className="text-muted-foreground">Priority</span>
      <div role="group" aria-label="Device priority" className="flex rounded-lg bg-muted p-1">
        {(['high', 'normal', 'low'] as Priority[]).map((value) => <Button key={value} size="sm" variant={priority === value ? 'outline' : 'ghost'} aria-pressed={priority === value} disabled={busy} onClick={() => void save(value)}>{value[0].toUpperCase() + value.slice(1)}</Button>)}
      </div>
    </div>
    {config.isError && <p className="text-sm text-destructive">Could not load priority and limits.</p>}
    <div className="space-y-1">
      <Meter label="Download" arrow="↓" current={downRate === undefined ? '—' : formatRate(downRate)} maximum={current?.download_mbps ? String(current.download_mbps) + ' Mbps' : undefined} onClick={() => begin('download')} />
      <Meter label="Upload" arrow="↑" current={upRate === undefined ? '—' : formatRate(upRate)} maximum={current?.upload_mbps ? String(current.upload_mbps) + ' Mbps' : undefined} tone="amber" onClick={() => begin('upload')} />
    </div>
    {children}
    <Dialog open={!!editing} onOpenChange={(open) => { if (!open && !apply.isPending) setEditing(null) }}>
      <DialogContent>
        <DialogHeader><DialogTitle>{editing === 'download' ? 'Download limit' : 'Upload limit'}</DialogTitle><DialogDescription>Applies immediately to traffic crossing this router. Leave blank for unlimited.</DialogDescription></DialogHeader>
        <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); if (valid && !busy) void save() }}>
          <div className="space-y-2"><Label htmlFor="device-speed-limit">Limit (Mbps)</Label><Input id="device-speed-limit" autoFocus type="number" min="0.001" max="100000" step="any" value={limit} placeholder="Unlimited" disabled={busy} onChange={(event) => setLimit(event.target.value)} /></div>
          <DialogFooter><Button type="button" variant="ghost" disabled={apply.isPending} onClick={() => setEditing(null)}>Cancel</Button><Button type="submit" disabled={busy || !valid}>Save</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  </section>
}
