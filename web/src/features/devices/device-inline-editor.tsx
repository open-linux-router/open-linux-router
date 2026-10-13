import { Pencil } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { CategoryPicker, type IconChoice } from '@/features/devices/category-picker'
import { DeviceIcon } from '@/features/devices/device-icon'
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
import { formatAgo } from '@/lib/utils'

const NO_GROUP = ' none'
const INHERIT_EXIT = ' inherit'
const DIRECT_EXIT = ' direct'

type Field = 'name' | 'icon' | 'notes' | 'reservation' | null

export function DeviceInlineEditor({ device, traffic }: { device: DeviceRow; traffic: ReactNode }) {
  const identity = useDevicesConfig()
  const saveIdentity = useApplyDevicesConfig()
  const dhcp = useDhcpConfig()
  const dhcpApplier = useDhcpApply()
  const gateway = useGatewayConfig()
  const gatewayApplier = useGatewayApply()
  const reservation = dhcp.data?.reservations?.find((r) => r.mac.toLowerCase() === device.mac.toLowerCase())
  const override = gateway.data?.devices?.find((r) => r.mac.toLowerCase() === device.mac.toLowerCase())
  const exit = override ? override.exit || DIRECT_EXIT : INHERIT_EXIT
  const [editing, setEditing] = useState<Field>(null)
  const [name, setName] = useState('')
  const [look, setLook] = useState<IconChoice>({ category: '', icon: '' })
  const [notes, setNotes] = useState('')
  const [ip, setIp] = useState('')
  const [hostname, setHostname] = useState('')
  const identityBusy = !identity.data || saveIdentity.isPending

  function begin(field: Exclude<Field, null>) {
    setEditing(field)
    setName(device.name_origin === 'operator' ? device.name : '')
    setLook({ category: device.category_origin === 'operator' ? device.category : '', icon: device.icon ?? '' })
    setNotes(device.notes ?? '')
    setIp(reservation?.ip ?? '')
    setHostname(reservation?.hostname ?? '')
  }

  async function save(patch: Partial<Device>) {
    if (!identity.data) return
    const stored = identity.data.devices?.find((d) => d.mac.toLowerCase() === device.mac.toLowerCase())
    const entry: Device = { ...stored, ...patch, mac: device.mac }
    const rest = (identity.data.devices ?? []).filter((d) => d.mac.toLowerCase() !== device.mac.toLowerCase())
    const empty = !entry.name && !entry.category && !entry.icon && !entry.group && !entry.notes && !entry.model
    const next: DevicesConfig = { ...identity.data, devices: empty ? rest : [...rest, entry] }
    try {
      await saveIdentity.mutateAsync(next)
      setEditing(null)
      toast.success('Device saved')
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : String(error))
    }
  }

  async function saveReservation(remove = false) {
    if (!dhcp.data) return
    const rest = (dhcp.data.reservations ?? []).filter((r) => r.mac.toLowerCase() !== device.mac.toLowerCase())
    const entry: Reservation = { ...reservation, mac: device.mac, ip: ip.trim(), hostname: hostname.trim() || undefined }
    const applied = await dhcpApplier.submit({ ...dhcp.data, reservations: remove ? rest : [...rest, entry] })
    if (applied) setEditing(null)
  }

  return <>
    <Card className="gap-0 overflow-hidden py-0">
    <header className="p-4 sm:px-6">
      <div className="flex items-center gap-4">
        <button type="button" disabled={identityBusy} onClick={() => begin('icon')} aria-label="Change device icon" className="group relative shrink-0 rounded-lg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring">
          <DeviceIcon icon={device.icon} category={device.category} vendor={device.vendor} vendorKey={device.vendor_key} online={device.online} size="lg" />
        </button>
        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            {editing === 'name' ? <div className="flex flex-wrap gap-2"><Input autoFocus aria-label="Device name" className="w-56" value={name} onChange={(e) => setName(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') void save({ name: name.trim() || undefined }); if (e.key === 'Escape') setEditing(null) }} /><Button size="sm" disabled={identityBusy} onClick={() => save({ name: name.trim() || undefined })}>Save</Button><Button size="sm" variant="ghost" onClick={() => setEditing(null)}>Cancel</Button></div>
              : <button type="button" disabled={identityBusy} onClick={() => begin('name')} aria-label="Edit device name" className="group flex items-center gap-2 text-left"><h1 className="break-words text-2xl font-semibold tracking-tight sm:text-3xl">{device.name || device.mac}</h1><Pencil className="size-4 shrink-0 text-muted-foreground opacity-60 group-hover:opacity-100" aria-hidden /></button>}
          </div>
        </div>
        <Badge className="shrink-0 self-start sm:self-center" variant={device.online ? 'success' : device.seen ? 'secondary' : 'outline'}>{device.online ? 'Online' : device.seen ? 'Offline' : 'Never seen'}</Badge>
      </div>
    </header>

    <div className="grid gap-y-4 lg:grid-cols-2">
      <section aria-label="Device information and settings" className="min-w-0 p-4 pt-0 sm:px-6">
          <div className="mb-3 flex flex-wrap items-center gap-1 text-sm text-muted-foreground">
            <span>{device.network || 'Network unknown'} · </span>
            <Select value={device.group || NO_GROUP} disabled={identityBusy} onValueChange={(v) => { if (v) void save({ group: v === NO_GROUP ? undefined : v }) }}>
              <SelectTrigger aria-label="Change device group" className="h-auto w-auto min-w-20 border-0 px-1 py-0 shadow-none"><SelectValue>{(v: string) => v === NO_GROUP || !v ? 'No group' : v}</SelectValue></SelectTrigger>
              <SelectContent><SelectItem value={NO_GROUP}>No group</SelectItem>{groupOptions(identity.data?.groups ?? []).map((g) => <SelectItem key={g.name} value={g.name}><span style={{ paddingLeft: g.depth * 12 }}>{g.name}</span></SelectItem>)}</SelectContent>
            </Select>
            {device.last_seen && <span>· Last heard {formatAgo(device.last_seen)}</span>}
          </div>
        <dl className="space-y-1 text-sm">
          <Fact label="MAC" value={device.mac} mono />
          <div className="grid grid-cols-[5rem_minmax(0,1fr)] items-start gap-3 py-2"><dt className="pt-1 text-muted-foreground">IP</dt><dd className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
            {(device.ips ?? []).map((address) => <div key={address} className="flex flex-wrap items-center gap-x-2 gap-y-1"><span className="break-all font-mono text-xs">{address}</span>{address === device.fixed_ip && <span className="text-xs text-muted-foreground">Fixed</span>}</div>)}
            {!device.ips?.length && <span className="text-muted-foreground">None observed</span>}
            {device.fixed_ip && !device.ips?.includes(device.fixed_ip) && <div className="flex flex-wrap items-center gap-2"><span className="break-all font-mono text-xs">{device.fixed_ip}</span><span className="text-xs text-muted-foreground">Fixed · not observed</span></div>}
            {editing === 'reservation' ? <div className="space-y-2"><Input autoFocus aria-label="Reserved IP address" value={ip} onChange={(e) => setIp(e.target.value)} placeholder="192.168.1.50" /><Input aria-label="Reservation hostname (optional)" value={hostname} onChange={(e) => setHostname(e.target.value)} placeholder="Hostname (optional)" /><div className="flex flex-wrap gap-2"><Button size="sm" disabled={!ip.trim() || !dhcp.data || dhcpApplier.busy} onClick={() => void saveReservation()}>Save</Button>{reservation && <Button size="sm" variant="destructive" disabled={dhcpApplier.busy} onClick={() => void saveReservation(true)}>Remove</Button>}<Button size="sm" variant="ghost" onClick={() => setEditing(null)}>Cancel</Button></div></div>
              : <button type="button" disabled={!dhcp.data || dhcpApplier.busy} onClick={() => begin('reservation')} aria-label="Edit fixed address" title="Edit address reservation" className="group inline-flex min-h-9 items-center gap-2 rounded-md text-left text-xs text-muted-foreground hover:text-primary focus-visible:outline-2 focus-visible:outline-ring max-sm:min-h-11">{!dhcp.data ? 'Reservation unavailable' : !device.fixed_ip ? 'No reservation · Reserve' : null}<Pencil className="size-3.5 opacity-60 group-hover:opacity-100" aria-hidden /></button>}
          </dd></div>
          {device.hostname && device.hostname !== device.name && <Fact label="Hostname" value={device.hostname} />}
          <Fact label="Vendor" value={device.vendor || 'Unknown'} />
          <div className="grid grid-cols-[5rem_minmax(0,1fr)] items-center gap-3 py-2 text-sm"><dt className="text-muted-foreground">Internet via</dt><dd className="min-w-0">
            <Select value={exit} disabled={!gateway.data || gatewayApplier.busy} onValueChange={(v) => { if (v) void gatewayApplier.submit(v === INHERIT_EXIT ? gatewayChange.removeDevice(device.mac) : gatewayChange.device(device.mac, v === DIRECT_EXIT ? '' : v)) }}>
              <SelectTrigger aria-label="Change gateway route" className="h-auto w-full max-w-xs border-0 px-0 py-0 shadow-none"><SelectValue>{(v: string) => v === INHERIT_EXIT ? 'Follow network setting' : v === DIRECT_EXIT ? 'This router’s connection' : v}</SelectValue></SelectTrigger>
              <SelectContent><SelectItem value={INHERIT_EXIT}>Follow network setting</SelectItem><SelectItem value={DIRECT_EXIT}>This router’s connection</SelectItem>{(gateway.data?.exits ?? []).map((e) => <SelectItem key={e.name} value={e.name}>{e.name}</SelectItem>)}</SelectContent>
            </Select>
          </dd></div>
          <Fact label="Seen by" value={device.sources?.length ? device.sources.map((s) => s === 'dhcp-lease' ? 'DHCP lease' : s === 'arp' ? 'Network traffic' : s).join(' · ') : 'Not observed'} />
          {device.expires && <Fact label="Lease ends" value={new Date(device.expires).toLocaleString()} />}
        </dl>
        <div className="mt-2 pt-2 text-sm">{editing === 'notes' ? <div className="space-y-2"><Textarea autoFocus aria-label="Device notes" value={notes} onChange={(e) => setNotes(e.target.value)} /><div className="flex gap-2"><Button size="sm" disabled={identityBusy} onClick={() => save({ notes: notes.trim() || undefined })}>Save notes</Button><Button size="sm" variant="ghost" onClick={() => setEditing(null)}>Cancel</Button></div></div>
          : <button type="button" disabled={identityBusy} onClick={() => begin('notes')} className="group inline-flex min-h-9 items-center gap-2 rounded-md text-left text-muted-foreground hover:text-foreground max-sm:min-h-11">{device.notes || 'Add notes'}<Pencil className="size-3.5 opacity-60 group-hover:opacity-100" aria-hidden /></button>}</div>
      </section>
      <div className="min-w-0 space-y-6 p-4 pt-0 sm:px-6">{traffic}</div>
    </div>
    </Card>
    <Dialog open={editing === 'icon'} onOpenChange={(open) => { if (!open) setEditing(null) }}>
      <DialogContent className="max-h-[90svh] overflow-y-auto">
        <DialogHeader><DialogTitle>Device icon</DialogTitle><DialogDescription>Choose an icon for {device.name || device.mac}.</DialogDescription></DialogHeader>
        <CategoryPicker value={look} detected={device.detected_category} onChange={setLook} />
        <DialogFooter><Button variant="ghost" disabled={saveIdentity.isPending} onClick={() => setEditing(null)}>Cancel</Button><Button disabled={identityBusy} onClick={() => void save({ category: look.category || undefined, icon: look.icon || undefined })}>Save icon</Button></DialogFooter>
      </DialogContent>
    </Dialog>
    <DhcpOutcome applier={dhcpApplier} />
    <GatewayOutcome applier={gatewayApplier} />
  </>
}

function Fact({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return <div className="grid grid-cols-[5rem_minmax(0,1fr)] items-baseline gap-3 py-2"><dt className="text-muted-foreground">{label}</dt><dd className={`min-w-0 break-words ${mono ? 'break-all font-mono text-xs' : ''}`}>{value}</dd></div>
}
