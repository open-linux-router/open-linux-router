import { useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
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
      toast.warning('Configuration saved, not active', { description: 'Enforcement is not available yet.' })
    } catch (error) {
      toast.error(String(error))
    }
  }

  return <section aria-label="Priority and limits" className="mt-4 border-t pt-3">
      <div className="text-sm [&_input]:text-right [&_[data-slot=select-value]]:justify-end [&_[data-slot=select-value]]:text-right">
        <div className="grid min-h-11 sm:min-h-9 grid-cols-[6rem_minmax(0,1fr)] items-center gap-3 py-1"><Label htmlFor="device-qos-priority" className="text-muted-foreground font-normal">Priority</Label><Select value={priority} disabled={!config.data || apply.isPending} onValueChange={(v) => { if (v) change({ priority: v as Priority }) }}><SelectTrigger id="device-qos-priority" className="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="high">High</SelectItem><SelectItem value="normal">Normal</SelectItem><SelectItem value="low">Low</SelectItem></SelectContent></Select></div>
        <div className="grid min-h-11 sm:min-h-9 grid-cols-[6rem_minmax(0,1fr)] items-center gap-3 py-1"><Label htmlFor="device-qos-down" className="text-muted-foreground font-normal">Download limit</Label><div className="flex items-center gap-2"><Input id="device-qos-down" type="number" min="0.001" max="100000" step="any" value={down} placeholder="Unlimited" disabled={!config.data || apply.isPending} onChange={(e) => change({ down: e.target.value })} /><span className="text-xs text-muted-foreground">Mbps</span></div></div>
        <div className="grid min-h-11 sm:min-h-9 grid-cols-[6rem_minmax(0,1fr)] items-center gap-3 py-1"><Label htmlFor="device-qos-up" className="text-muted-foreground font-normal">Upload limit</Label><div className="flex items-center gap-2"><Input id="device-qos-up" type="number" min="0.001" max="100000" step="any" value={up} placeholder="Unlimited" disabled={!config.data || apply.isPending} onChange={(e) => change({ up: e.target.value })} /><span className="text-xs text-muted-foreground">Mbps</span></div></div>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-xs text-muted-foreground">{config.isError || status.isError ? 'QoS status unavailable.' : !status.data ? 'Checking QoS status…' : !status.data.enabled ? 'Not active' : !status.data.active ? `Not active — ${status.data.reason || 'kernel state not verified'}` : pending ? 'Pending — no unambiguous address for this device.' : 'QoS active on this router.'}</p>
        {draft && <div className="flex gap-2"><Button size="sm" variant="ghost" disabled={apply.isPending} onClick={() => setDraft(null)}>Cancel</Button><Button size="sm" disabled={!valid(down) || !valid(up) || apply.isPending} onClick={() => void save()}>Save configuration</Button></div>}
      </div>
  </section>
}
