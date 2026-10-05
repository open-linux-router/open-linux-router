import { ArrowLeft, FolderTree, Network, ShieldAlert } from 'lucide-react'
import { useState } from 'react'
import { Link, useParams } from 'react-router'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { List, ListEmpty, ListRow } from '@/components/ui/list'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { DeviceIcon } from '@/features/devices/device-icon'
import { groupOptions, subtreeOf } from '@/features/devices/group-tree'
import { useDeviceList, useDevicesConfig } from '@/features/devices/queries'
import { ApplyOutcome as GatewayOutcome } from '@/features/gateway/editor'
import { gatewayChange, useGatewayConfig } from '@/features/gateway/queries'
import { useGatewayApply } from '@/features/gateway/use-apply'
import { useQosConfig, useQosDevicesApply, type Priority } from '@/features/qos/queries'

const FOLLOW = ' follow'
const DIRECT = ' direct'

export function GroupPage() {
  const { name = '' } = useParams()
  const identity = useDevicesConfig()
  const devices = useDeviceList()
  const gateway = useGatewayConfig()
  const qos = useQosConfig()
  const gatewayApply = useGatewayApply()
  const qosApply = useQosDevicesApply()
  const [exit, setExit] = useState(FOLLOW)
  const [priority, setPriority] = useState<Priority>('normal')
  const [confirming, setConfirming] = useState<'exit' | 'priority' | null>(null)
  const [working, setWorking] = useState(false)
  const groups = identity.data?.groups ?? []
  const group = groups.find((g) => g.name === name)
  const descendants = subtreeOf(groups, name)
  const members = (devices.data?.devices ?? []).filter((d) => d.group && descendants.has(d.group))
  const direct = members.filter((d) => d.group === name).length
  const children = groupOptions(groups).filter((g) => g.parent === name)

  if (identity.isPending || devices.isPending) return <p className="py-16 text-sm text-muted-foreground">Loading group…</p>
  if (identity.isError || devices.isError) return <State title="Could not load this group" />
  if (!group) return <State title="Group not found" />

  async function applyCurrentMembers() {
    if (!confirming) return
    const action = confirming
    setConfirming(null)
    setWorking(true)
    let applied = 0
    try {
      for (const device of members) {
        if (action === 'exit') {
          const change = exit === FOLLOW ? gatewayChange.removeDevice(device.mac) : gatewayChange.device(device.mac, exit === DIRECT ? '' : exit)
          // An inherited device has nothing to remove. Do not turn a no-op into a 404.
          if (exit === FOLLOW && !gateway.data?.devices?.some((d) => d.mac.toLowerCase() === device.mac.toLowerCase())) continue
          if (!await gatewayApply.submit(change)) break
        } else {
          const current = qos.data?.devices?.find((d) => d.mac.toLowerCase() === device.mac.toLowerCase())
          await qosApply.mutateAsync({ mac: device.mac, priority, download_mbps: current?.download_mbps || 0, upload_mbps: current?.upload_mbps || 0 })
        }
        applied++
      }
      if (action === 'priority' && applied) toast.warning(`Priority saved for ${applied} devices, but not active`, { description: 'QoS enforcement is not available yet.' })
      if (applied < members.length) toast.warning(`Stopped after ${applied} of ${members.length} devices`, { description: 'Review the remaining devices before trying again.' })
    } catch (error) {
      toast.error(`Stopped after ${applied} of ${members.length} devices`, { description: String(error) })
    } finally {
      setWorking(false)
    }
  }

  return <div className="mx-auto max-w-5xl space-y-6 pb-12">
    <Link to="/" className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"><ArrowLeft className="size-4" aria-hidden /> Network overview</Link>
    <header className="rounded-2xl border bg-card p-5 sm:p-7">
      <div className="flex items-start gap-4"><span className="rounded-xl bg-muted p-3"><FolderTree className="size-6" aria-hidden /></span><div className="min-w-0 space-y-1"><h1 className="break-words text-2xl font-semibold tracking-tight sm:text-3xl">{group.name}</h1><p className="text-sm text-muted-foreground">{group.parent ? <>Inside <Link className="underline underline-offset-4" to={`/groups/${encodeURIComponent(group.parent)}`}>{group.parent}</Link> · </> : 'Top-level group · '}{members.length} devices, {direct} directly here</p></div></div>
    </header>

    <Card><CardHeader><CardTitle>Devices in this group</CardTitle></CardHeader><CardContent className="space-y-4">
      <p className="text-sm text-muted-foreground">Includes devices in subgroups. Each device opens its own details and settings.</p>
      {children.length > 0 && <div className="flex flex-wrap gap-2">{children.map((child) => <Button key={child.name} size="sm" variant="outline" render={<Link to={`/groups/${encodeURIComponent(child.name)}`} />}>{child.name}</Button>)}</div>}
      {members.length ? <List>{members.map((device) => <ListRow key={device.mac} to={`/devices/${encodeURIComponent(device.mac)}`} title={device.name || device.mac} subtitle={`${device.group === name ? 'Direct member' : device.group} · ${device.network || 'Network unknown'} · ${device.mac}`} leading={<DeviceIcon icon={device.icon} category={device.category} vendor={device.vendor} vendorKey={device.vendor_key} online={device.online} />} trailing={<Badge variant={device.online ? 'success' : device.seen ? 'secondary' : 'outline'}>{device.online ? 'Online' : device.seen ? 'Offline' : 'Never seen'}</Badge>} />)}</List> : <ListEmpty>No devices in this group or its subgroups yet.</ListEmpty>}
    </CardContent></Card>

    <Card><CardHeader><CardTitle>Internet via</CardTitle></CardHeader><CardContent className="space-y-4">
      <p className="text-sm text-muted-foreground">Apply a route to the {members.length} devices currently listed here. This sets each device’s override; it is not inherited by devices added later. Existing per-device choices will be replaced.</p>
      <div className="flex flex-wrap gap-3"><Select value={exit} disabled={!gateway.data || working} onValueChange={(v) => { if (v) setExit(v) }}><SelectTrigger aria-label="Internet via for current devices" className="w-full sm:w-72"><SelectValue>{(v: string) => v === FOLLOW ? 'Follow each network' : v === DIRECT ? 'This router’s connection' : v}</SelectValue></SelectTrigger><SelectContent><SelectItem value={FOLLOW}>Follow each network</SelectItem><SelectItem value={DIRECT}>This router’s connection</SelectItem>{(gateway.data?.exits ?? []).map((e) => <SelectItem key={e.name} value={e.name}>{e.name}</SelectItem>)}</SelectContent></Select><Button disabled={!members.length || !gateway.data || working} onClick={() => setConfirming('exit')}>Apply to current devices</Button></div>
      {gateway.isError && <p className="text-sm text-destructive">Gateway settings are unavailable.</p>}
    </CardContent></Card>

    <Card><CardHeader><CardTitle>Network priority and speed limit</CardTitle></CardHeader><CardContent className="space-y-4">
      <p className="text-sm text-muted-foreground">Save a priority on the devices currently listed, preserving their individual limits. New members will not inherit it. QoS settings are saved but not enforced by this build.</p>
      <div className="flex flex-wrap gap-3"><Select value={priority} disabled={!qos.data || working} onValueChange={(v) => { if (v) setPriority(v as Priority) }}><SelectTrigger aria-label="Priority for current devices" className="w-full sm:w-48"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="high">High</SelectItem><SelectItem value="normal">Normal</SelectItem><SelectItem value="low">Low</SelectItem></SelectContent></Select><Button disabled={!members.length || !qos.data || working} onClick={() => setConfirming('priority')}>Save for current devices</Button></div>
      <div className="flex items-start gap-2 rounded-xl border border-dashed p-4 text-sm text-muted-foreground"><ShieldAlert className="mt-0.5 size-4 shrink-0" aria-hidden /><p>A total group speed limit is not available. Per-device limits do not add up to a shared cap, and QoS enforcement is not implemented yet. Set individual saved limits from a device’s details page.</p></div>
      {qos.isError && <p className="text-sm text-destructive">QoS settings are unavailable.</p>}
    </CardContent></Card>
    <GatewayOutcome applier={gatewayApply} />
    <Dialog open={confirming !== null} onOpenChange={(open) => !open && setConfirming(null)}><DialogContent><DialogHeader><DialogTitle>Change {members.length} current devices?</DialogTitle><DialogDescription>{confirming === 'exit' ? 'Their existing Internet via overrides will be changed. Traffic may take a different path; established connections normally keep theirs.' : 'Their saved priorities will change, but no packets will be prioritized until QoS enforcement is available.'} New members will not inherit this setting.</DialogDescription></DialogHeader><DialogFooter><Button variant="ghost" onClick={() => setConfirming(null)}>Cancel</Button><Button onClick={() => void applyCurrentMembers()}>Continue</Button></DialogFooter></DialogContent></Dialog>
  </div>
}

function State({ title }: { title: string }) {
  return <div className="mx-auto max-w-5xl space-y-3 py-16 text-center"><Network className="mx-auto size-8 text-muted-foreground" aria-hidden /><h1 className="text-xl font-semibold">{title}</h1><Button variant="outline" render={<Link to="/">Back to overview</Link>} /></div>
}
