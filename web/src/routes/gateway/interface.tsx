import { t, useLocale } from '@/lib/i18n'
import { Heading } from '@/components/ui/heading'
import { TechnicalValue } from '@/components/ui/property-row'
import { ArrowUpRight, Pencil } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { Link, useParams } from 'react-router'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { SwitchField } from '@/components/ui/editable-field'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useDhcpConfig, useDhcpLeases } from '@/features/dhcp/queries'
import { useUplink } from '@/features/dial/queries'
import { UplinkCard } from '@/features/dial/uplink-card'
import { ApplyOutcome, useGatewayEditor } from '@/features/gateway/editor'
import { DIRECT, INHERIT } from '@/features/gateway/network-list'
import { gatewayChange, useGatewayStatus } from '@/features/gateway/queries'
import { InterfacesCard } from '@/features/link/interfaces-card'
import { useInterfaceTraffic } from '@/features/link/interface-traffic'
import { InterfaceVisual, interfaceState } from '@/features/link/interface-visual'
import { NetworkDialog } from '@/features/link/network-dialog'
import { useInterfaces } from '@/features/link/queries'
import { useNetworkEditor } from '@/features/link/use-networks'
import { ConfirmDisruptive, PartialApply } from '@/routes/gateway/networks'
import type { Network } from '@/lib/config-types'
import { formatBytes, formatRate } from '@/lib/utils'

/**
 * One interface, on its own page.
 *
 * The Gateway landing page's cards come here directly: a card can carry a name,
 * a role, an address and a rate, and everything else an operator asks an
 * interface about — its negotiated speed, every address, what it has moved
 * since the kernel created it, the network and address range it serves, the
 * internet route it is given — belongs beside the switches that hand the
 * interface to olr. That is why this is a page and not a dialog.
 */
export function GatewayInterfacePage() {
  useLocale()
  const { name } = useParams()
  const editor = useNetworkEditor()
  const gateway = useGatewayEditor()
  const status = useGatewayStatus()
  const uplink = useUplink()
  const dhcp = useDhcpConfig()
  const leases = useDhcpLeases()
  const interfaces = useInterfaces()
  const traffic = useInterfaceTraffic(interfaces.data, interfaces.isError)
  const [editing, setEditing] = useState(false)
  const row = editor.interfaces.find((item) => item.name === name)
  const network = editor.networks.find((item) => item.members.includes(name ?? ''))
  const stored = editor.config?.networks ?? []
  const initial = stored.find((item) => item.name === network?.name)
  const isUplink = name === uplink.data?.uplink?.interface
  const assignment = gateway.config?.interfaces?.find((item) => item.interface === name)
  const observed = status.data?.assignments?.find((item) => item.interface === name)
  const rates = name ? traffic.get(name)?.history.at(-1) : undefined
  const pool = dhcp.data?.pools?.find((item) => item.network === network?.name)
  const usage = leases.data?.usage.find((item) => item.network === network?.name)

  if (editor.isPending) return <p className="py-16 text-sm text-muted-foreground">{t('Loading interface…')}</p>
  if (editor.error) return <State title={t('Could not load interface')} detail={editor.error.message} />
  if (!row) return <State title={t('Interface not found')} detail="It may have been removed from this machine." />

  const state = interfaceState(row)
  const carrying = row.present && row.up && row.running
  function save(next: Network) {
    editor.save([...stored.filter((item) => item.name !== initial?.name), next])
    setEditing(false)
  }

  return <div className="space-y-6 pb-12">
    <header className="flex flex-wrap items-start gap-4">
      <InterfaceVisual row={row} uplink={isUplink} />
      <div className="min-w-0 flex-1">
        <Heading as="h1" size="lg" className="font-mono">{row.name}</Heading>
        <p className="text-sm text-muted-foreground">{isUplink ? t('Internet uplink') : network ? `${network.name} network` : row.loopback ? 'Local interface' : 'Network interface'}</p>
      </div>
      <span className={`text-sm font-medium ${state.tone}`}>{state.label}</span>
    </header>

    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <Fact label={t('Link')} value={row.present ? row.up ? row.running ? 'Up · carrier detected' : 'Up · no carrier' : t('Down') : 'Not present'} />
      <Fact label={t('Link speed')} value={linkSpeed(row.speed_mbps)} />
      <Fact label={t('Address')} value={row.prefixes?.join(', ') || 'None'} mono />
      <Fact label={t('Hardware address')} value={row.mac || 'Unavailable'} mono />
      <Fact label={t('Traffic')} value={!interfaces.isError && carrying && rates
        ? <TrafficPair rx={`↓ ${formatRate(rates.rx)}`} tx={`↑ ${formatRate(rates.tx)}`} rxLabel={t('Receive {rate}', { rate: formatRate(rates.rx) })} txLabel={t('Transmit {rate}', { rate: formatRate(rates.tx) })} />
        : <span className="text-muted-foreground">{interfaces.isError ? t('No readings') : !carrying ? t('No traffic') : t('Measuring…')}</span>} />
      <Fact label={t('Total')} value={interfaces.isError || row.rx_bytes === undefined || row.tx_bytes === undefined
        ? '—'
        : <TrafficPair rx={`↓ ${formatBytes(row.rx_bytes)}`} tx={`↑ ${formatBytes(row.tx_bytes)}`} rxLabel={t('Total received {total}', { total: formatBytes(row.rx_bytes) })} txLabel={t('Total sent {total}', { total: formatBytes(row.tx_bytes) })} />} />
    </div>

    {!row.loopback && row.present && !row.speed_mbps && !interfaces.isError && (
      <p className="text-sm text-muted-foreground">
        {t('Link speed is unavailable from the system. Some drivers, especially virtual network drivers, do not report it. Traffic rates are measured separately.')}
      </p>
    )}

    <section className="space-y-2">
      <Heading>{t('Management')}</Heading>
      <p className="text-sm text-muted-foreground">{t('Allow OLR to configure this interface. Enabling management alone does not change its address.')}</p>
      <InterfacesCard dhcp={dhcp.data} only={row.name} />
    </section>

    {!row.loopback && editor.config && <section className="space-y-3">
      <Heading>{t('IPv6 addressing')}</Heading>
        <SwitchField
          id="interface-ipv6-auto"
          label={t('Automatically obtain IPv6 addresses')}
          checked={editor.config.ipv6_auto?.[row.name] !== false}
          busy={editor.busy || !row.adopted || !row.present}
          hint={editor.config.ipv6_auto?.[row.name] === undefined
            ? 'Not managed yet — uses this interface’s host settings. Switching on or off hands automatic IPv6 addressing to OLR.'
            : 'Controls SLAAC and DHCPv6 address acquisition. Static addresses stay configured; existing dynamic addresses may remain until their leases expire.'}
          onChange={(enabled) => editor.submit({ ...editor.config!, ipv6_auto: { ...editor.config!.ipv6_auto, [row.name]: enabled } })}
        />
        {!row.adopted && <p className="text-sm text-muted-foreground">{t('Allow OLR to manage this interface first.')}</p>}
    </section>}

    {isUplink ? <section className="space-y-2"><Heading>{t('Internet uplink')}</Heading><UplinkCard interfaces={editor.interfaces} networks={editor.networks} /></section> : (
      <Card>
        <CardHeader><CardTitle>{t('Network')}</CardTitle></CardHeader>
        <CardContent className="space-y-4">
          {network ? <div className="space-y-1 text-sm">
            <p className="font-medium">{network.name}</p>
            <p className="text-muted-foreground">{network.subnet || 'No IPv4 subnet'}{network.router ? ` · Router ${network.router}` : ''}</p>
            {network.subnet6 && <p className="text-muted-foreground">IPv6 {network.subnet6}</p>}
            {pool?.ipv4?.start && <p className="text-muted-foreground">{usage
              ? t('DHCP {range} · {active} of {size} in use', { range: poolRange(pool.ipv4.start, pool.ipv4.end), active: usage.active, size: usage.size })
              : t('DHCP {range}', { range: poolRange(pool.ipv4.start, pool.ipv4.end) })}</p>}
          </div> : <p className="text-sm text-muted-foreground">{t('No network is configured on this interface.')}</p>}
          {!row.loopback && <Button size="sm" variant="outline" disabled={!row.adopted || editor.busy} onClick={() => setEditing(true)}><Pencil className="size-4" aria-hidden /> {network ? 'Edit network' : 'Add network'}</Button>}
          {!row.adopted && !row.loopback && <p className="text-xs text-muted-foreground">Give this interface to the router first to configure a network.</p>}
        </CardContent>
      </Card>
    )}

    {network && gateway.config && <Card>
      <CardHeader><CardTitle>{t('Internet via')}</CardTitle></CardHeader>
      <CardContent className="space-y-3">
        <p className="text-sm text-muted-foreground">{observed?.reason || (assignment ? 'Interface override' : 'Following the Gateway default.')}</p>
        <Select value={assignment ? assignment.exit || DIRECT : INHERIT} disabled={gateway.busy} onValueChange={(value) => {
          if (!value || value === INHERIT) {
            if (assignment) gateway.change(gatewayChange.removeAssignment(row.name))
          } else gateway.change(gatewayChange.assign(row.name, value === DIRECT ? '' : value))
        }}>
          <SelectTrigger className="w-full sm:w-80" aria-label={`Internet via for ${row.name}`}><SelectValue>{(value: string) => value === INHERIT ? `Follow Gateway default (${gateway.config?.default || 'router connection'})` : value === DIRECT ? t('This router’s own connection') : value}</SelectValue></SelectTrigger>
          <SelectContent><SelectItem value={INHERIT}>{t('Follow Gateway default')}</SelectItem><SelectItem value={DIRECT}>{t('This router&rsquo;s own connection')}</SelectItem>{gateway.config.exits?.map((exit) => <SelectItem key={exit.name} value={exit.name}>{exit.name}</SelectItem>)}</SelectContent>
        </Select>
        {!gateway.config.enabled && <p className="text-xs text-muted-foreground">{t('Gateway rules are currently off.')}</p>}
      </CardContent>
    </Card>}
    {gateway.gate}
    <ApplyOutcome applier={gateway.applier} />
    <PartialApply editor={editor} />
    {editing && <NetworkDialog key={initial?.name ?? row.name} open={editing} onOpenChange={setEditing} initial={initial} interfaces={editor.interfaces.filter((item) => item.name === row.name)} taken={new Set(stored.flatMap((item) => item.members))} onSubmit={save} onRemove={initial ? () => { editor.save(stored.filter((item) => item.name !== initial.name)); setEditing(false) } : undefined} />}
    <ConfirmDisruptive editor={editor} />
    <Link to="/gateway/dhcp" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">{t('Served networks')} <ArrowUpRight className="size-4" aria-hidden /></Link>
  </div>
}

/** One reading per tile, with the rate and the total kept apart: the rate is
 *  what the interface is doing now, the byte count is everything it has moved
 *  since the kernel created it. */
function Fact({ label, value, mono = false }: { label: string; value: ReactNode; mono?: boolean }) {
  useLocale()
  return <div className="panel-surface min-w-0 p-4 sm:p-6"><p className="text-sm text-muted-foreground">{label}</p><div className="mt-1 flex flex-wrap items-baseline gap-x-4 gap-y-1 text-sm">{mono ? <TechnicalValue>{value}</TechnicalValue> : value}</div></div>
}

function TrafficPair({ rx, tx, rxLabel, txLabel }: { rx: string; tx: string; rxLabel: string; txLabel: string }) {
  return <>
    <span className="font-medium tabular-nums text-receive" aria-label={rxLabel}>{rx}</span>
    <span className="font-medium tabular-nums text-transmit" aria-label={txLabel}>{tx}</span>
  </>
}

function poolRange(start: string, end?: string) {
  return end ? `${start}–${end}` : start
}

function linkSpeed(mbps?: number) {
  return !mbps ? t('Unknown') : mbps >= 1000 ? `${mbps / 1000} Gbps` : `${mbps} Mbps`
}

function State({ title, detail }: { title: string; detail: string }) {
  useLocale()
  return <div className="space-y-3 py-16 text-center"><Heading as="h1" size="lg">{title}</Heading><p className="text-sm text-muted-foreground">{detail}</p><Button variant="outline" nativeButton={false} render={<Link to="/gateway">{t('Back to Gateway')}</Link>} /></div>
}
