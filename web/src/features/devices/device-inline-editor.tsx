import { Pencil } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
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
    <header className="rounded-2xl border bg-card p-5 sm:p-7">
      <div className="flex items-start gap-5">
        <button type="button" disabled={identityBusy} onClick={() => begin('icon')} aria-label="Change device icon" className="group relative shrink-0 rounded-lg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring">
          <DeviceIcon icon={device.icon} category={device.category} vendor={device.vendor} vendorKey={device.vendor_key} online={device.online} size="lg" />
          <Pencil className="absolute -bottom-1 -right-1 size-4 rounded-full bg-card p-0.5 opacity-70 group-hover:opacity-100" aria-hidden />
        </button>
        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            {editing === 'name' ? <div className="flex flex-wrap gap-2"><Input autoFocus aria-label="Device name" className="w-56" value={name} onChange={(e) => setName(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') void save({ name: name.trim() || undefined }); if (e.key === 'Escape') setEditing(null) }} /><Button size="sm" disabled={identityBusy} onClick={() => save({ name: name.trim() || undefined })}>Save</Button><Button size="sm" variant="ghost" onClick={() => setEditing(null)}>Cancel</Button></div>
              : <button type="button" disabled={identityBusy} onClick={() => begin('name')} aria-label="Edit device name" className="group flex items-center gap-2 text-left"><h1 className="break-words text-2xl font-semibold tracking-tight sm:text-3xl">{device.name || device.mac}</h1><Pencil className="size-4 shrink-0 text-muted-foreground opacity-60 group-hover:opacity-100" aria-hidden /></button>}
            <Badge variant={device.online ? 'success' : device.seen ? 'secondary' : 'outline'}>{device.online ? 'Online' : device.seen ? 'Offline' : 'Never seen'}</Badge>
          </div>
          <p className="font-mono text-xs text-muted-foreground">{device.mac}</p>
          <div className="flex flex-wrap items-center gap-1 text-sm text-muted-foreground">
            <span>{device.network || 'Network unknown'} · </span>
            <Select value={device.group || NO_GROUP} disabled={identityBusy} onValueChange={(v) => { if (v) void save({ group: v === NO_GROUP ? undefined : v }) }}>
              <SelectTrigger aria-label="Change device group" className="h-auto w-auto min-w-20 border-0 px-1 py-0 shadow-none"><SelectValue>{(v: string) => v === NO_GROUP || !v ? 'No group' : v}</SelectValue></SelectTrigger>
              <SelectContent><SelectItem value={NO_GROUP}>No group</SelectItem>{groupOptions(identity.data?.groups ?? []).map((g) => <SelectItem key={g.name} value={g.name}><span style={{ paddingLeft: g.depth * 12 }}>{g.name}</span></SelectItem>)}</SelectContent>
            </Select>
            {device.last_seen && <span>· Last heard {formatAgo(device.last_seen)}</span>}
          </div>
        </div>
      </div>
      {editing === 'icon' && <div className="mt-5 border-t pt-4"><div className="mb-3 flex items-center justify-between"><h2 className="font-medium">Choose icon</h2><Button variant="ghost" size="sm" onClick={() => setEditing(null)}>Cancel</Button></div><CategoryPicker value={look} detected={device.detected_category} onChange={(choice) => { setLook(choice); void save({ category: choice.category || undefined, icon: choice.icon || undefined }) }} /></div>}
    </header>

    <div className="grid gap-4 md:grid-cols-[1.1fr_0.9fr]">
      <Card><CardHeader><CardTitle>What the router knows</CardTitle></CardHeader><CardContent>
        <dl className="divide-y text-sm">
          <Fact label="Addresses" value={device.ips?.length ? device.ips.join(', ') : 'None observed'} mono />
          <div className="grid gap-1 py-2.5 sm:grid-cols-[9rem_1fr]"><dt className="text-muted-foreground">Fixed address</dt><dd>
            {editing === 'reservation' ? <div className="space-y-2"><Input autoFocus aria-label="Reserved IP address" value={ip} onChange={(e) => setIp(e.target.value)} placeholder="192.168.1.50" /><Input aria-label="Reservation hostname (optional)" value={hostname} onChange={(e) => setHostname(e.target.value)} placeholder="Hostname (optional)" /><div className="flex flex-wrap gap-2"><Button size="sm" disabled={!ip.trim() || !dhcp.data || dhcpApplier.busy} onClick={() => void saveReservation()}>Save</Button>{reservation && <Button size="sm" variant="destructive" disabled={dhcpApplier.busy} onClick={() => void saveReservation(true)}>Remove</Button>}<Button size="sm" variant="ghost" onClick={() => setEditing(null)}>Cancel</Button></div></div>
              : <button type="button" disabled={!dhcp.data || dhcpApplier.busy} onClick={() => begin('reservation')} aria-label="Edit fixed address" className="group inline-flex items-center gap-2 text-left font-mono text-xs hover:text-primary">{device.fixed_ip || 'Reserve an address'}<Pencil className="size-3.5 opacity-60 group-hover:opacity-100" aria-hidden /></button>}
          </dd></div>
          <Fact label="Calls itself" value={device.hostname || 'Not reported'} />
          <Fact label="Vendor" value={device.vendor || 'Unknown'} />
          <Fact label="Network" value={device.network || 'Unknown'} />
          <div className="grid gap-1 py-2.5 text-sm sm:grid-cols-[9rem_1fr]"><dt className="text-muted-foreground">Internet via</dt><dd>
            <Select value={exit} disabled={!gateway.data || gatewayApplier.busy} onValueChange={(v) => { if (v) void gatewayApplier.submit(v === INHERIT_EXIT ? gatewayChange.removeDevice(device.mac) : gatewayChange.device(device.mac, v === DIRECT_EXIT ? '' : v)) }}>
              <SelectTrigger aria-label="Change gateway route" className="h-auto w-full max-w-xs border-0 px-0 py-0 shadow-none"><SelectValue>{(v: string) => v === INHERIT_EXIT ? 'Follow network setting' : v === DIRECT_EXIT ? 'This router’s connection' : v}</SelectValue></SelectTrigger>
              <SelectContent><SelectItem value={INHERIT_EXIT}>Follow network setting</SelectItem><SelectItem value={DIRECT_EXIT}>This router’s connection</SelectItem>{(gateway.data?.exits ?? []).map((e) => <SelectItem key={e.name} value={e.name}>{e.name}</SelectItem>)}</SelectContent>
            </Select>
          </dd></div>
          <Fact label="Seen by" value={device.sources?.length ? device.sources.map((s) => s === 'dhcp-lease' ? 'DHCP lease' : s === 'arp' ? 'Network traffic' : s).join(', ') : 'Not observed'} />
          <Fact label="Lease expires" value={device.expires ? new Date(device.expires).toLocaleString() : 'No expiring lease'} />
        </dl>
        <div className="mt-4 text-sm">{editing === 'notes' ? <div className="space-y-2"><Textarea autoFocus aria-label="Device notes" value={notes} onChange={(e) => setNotes(e.target.value)} /><div className="flex gap-2"><Button size="sm" disabled={identityBusy} onClick={() => save({ notes: notes.trim() || undefined })}>Save notes</Button><Button size="sm" variant="ghost" onClick={() => setEditing(null)}>Cancel</Button></div></div>
          : <button type="button" disabled={identityBusy} onClick={() => begin('notes')} className="group inline-flex items-center gap-2 rounded-lg bg-muted/60 p-3 text-left">{device.notes || 'Add notes'}<Pencil className="size-3.5 text-muted-foreground opacity-60 group-hover:opacity-100" aria-hidden /></button>}</div>
        <p className="mt-4 text-xs text-muted-foreground">Addresses and presence are observations, not settings. Click a setting to change it.</p>
      </CardContent></Card>
      {traffic}
    </div>
    <DhcpOutcome applier={dhcpApplier} />
    <GatewayOutcome applier={gatewayApplier} />
  </>
}

function Fact({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return <div className="grid gap-1 py-2.5 sm:grid-cols-[9rem_1fr]"><dt className="text-muted-foreground">{label}</dt><dd className={`min-w-0 break-all ${mono ? 'font-mono text-xs' : ''}`}>{value}</dd></div>
}
