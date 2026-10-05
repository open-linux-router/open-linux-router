import { useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { CategoryPicker, type IconChoice } from '@/features/devices/category-picker'
import { groupOptions } from '@/features/devices/group-tree'
import { useApplyDevicesConfig, useDevicesConfig } from '@/features/devices/queries'
import { ApplyOutcome as DhcpOutcome } from '@/features/dhcp/editor'
import { useDhcpConfig } from '@/features/dhcp/queries'
import { useDhcpApply } from '@/features/dhcp/use-apply'
import { ApplyOutcome as GatewayOutcome } from '@/features/gateway/editor'
import { gatewayChange, useGatewayConfig } from '@/features/gateway/queries'
import { useGatewayApply } from '@/features/gateway/use-apply'
import { ApiError } from '@/lib/api'
import type { DeviceRow } from '@/lib/api-types'
import type { Device, DevicesConfig, Reservation } from '@/lib/config-types'

const NO_GROUP = ' none'
const INHERIT_EXIT = ' inherit'
const DIRECT_EXIT = ' direct'

export function DeviceInlineEditor({ device }: { device: DeviceRow }) {
  const identity = useDevicesConfig()
  const saveIdentity = useApplyDevicesConfig()
  const dhcp = useDhcpConfig()
  const dhcpApplier = useDhcpApply()
  const gateway = useGatewayConfig()
  const gatewayApplier = useGatewayApply()
  const reservation = dhcp.data?.reservations?.find((r) => r.mac.toLowerCase() === device.mac.toLowerCase())
  const override = gateway.data?.devices?.find((r) => r.mac.toLowerCase() === device.mac.toLowerCase())
  const exit = override ? override.exit || DIRECT_EXIT : INHERIT_EXIT
  const [name, setName] = useState(device.name_origin === 'operator' ? device.name : '')
  const [look, setLook] = useState<IconChoice>({ category: device.category_origin === 'operator' ? device.category : '', icon: device.icon ?? '' })
  const [group, setGroup] = useState(device.group ?? '')
  const [notes, setNotes] = useState(device.notes ?? '')
  const [ipDraft, setIp] = useState<string | null>(null)
  const [hostnameDraft, setHostname] = useState<string | null>(null)
  const ip = ipDraft ?? reservation?.ip ?? ''
  const hostname = hostnameDraft ?? reservation?.hostname ?? ''

  async function save() {
    if (!identity.data) return
    const entry: Device = {
      mac: device.mac,
      name: name.trim() || undefined,
      category: look.category || undefined,
      icon: look.icon || undefined,
      group: group || undefined,
      notes: notes.trim() || undefined,
      model: device.model || undefined,
    }
    const rest = (identity.data.devices ?? []).filter((d) => d.mac.toLowerCase() !== device.mac.toLowerCase())
    const empty = !entry.name && !entry.category && !entry.icon && !entry.group && !entry.notes && !entry.model
    const next: DevicesConfig = { ...identity.data, devices: empty ? rest : [...rest, entry] }
    try {
      await saveIdentity.mutateAsync(next)
      toast.success('Device saved')
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : String(error))
    }
  }

  function saveReservation(remove = false) {
    if (!dhcp.data) return
    const rest = (dhcp.data.reservations ?? []).filter((r) => r.mac.toLowerCase() !== device.mac.toLowerCase())
    const entry: Reservation = { ...reservation, mac: device.mac, ip: ip.trim(), hostname: hostname.trim() || undefined }
    void dhcpApplier.submit({ ...dhcp.data, reservations: remove ? rest : [...rest, entry] }).then((applied) => {
      if (applied) { setIp(null); setHostname(null) }
    })
  }

  return <section className="rounded-2xl border bg-card p-5 sm:p-7" aria-labelledby="device-settings-title">
    <h2 id="device-settings-title" className="text-lg font-semibold">Device settings</h2>
    <p className="mt-1 text-sm text-muted-foreground">Your choices are separate from the network observations above.</p>
    <div className="mt-5 grid gap-6 md:grid-cols-2">
      <div className="space-y-4">
        <div className="grid gap-2"><Label htmlFor="device-name">Name</Label><Input id="device-name" value={name} onChange={(e) => setName(e.target.value)} placeholder={device.hostname || device.mac} /></div>
        <div className="grid gap-2"><Label htmlFor="device-group">Group</Label>
          <Select value={group || NO_GROUP} onValueChange={(v) => setGroup(!v || v === NO_GROUP ? '' : v)}>
            <SelectTrigger id="device-group" className="w-full"><SelectValue>{(v: string) => v === NO_GROUP || !v ? 'None' : v}</SelectValue></SelectTrigger>
            <SelectContent><SelectItem value={NO_GROUP}>None</SelectItem>{groupOptions(identity.data?.groups ?? []).map((g) => <SelectItem key={g.name} value={g.name}><span style={{ paddingLeft: g.depth * 12 }}>{g.name}</span></SelectItem>)}</SelectContent>
          </Select>
        </div>
        <div className="grid gap-2"><Label htmlFor="device-notes">Notes</Label><Textarea id="device-notes" rows={2} value={notes} onChange={(e) => setNotes(e.target.value)} /></div>
        <div className="grid gap-2"><Label>Icon</Label><CategoryPicker value={look} detected={device.detected_category} onChange={setLook} /></div>
        <Button disabled={identity.isPending || identity.isError || saveIdentity.isPending} onClick={save}>Save device</Button>
        {identity.isError && <p role="alert" className="text-sm text-destructive">Device settings are unavailable.</p>}
      </div>
      <div className="space-y-6">
        <div className="space-y-3 rounded-xl border p-4">
          <h3 className="font-medium">Fixed address</h3>
          <p className="text-xs text-muted-foreground">Reserve an address for {device.mac}. Changes may affect its DHCP lease.</p>
          <div className="grid gap-2"><Label htmlFor="device-ip">IP address</Label><Input id="device-ip" value={ip} onChange={(e) => setIp(e.target.value)} placeholder="192.168.1.50" /></div>
          <div className="grid gap-2"><Label htmlFor="device-hostname">Hostname (optional)</Label><Input id="device-hostname" value={hostname} onChange={(e) => setHostname(e.target.value)} /></div>
          <div className="flex flex-wrap gap-2"><Button disabled={!ip.trim() || dhcp.isPending || dhcp.isError || dhcpApplier.busy} onClick={() => saveReservation()}>{reservation ? 'Save reservation' : 'Reserve address'}</Button>
            {reservation && <Button variant="outline" disabled={dhcpApplier.busy} onClick={() => saveReservation(true)}>Remove reservation</Button>}</div>
          {dhcp.isError && <p role="alert" className="text-sm text-destructive">DHCP settings are unavailable.</p>}
        </div>
        <div className="space-y-2 rounded-xl border p-4">
          <Label htmlFor="device-exit">Internet via</Label>
          <Select value={exit} disabled={!gateway.data || gatewayApplier.busy} onValueChange={(value) => {
            if (!value) return
            gatewayApplier.submit(value === INHERIT_EXIT ? gatewayChange.removeDevice(device.mac) : gatewayChange.device(device.mac, value === DIRECT_EXIT ? '' : value))
          }}>
            <SelectTrigger id="device-exit" className="w-full"><SelectValue>{(v: string) => v === INHERIT_EXIT ? 'Follow the network setting' : v === DIRECT_EXIT ? 'This router’s own connection' : v}</SelectValue></SelectTrigger>
            <SelectContent><SelectItem value={INHERIT_EXIT}>Follow the network setting</SelectItem><SelectItem value={DIRECT_EXIT}>This router’s own connection</SelectItem>{(gateway.data?.exits ?? []).map((e) => <SelectItem key={e.name} value={e.name}>{e.name}</SelectItem>)}</SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">For devices directly on this router. Changes apply immediately; open connections normally keep their route.</p>
          {gateway.isError && <p role="alert" className="text-sm text-destructive">Gateway settings are unavailable.</p>}
        </div>
      </div>
    </div>
    <div className="mt-4 space-y-3"><DhcpOutcome applier={dhcpApplier} /><GatewayOutcome applier={gatewayApplier} /></div>
  </section>
}
