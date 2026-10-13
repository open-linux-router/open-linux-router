import { useRef, useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { CategoryPicker, type IconChoice } from '@/features/devices/category-picker'
import { DeviceIcon } from '@/features/devices/device-icon'
import { DeviceSettingRow } from '@/features/devices/device-setting-row'
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

type Field = 'name' | 'icon' | 'reservation' | null

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
  const [ip, setIp] = useState('')
  const [fixed, setFixed] = useState(false)
  const identityBusy = !identity.data || saveIdentity.isPending

  function begin(field: Exclude<Field, null>) {
    setEditing(field)
    nameFinished.current = false
    setName(device.name || device.mac)
    setLook({ category: device.category_origin === 'operator' ? device.category : '', icon: device.icon ?? '' })
    setIp(reservation?.ip ?? device.ips?.find((address) => !address.includes(':')) ?? '')
    setFixed(!!reservation)
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

  async function saveReservation() {
    if (!dhcp.data) return
    const rest = (dhcp.data.reservations ?? []).filter((r) => r.mac.toLowerCase() !== device.mac.toLowerCase())
    const entry: Reservation = { ...reservation, mac: device.mac, ip: ip.trim() }
    const applied = await dhcpApplier.submit({ ...dhcp.data, reservations: fixed ? [...rest, entry] : rest })
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
              {editing === 'name' ? <Input autoFocus aria-label="Device name" className="h-auto w-80 max-w-full py-1 text-2xl font-semibold sm:text-3xl" value={name} disabled={saveIdentity.isPending} onChange={(e) => { nameFinished.current = false; setName(e.target.value) }} onBlur={() => finishName()} onKeyDown={(e) => { if (e.nativeEvent.isComposing) return; if (e.key === 'Enter' || e.key === 'Escape') { e.preventDefault(); finishName(e.key === 'Escape') } }} />
                : <button type="button" disabled={identityBusy} onClick={() => begin('name')} aria-label="Edit device name" className="w-full break-words rounded-md text-left hover:text-primary focus-visible:outline-2 focus-visible:outline-ring">{device.name || device.mac}</button>}
            </h1>
            <Badge className="text-2xl sm:text-3xl" variant={device.online ? 'success' : device.seen ? 'secondary' : 'outline'}>{device.online ? 'Online' : device.seen ? 'Offline' : 'Never seen'}</Badge>
          </div>
        </div>
      </div>
    </header>
    <div className="mt-4 grid gap-y-8 lg:grid-cols-2 lg:gap-x-16">
      <div className="min-w-0">
        <dl className="text-sm">
          <Fact label="Vendor" value={device.vendor || 'Unknown'} />
          <Fact label="MAC" value={device.mac} mono />
          <DeviceSettingRow label="IP">
            <div className="flex flex-wrap items-center justify-end gap-2">
              <Tooltip><TooltipTrigger onClick={() => begin('reservation')} disabled={!dhcp.data || dhcpApplier.busy} aria-label="Edit IP address" className="min-h-11 sm:min-h-9 min-w-0 rounded-sm text-right font-mono text-xs hover:text-primary focus-visible:outline-2 focus-visible:outline-ring"><span className="break-all">{device.ips?.length ? device.ips.join(', ') : device.fixed_ip || 'None observed'}</span></TooltipTrigger><TooltipContent>{device.expires ? `DHCP lease ends ${new Date(device.expires).toLocaleString()}` : 'No expiring DHCP lease'}</TooltipContent></Tooltip>
              {reservation && <Badge variant="secondary">Fixed</Badge>}
            </div>
          </DeviceSettingRow>
          <Fact label="Last heard" value={device.last_seen ? formatAgo(device.last_seen) : 'Never'} />
          <DeviceSettingRow label="Group"><Select value={device.group || NO_GROUP} disabled={identityBusy} onValueChange={(v) => { if (v) void save({ group: v === NO_GROUP ? undefined : v }) }}><SelectTrigger aria-label="Change device group" className="ml-auto h-auto min-h-11 sm:min-h-9 max-w-full rounded-md border-transparent bg-secondary px-2 text-xs font-medium shadow-none whitespace-normal"><SelectValue className="flex-none text-right break-words">{(v: string) => v === NO_GROUP || !v ? 'No group' : v}</SelectValue></SelectTrigger><SelectContent><SelectItem value={NO_GROUP}>No group</SelectItem>{groupOptions(identity.data?.groups ?? []).map((g) => <SelectItem key={g.name} value={g.name}><span style={{ paddingLeft: g.depth * 12 }}>{g.name}</span></SelectItem>)}</SelectContent></Select></DeviceSettingRow>
          <DeviceSettingRow label="Internet via">
            <Select value={exit} disabled={!gateway.data || gatewayApplier.busy} onValueChange={(v) => { if (v) void gatewayApplier.submit(v === INHERIT_EXIT ? gatewayChange.removeDevice(device.mac) : gatewayChange.device(device.mac, v === DIRECT_EXIT ? '' : v)) }}>
              <SelectTrigger aria-label="Change gateway route" className="ml-auto h-auto min-h-11 sm:min-h-9 max-w-full border-0 px-0 shadow-none whitespace-normal"><SelectValue className="flex-none text-right break-words">{(v: string) => v === INHERIT_EXIT ? 'Follow network setting' : v === DIRECT_EXIT ? 'This router’s connection' : v}</SelectValue></SelectTrigger>
              <SelectContent><SelectItem value={INHERIT_EXIT}>Follow network setting</SelectItem><SelectItem value={DIRECT_EXIT}>This router’s connection</SelectItem>{(gateway.data?.exits ?? []).map((e) => <SelectItem key={e.name} value={e.name}>{e.name}</SelectItem>)}</SelectContent>
            </Select>
          </DeviceSettingRow>
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
    <Dialog open={editing === 'reservation' && !dhcpApplier.confirming} onOpenChange={(open) => { if (!open && !dhcpApplier.busy) setEditing(null) }}>
      <DialogContent className="max-h-[90svh] overflow-y-auto">
        <DialogHeader><DialogTitle>IP address</DialogTitle><DialogDescription>Address settings for {device.name || device.mac}.</DialogDescription></DialogHeader>
        <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); if (!dhcpApplier.busy && (!fixed || ip.trim())) void saveReservation() }}>
          <div className="space-y-2"><label htmlFor="device-ip" className="text-sm font-medium">IP address</label><Input id="device-ip" autoFocus value={ip} disabled={dhcpApplier.busy} onChange={(e) => { setIp(e.target.value); setFixed(true) }} placeholder="192.168.1.50" /></div>
          <div className="flex items-center justify-between gap-3"><label htmlFor="device-fixed" className="text-sm font-medium">Fixed IP address</label><Switch id="device-fixed" checked={fixed} disabled={dhcpApplier.busy} onCheckedChange={setFixed} /></div>
          <DialogFooter><Button type="button" variant="ghost" disabled={dhcpApplier.busy} onClick={() => setEditing(null)}>Cancel</Button><Button type="submit" disabled={(fixed && !ip.trim()) || !dhcp.data || dhcpApplier.busy}>Save</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
    <DhcpOutcome applier={dhcpApplier} />
    <GatewayOutcome applier={gatewayApplier} />
  </>
}

function Fact({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return <DeviceSettingRow label={label}><span className={`break-words ${mono ? 'font-mono text-xs break-all' : ''}`}>{value}</span></DeviceSettingRow>
}
