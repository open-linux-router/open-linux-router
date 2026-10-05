import { useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useQosConfig, useQosDeviceApply, useQosStatus, type Priority } from '@/features/qos/queries'

export function DevicePolicy({ mac }: { mac: string }) {
  const config = useQosConfig()
  const status = useQosStatus()
  const apply = useQosDeviceApply(mac)
  const current = config.data?.devices?.find((d) => d.mac.toLowerCase() === mac.toLowerCase())
  const [draft, setDraft] = useState<{ priority: Priority; down: string; up: string } | null>(null)
  const priority = draft?.priority ?? current?.priority ?? 'normal'
  const down = draft?.down ?? String(current?.download_mbps || '')
  const up = draft?.up ?? String(current?.upload_mbps || '')
  const valid = (s: string) => s === '' || (Number.isFinite(Number(s)) && Number(s) > 0 && Number(s) <= 100000)
  const change = (next: Partial<NonNullable<typeof draft>>) => setDraft({ priority, down, up, ...next })
  const pending = status.data?.pending?.some((m) => m.toLowerCase() === mac.toLowerCase())

  async function save() {
    try {
      await apply.mutateAsync({ mac, priority, download_mbps: down ? Number(down) : 0, upload_mbps: up ? Number(up) : 0 })
      setDraft(null)
      toast.warning('Policy saved, but not active', { description: 'Enforcement is not available yet.' })
    } catch (error) {
      toast.error(String(error))
    }
  }

  return <Card>
    <CardHeader><CardTitle>Network priority and speed limit</CardTitle></CardHeader>
    <CardContent className="space-y-4">
      <p className="text-sm text-muted-foreground">Priority matters only when the managed connection is busy. Limits are maximum speeds, not guaranteed speeds.</p>
      <div className="grid gap-4 sm:grid-cols-3">
        <div className="grid gap-2"><Label htmlFor="device-qos-priority">Priority</Label><Select value={priority} disabled={!config.data || apply.isPending} onValueChange={(v) => { if (v) change({ priority: v as Priority }) }}><SelectTrigger id="device-qos-priority" className="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="high">High</SelectItem><SelectItem value="normal">Normal</SelectItem><SelectItem value="low">Low</SelectItem></SelectContent></Select></div>
        <div className="grid gap-2"><Label htmlFor="device-qos-down">Download limit (Mbps)</Label><Input id="device-qos-down" type="number" min="0.001" max="100000" step="any" value={down} placeholder="Unlimited" onChange={(e) => change({ down: e.target.value })} /></div>
        <div className="grid gap-2"><Label htmlFor="device-qos-up">Upload limit (Mbps)</Label><Input id="device-qos-up" type="number" min="0.001" max="100000" step="any" value={up} placeholder="Unlimited" onChange={(e) => change({ up: e.target.value })} /></div>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-xs text-muted-foreground">{config.isError || status.isError ? 'QoS status unavailable.' : !status.data ? 'Checking QoS status…' : !status.data.enabled ? 'Not active — enforcement is not available yet.' : !status.data.active ? `Not active — ${status.data.reason || 'kernel state not verified'}` : pending ? 'Pending — no unambiguous address for this device.' : 'QoS active on this router.'}</p>
        <div className="flex gap-2"><Button size="sm" variant="ghost" disabled={!draft || apply.isPending} onClick={() => setDraft(null)}>Cancel</Button><Button size="sm" disabled={!draft || !valid(down) || !valid(up) || apply.isPending} onClick={() => void save()}>Save policy</Button></div>
      </div>
      <p className="text-xs text-muted-foreground">Enforcement is not available in this build. Settings can be saved but do not currently limit or prioritize packets.</p>
    </CardContent>
  </Card>
}
