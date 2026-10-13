import { Pencil } from 'lucide-react'
import { useRef, useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { Switch } from '@/components/ui/switch'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
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
  const nameFinished = useRef(false)
  const [look, setLook] = useState<IconChoice>({ category: '', icon: '' })
  const [notes, setNotes] = useState('')
  const [ip, setIp] = useState('')
  const [hostname, setHostname] = useState('')
  const identityBusy = !identity.data || saveIdentity.isPending

  function begin(field: Exclude<Field, null>) {
    setEditing(field)
    nameFinished.current = false
    setName(device.name || device.mac)
    setLook({ category: device.category_origin === 'operator' ? device.category : '', icon: device.icon ?? '' })
    setNotes(device.notes ?? '')
    setIp(reservation?.ip ?? device.ips?.find((address) => !address.includes(':')) ?? '')
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
      nameFinished.current = false
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

  function finishName(discard = false) {
    if (nameFinished.current) return
    nameFinished.current = true
    if (discard || name.trim() === (device.name || device.mac)) {
      setEditing(null)
      return
    }
    void save({ name: name.trim() || undefined })
  }

  return <>
    <Card className="gap-0 p-4 sm:p-6">
    <header>
      <div className="flex items-center gap-4 border-b pb-4">
        <button type="button" disabled={identityBusy} onClick={() => begin('icon')} aria-label="Change device icon" className="group relative shrink-0 rounded-lg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring">
          <DeviceIcon icon={device.icon} category={device.category} vendor={device.vendor} vendorKey={device.vendor_key} online={device.online} size="lg" />
        </button>
        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="min-w-0 flex-1 basis-full text-2xl font-semibold tracking-tight sm:basis-auto sm:text-3xl">
              {editing === 'name' ? <Input autoFocus aria-label="Device name" className="h-auto w-full py-1 text-2xl font-semibold sm:text-3xl" value={name} disabled={saveIdentity.isPending} onChange={(e) => { nameFinished.current = false; setName(e.target.value) }} onBlur={() => finishName()} onKeyDown={(e) => { if (e.nativeEvent.isComposing) return; if (e.key === 'Enter' || e.key === 'Escape') { e.preventDefault(); finishName(e.key === 'Escape') } }} />
                : <button type="button" disabled={identityBusy} onClick={() => begin('name')} aria-label="Edit device name" className="w-full break-words rounded-md text-left hover:text-primary focus-visible:outline-2 focus-visible:outline-ring">{device.name || device.mac}</button>}
            </h1>
            <Badge variant={device.online ? 'success' : device.seen ? 'secondary' : 'outline'}>{device.online ? 'Online' : device.seen ? 'Offline' : 'Never seen'}</Badge>
          </div>
        </div>
      </div>
    </header>
    <div className="mt-4 grid gap-x-8 gap-y-6 lg:grid-cols-2">
      <div className="min-w-0">
        <dl className="text-sm">
          <Fact label="MAC" value={device.mac} mono />
          <div className="grid min-h-11 grid-cols-[6rem_minmax(0,1fr)] items-center gap-3 py-1 sm:min-h-9"><dt className="text-muted-foreground">IP</dt><dd className="min-w-0">
            <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
              <Tooltip><TooltipTrigger onClick={() => begin('reservation')} disabled={!dhcp.data || dhcpApplier.busy} aria-label="Edit IP reservation" className="min-w-0 rounded-sm text-left font-mono text-xs hover:text-primary focus-visible:outline-2 focus-visible:outline-ring"><span className="break-all">{device.ips?.length ? device.ips.join(', ') : device.fixed_ip || 'None observed'}</span>{reservation && <Pencil className="ml-1.5 inline size-3 text-muted-foreground" aria-hidden />}</TooltipTrigger><TooltipContent>{device.expires ? `Lease ends ${new Date(device.expires).toLocaleString()}` : 'No expiring lease'}{reservation && <span> · Fixed address {reservation.ip}</span>}</TooltipContent></Tooltip>
              <label className="inline-flex min-h-11 shrink-0 items-center gap-2 sm:min-h-9"><Switch aria-label="Fixed IP address" checked={!!reservation} disabled={!dhcp.data || dhcpApplier.busy} onCheckedChange={(checked) => { if (checked) begin('reservation'); else void saveReservation(true) }} /><span className="text-xs text-muted-foreground">Fixed</span></label>
            </div>
            {editing === 'reservation' && <div className="space-y-2 py-2"><Input autoFocus aria-label="Reserved IP address" value={ip} onChange={(e) => setIp(e.target.value)} placeholder="192.168.1.50" /><Input aria-label="Reservation hostname (optional)" value={hostname} onChange={(e) => setHostname(e.target.value)} placeholder="Hostname (optional)" /><div className="flex flex-wrap gap-2"><Button size="sm" disabled={!ip.trim() || !dhcp.data || dhcpApplier.busy} onClick={() => void saveReservation()}>Save</Button><Button size="sm" variant="ghost" disabled={dhcpApplier.busy} onClick={() => setEditing(null)}>Cancel</Button></div></div>}
          </dd></div>
          <Fact label="Vendor" value={device.vendor || 'Unknown'} />
          <Fact label="Last heard" value={device.last_seen ? formatAgo(device.last_seen) : 'Never'} />
          <div className="grid min-h-11 sm:min-h-9 grid-cols-[6rem_minmax(0,1fr)] items-center gap-3 py-1 text-sm"><dt className="text-muted-foreground">Group</dt><dd><Select value={device.group || NO_GROUP} disabled={identityBusy} onValueChange={(v) => { if (v) void save({ group: v === NO_GROUP ? undefined : v }) }}><SelectTrigger aria-label="Change device group" className="w-full border-0 px-0 shadow-none"><SelectValue>{(v: string) => v === NO_GROUP || !v ? 'No group' : v}</SelectValue></SelectTrigger><SelectContent><SelectItem value={NO_GROUP}>No group</SelectItem>{groupOptions(identity.data?.groups ?? []).map((g) => <SelectItem key={g.name} value={g.name}><span style={{ paddingLeft: g.depth * 12 }}>{g.name}</span></SelectItem>)}</SelectContent></Select></dd></div>
          <div className="grid min-h-11 sm:min-h-9 grid-cols-[6rem_minmax(0,1fr)] items-center gap-3 py-1 text-sm"><dt className="text-muted-foreground">Internet via</dt><dd>
            <Select value={exit} disabled={!gateway.data || gatewayApplier.busy} onValueChange={(v) => { if (v) void gatewayApplier.submit(v === INHERIT_EXIT ? gatewayChange.removeDevice(device.mac) : gatewayChange.device(device.mac, v === DIRECT_EXIT ? '' : v)) }}>
              <SelectTrigger aria-label="Change gateway route" className="h-auto w-full max-w-xs border-0 px-0 py-0 shadow-none"><SelectValue>{(v: string) => v === INHERIT_EXIT ? 'Follow network setting' : v === DIRECT_EXIT ? 'This router’s connection' : v}</SelectValue></SelectTrigger>
              <SelectContent><SelectItem value={INHERIT_EXIT}>Follow network setting</SelectItem><SelectItem value={DIRECT_EXIT}>This router’s connection</SelectItem>{(gateway.data?.exits ?? []).map((e) => <SelectItem key={e.name} value={e.name}>{e.name}</SelectItem>)}</SelectContent>
            </Select>
          </dd></div>
          <div className="grid min-h-11 sm:min-h-9 grid-cols-[6rem_minmax(0,1fr)] items-center gap-3 py-1"><dt className="text-muted-foreground">Notes</dt><dd className="min-w-0">{editing === 'notes' ? <div className="space-y-2"><Textarea autoFocus aria-label="Device notes" value={notes} onChange={(e) => setNotes(e.target.value)} /><div className="flex gap-2"><Button size="sm" disabled={identityBusy} onClick={() => save({ notes: notes.trim() || undefined })}>Save notes</Button><Button size="sm" variant="ghost" onClick={() => setEditing(null)}>Cancel</Button></div></div>
            : <button type="button" disabled={identityBusy} onClick={() => begin('notes')} className="group inline-flex min-h-11 items-center gap-2 rounded-md text-left hover:text-primary"><span className="break-words">{device.notes || 'Add notes'}</span><Pencil className="size-3.5 shrink-0 text-muted-foreground opacity-60 group-hover:opacity-100" aria-hidden /></button>}</dd></div>
        </dl>
      </div>
      <div className="min-w-0">{traffic}</div>
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
  return <div className="grid min-h-11 sm:min-h-9 grid-cols-[6rem_minmax(0,1fr)] items-center gap-3 py-1"><dt className="text-muted-foreground">{label}</dt><dd className={`min-w-0 break-words ${mono ? 'font-mono text-xs break-all' : ''}`}>{value}</dd></div>
}
