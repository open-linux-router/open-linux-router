import { ArrowLeft, ArrowUpRight, Pencil } from 'lucide-react'
import { useState } from 'react'
import { Link, useParams } from 'react-router'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useDhcpConfig } from '@/features/dhcp/queries'
import { useUplink } from '@/features/dial/queries'
import { UplinkCard } from '@/features/dial/uplink-card'
import { ApplyOutcome, useGatewayEditor } from '@/features/gateway/editor'
import { DIRECT, INHERIT } from '@/features/gateway/network-list'
import { gatewayChange, useGatewayStatus } from '@/features/gateway/queries'
import { InterfacesCard } from '@/features/link/interfaces-card'
import { InterfaceVisual, interfaceState } from '@/features/link/interface-visual'
import { NetworkDialog } from '@/features/link/network-dialog'
import { useNetworkEditor } from '@/features/link/use-networks'
import { ConfirmDisruptive, PartialApply } from '@/routes/gateway/networks'
import type { Network } from '@/lib/config-types'

export function GatewayInterfacePage() {
  const { name } = useParams()
  const editor = useNetworkEditor()
  const gateway = useGatewayEditor()
  const status = useGatewayStatus()
  const uplink = useUplink()
  const dhcp = useDhcpConfig()
  const [editing, setEditing] = useState(false)
  const row = editor.interfaces.find((item) => item.name === name)
  const network = editor.networks.find((item) => item.members.includes(name ?? ''))
  const stored = editor.config?.networks ?? []
  const initial = stored.find((item) => item.name === network?.name)
  const isUplink = name === uplink.data?.uplink?.interface
  const assignment = gateway.config?.interfaces?.find((item) => item.interface === name)
  const observed = status.data?.assignments?.find((item) => item.interface === name)

  if (editor.isPending) return <p className="py-16 text-sm text-muted-foreground">Loading interface…</p>
  if (editor.error) return <State title="Could not load interface" detail={editor.error.message} />
  if (!row) return <State title="Interface not found" detail="It may have been removed from this machine." />

  const state = interfaceState(row)
  function save(next: Network) {
    editor.save([...stored.filter((item) => item.name !== initial?.name), next])
    setEditing(false)
  }

  return <div className="mx-auto max-w-5xl space-y-6 pb-12">
    <Link to="/gateway" className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"><ArrowLeft className="size-4" aria-hidden /> Gateway</Link>
    <header className="flex flex-wrap items-start gap-4">
      <InterfaceVisual row={row} uplink={isUplink} />
      <div className="min-w-0 flex-1">
        <h1 className="font-mono text-2xl font-semibold tracking-tight">{row.name}</h1>
        <p className="text-sm text-muted-foreground">{isUplink ? 'Internet uplink' : network ? `${network.name} network` : row.loopback ? 'Local interface' : 'Network interface'}</p>
      </div>
      <span className={`text-sm font-medium ${state.tone}`}>{state.label}</span>
    </header>

    <div className="grid gap-4 sm:grid-cols-3">
      <Fact label="Link" value={row.present ? row.up ? row.running ? 'Up · carrier detected' : 'Up · no carrier' : 'Down' : 'Not present'} />
      <Fact label="Address" value={row.prefixes?.join(', ') || 'None'} mono />
      <Fact label="Hardware address" value={row.mac || 'Unavailable'} mono />
    </div>

    <section className="space-y-2">
      <h2 className="text-lg font-medium">Management</h2>
      <p className="text-sm text-muted-foreground">Allow this router to configure this interface. Switching it on alone does not change its address.</p>
      <InterfacesCard dhcp={dhcp.data} only={row.name} />
    </section>

    {isUplink ? <section className="space-y-2"><h2 className="text-lg font-medium">Internet uplink</h2><UplinkCard interfaces={editor.interfaces} networks={editor.networks} /></section> : (
      <Card>
        <CardHeader><CardTitle>Network</CardTitle></CardHeader>
        <CardContent className="space-y-4">
          {network ? <div className="space-y-1 text-sm"><p className="font-medium">{network.name}</p><p className="text-muted-foreground">{network.subnet || 'No IPv4 subnet'}{network.router ? ` · Router ${network.router}` : ''}</p>{network.subnet6 && <p className="text-muted-foreground">IPv6 {network.subnet6}</p>}</div> : <p className="text-sm text-muted-foreground">No network is configured on this interface.</p>}
          {!row.loopback && <Button size="sm" variant="outline" disabled={!row.adopted || editor.busy} onClick={() => setEditing(true)}><Pencil className="size-4" aria-hidden /> {network ? 'Edit network' : 'Add network'}</Button>}
          {!row.adopted && !row.loopback && <p className="text-xs text-muted-foreground">Give this interface to the router first to configure a network.</p>}
        </CardContent>
      </Card>
    )}

    {network && gateway.config && <Card>
      <CardHeader><CardTitle>Internet via</CardTitle></CardHeader>
      <CardContent className="space-y-3">
        <p className="text-sm text-muted-foreground">Where devices on {network.name} go. {observed?.reason || (assignment ? 'This interface has its own route.' : 'Following the Gateway default.')}</p>
        <Select value={assignment ? assignment.exit || DIRECT : INHERIT} disabled={gateway.busy} onValueChange={(value) => {
          if (!value || value === INHERIT) {
            if (assignment) gateway.change(gatewayChange.removeAssignment(row.name))
          } else gateway.change(gatewayChange.assign(row.name, value === DIRECT ? '' : value))
        }}>
          <SelectTrigger className="w-full sm:w-80" aria-label={`Internet via for ${row.name}`}><SelectValue>{(value: string) => value === INHERIT ? `Follow Gateway default (${gateway.config?.default || 'router connection'})` : value === DIRECT ? 'This router’s own connection' : value}</SelectValue></SelectTrigger>
          <SelectContent><SelectItem value={INHERIT}>Follow Gateway default</SelectItem><SelectItem value={DIRECT}>This router&rsquo;s own connection</SelectItem>{gateway.config.exits?.map((exit) => <SelectItem key={exit.name} value={exit.name}>{exit.name}</SelectItem>)}</SelectContent>
        </Select>
        {!gateway.config.enabled && <p className="text-xs text-muted-foreground">Gateway rules are currently off.</p>}
      </CardContent>
    </Card>}
    {gateway.gate}
    <ApplyOutcome applier={gateway.applier} />
    <PartialApply editor={editor} />
    {editing && <NetworkDialog key={initial?.name ?? row.name} open={editing} onOpenChange={setEditing} initial={initial} interfaces={editor.interfaces.filter((item) => item.name === row.name)} taken={new Set(stored.flatMap((item) => item.members))} onSubmit={save} onRemove={initial ? () => { editor.save(stored.filter((item) => item.name !== initial.name)); setEditing(false) } : undefined} />}
    <ConfirmDisruptive editor={editor} />
    <Link to="/gateway#dhcp" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">Served networks <ArrowUpRight className="size-4" aria-hidden /></Link>
  </div>
}

function Fact({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return <div className="min-w-0 rounded-xl border bg-card px-4 py-3"><p className="text-xs text-muted-foreground">{label}</p><p className={`mt-1 break-all text-sm font-medium ${mono ? 'font-mono' : ''}`}>{value}</p></div>
}

function State({ title, detail }: { title: string; detail: string }) {
  return <div className="mx-auto max-w-5xl space-y-3 py-16 text-center"><h1 className="text-xl font-semibold">{title}</h1><p className="text-sm text-muted-foreground">{detail}</p><Button variant="outline" render={<Link to="/gateway">Back to Gateway</Link>} /></div>
}
