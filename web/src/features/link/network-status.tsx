import { AlertTriangle, ChevronRight } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { ListEmpty } from '@/components/ui/list'
import { Skeleton } from '@/components/ui/skeleton'
import { useDhcpLeases, useDhcpStatus } from '@/features/dhcp/queries'
import { useUplink } from '@/features/dial/queries'
import { useDnsStatus } from '@/features/dns/queries'
import { useInterfaceTraffic, type RateSample } from '@/features/link/interface-traffic'
import { NetworkIcon } from '@/features/link/network-icon'
import { useInterfaces } from '@/features/link/queries'
import type { InterfaceRow } from '@/lib/api-types'
import { cn, formatBytes, formatRate } from '@/lib/utils'

const card = 'rounded-xl border bg-card text-card-foreground shadow-xs'
const detailLink = 'inline-flex min-h-11 items-center gap-1 text-sm font-medium text-primary hover:underline underline-offset-4 focus-visible:outline-2 focus-visible:outline-ring focus-visible:outline-offset-4'

export function NetworkStatus() {
  const interfaces = useInterfaces()
  const uplink = useUplink()
  const dhcp = useDhcpStatus()
  const leases = useDhcpLeases()
  const dns = useDnsStatus()
  const traffic = useInterfaceTraffic(interfaces.data, interfaces.isError)
  const [selected, setSelected] = useState<string | null>(null)
  const rows = (interfaces.data?.interfaces ?? []).filter(row => row.physical)
  const chosen = rows.find(row => row.name === selected)
  const wan = uplink.isError ? undefined : uplink.data?.uplink?.interface
  const roleOf = (row: InterfaceRow) => {
    if (row.name === wan) return 'WAN'
    if (row.network || interfaces.data?.networks.some(n => n.members.includes(row.name))) return 'LAN'
    return undefined
  }
  const dhcpState = dhcp.isError ? 'Unavailable' : !dhcp.data ? 'Loading…' : !dhcp.data.enabled ? 'Off'
    : dhcp.data.service_error ? 'Unavailable'
      : dhcp.data.drift_error || dhcp.data.blockers?.length ? 'Needs attention'
        : dhcp.data.service?.active ? dhcp.data.drifted ? 'Needs attention' : 'Running' : 'Not running'
  const dnsState = dns.isError ? 'Unavailable' : !dns.data ? 'Loading…' : !dns.data.enabled ? 'Off'
    : dns.data.services.some(service => service.error) ? 'Unavailable'
      : dns.data.drift_error || dns.data.blockers?.length ? 'Needs attention'
      : dns.data.services.length > 0 && dns.data.services.every(service => service.status?.active)
        ? dns.data.drifted ? 'Needs attention' : 'Running' : 'Not running'
  const activeLeases = leases.isError ? undefined : leases.data?.leases.filter(lease => lease.active).length
  const queries = dns.isError || dns.data?.stats_error ? undefined : dns.data?.stats?.queries

  return (
    <div className="space-y-8">
      <section id="interfaces" aria-labelledby="interfaces-heading" className="scroll-mt-20">
        <h2 id="interfaces-heading" className="sr-only">Interfaces</h2>
        <div>
          {interfaces.isPending && <div className="grid gap-4 min-[1000px]:grid-cols-3">
            {[0, 1, 2].map(i => <Skeleton key={i} className="h-[280px] rounded-2xl" />)}
          </div>}
          {interfaces.isError && <Alert variant="destructive"><AlertTriangle /><AlertTitle>Could not load interfaces</AlertTitle><AlertDescription>{interfaces.error.message}</AlertDescription></Alert>}
          {interfaces.isSuccess && rows.length === 0 && <ListEmpty>No physical network interfaces found. <Link to="/gateway/interfaces" className="underline underline-offset-4">Open interface settings</Link></ListEmpty>}
          {rows.length > 0 && <div className="grid gap-4 min-[1000px]:grid-cols-3">
            {rows.map(row => <InterfaceCard key={row.name} row={row} role={roleOf(row)} history={traffic.get(row.name)?.history ?? []} failed={interfaces.isError} onSelect={() => setSelected(row.name)} />)}
          </div>}
          {!uplink.isPending && uplink.isError && <p className="mt-3 text-xs text-muted-foreground">WAN assignment unavailable. <Link to="/gateway/interfaces" className="underline underline-offset-4">Review settings</Link></p>}
        </div>
      </section>

      <div className="grid gap-4 sm:grid-cols-2">
        <ServiceCard kind="dhcp" title="DHCP" value={activeLeases} pending={leases.isPending} label="active leases" state={dhcpState} />
        <ServiceCard kind="dns" title="DNS" value={queries} pending={dns.isPending} label="queries since restart" state={dnsState} />
      </div>

      <Dialog open={selected !== null} onOpenChange={open => !open && setSelected(null)}>
        <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{selected}</DialogTitle>
            <DialogDescription>{interfaces.isError ? 'Interface readings unavailable.' : chosen ? `${connectionState(chosen, roleOf(chosen))}${roleOf(chosen) ? ` · ${roleOf(chosen)}` : ''}` : 'This interface is no longer present.'}</DialogDescription>
          </DialogHeader>
          {interfaces.isError && <p className="text-sm text-destructive">Interface readings unavailable.</p>}
          {chosen && <dl className="divide-y text-sm">
            <Detail label="Network" value={chosen.network ?? interfaces.data?.networks.find(n => n.members.includes(chosen.name))?.name ?? 'Not assigned'} />
            <Detail label="Physical link" value={interfaces.isError ? 'Unavailable' : !chosen.present ? 'Not found' : !chosen.up ? 'Down' : chosen.running ? 'Detected' : 'Disconnected'} />
            <Detail label="Link speed" value={interfaces.isError ? '—' : linkSpeed(chosen.speed_mbps)} />
            <Detail label="MAC" value={chosen.mac ?? '—'} />
            <Detail label="Addresses" value={chosen.prefixes?.join(', ') || 'Not assigned'} />
            <Detail label="Receive total" value={!interfaces.isError && chosen.rx_bytes !== undefined ? formatBytes(chosen.rx_bytes) : '—'} />
            <Detail label="Transmit total" value={!interfaces.isError && chosen.tx_bytes !== undefined ? formatBytes(chosen.tx_bytes) : '—'} />
          </dl>}
          {chosen && !interfaces.isError && !chosen.speed_mbps && <p className="text-sm text-muted-foreground">Link speed is unavailable from the system. Some drivers, especially virtual network drivers, do not report it. Traffic rates are measured separately.</p>}
          <Link to={chosen ? `/gateway/interfaces/${encodeURIComponent(chosen.name)}` : '/gateway/interfaces'} className={detailLink}>Interface settings <ChevronRight className="size-3.5" aria-hidden /></Link>
          <Link to="/gateway/interfaces" className={detailLink}>All interfaces and routing <ChevronRight className="size-3.5" aria-hidden /></Link>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function InterfaceCard({ row, role, history, failed, onSelect }: {
  row: InterfaceRow; role?: string; history: RateSample[]; failed: boolean; onSelect: () => void
}) {
  const connected = connectionState(row, role) === 'Connected'
  const current = failed ? undefined : history.at(-1)
  return (
    <button type="button" onClick={onSelect} aria-label={`${row.name}${role ? ` ${role}` : ''} details`}
      className={cn(card, 'flex min-h-[280px] w-full min-w-0 cursor-pointer flex-col overflow-hidden p-6 text-left transition-shadow hover:shadow-md focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring')}>
      <div className="flex items-center gap-3">
        <span className="relative block size-16 shrink-0" aria-hidden="true">
          <NetworkIcon kind="interface" className={cn('size-full', (!connected || failed) && 'opacity-50 grayscale')} />
          {role && <span className="absolute left-1/2 top-[56.25%] -translate-x-1/2 -translate-y-1/2 text-[9px] leading-none font-bold text-[#3e4a63]">{role}</span>}
          <span className={cn('absolute left-[24.22%] top-[72.66%] size-[7.03%] -translate-x-1/2 -translate-y-1/2 rounded-[1px]', connected && !failed ? 'bg-[#38975f]' : 'bg-muted-foreground/40')} />
          <span className={cn('absolute left-[75.78%] top-[72.66%] size-[7.03%] -translate-x-1/2 -translate-y-1/2 rounded-[1px]', connected && !failed ? 'bg-[#cba34a]' : 'bg-muted-foreground/40')} />
        </span>
        <div className="min-w-0">
          <strong className="block break-all text-base font-semibold">{row.name}</strong><span className={cn('text-sm', connected && !failed ? 'text-success-foreground' : 'text-muted-foreground')}>{failed ? 'Unavailable' : connectionState(row, role)}</span>
        </div>
      </div>
      <div className="mt-4 grid grid-cols-[minmax(0,1fr)_minmax(0,1.5fr)] gap-4">
        <Fact label="Link speed" value={failed ? '—' : linkSpeed(row.speed_mbps)} />
        <Fact label="IP" value={failed ? '—' : row.address || row.prefixes?.[0]?.split('/')[0] || 'Not assigned'} mono />
      </div>
      <div className="mt-4 grid grid-cols-[minmax(0,1fr)_minmax(0,1.5fr)] gap-4 text-sm font-medium tabular-nums">
        <span className="text-receive" aria-label={`Receive ${current ? formatRate(current.rx) : 'unavailable'}`}>↓ {current ? formatRate(current.rx) : '—'}</span>
        <span className="text-transmit" aria-label={`Transmit ${current ? formatRate(current.tx) : 'unavailable'}`}>↑ {current ? formatRate(current.tx) : '—'}</span>
      </div>
      <div className="mt-auto pt-3">
        <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
          <span>Last 60 s</span><span className="flex gap-3"><span className="flex items-center gap-1"><i className="h-0.5 w-3 bg-receive" />Receive</span><span className="flex items-center gap-1"><i className="h-0.5 w-3 bg-transmit" />Transmit</span></span>
        </div>
        <TrafficChart history={failed ? [] : history} message={failed ? 'Traffic unavailable' : !connected ? connectionState(row, role) : row.rx_bytes === undefined || row.tx_bytes === undefined ? 'Traffic unavailable' : 'Measuring…'} />
      </div>
    </button>
  )
}

function TrafficChart({ history, message }: { history: RateSample[]; message: string }) {
  if (history.length < 2) return <div className="flex h-[76px] items-center text-xs text-muted-foreground">{history.length === 1 ? 'Collecting traffic history…' : message}</div>
  const newest = history.at(-1)!.at
  const peak = Math.max(1, ...history.flatMap(sample => [sample.rx, sample.tx]))
  const points = (key: 'rx' | 'tx') => history.map(sample => `${((sample.at - newest + 60_000) / 60_000 * 300).toFixed(1)},${(68 - sample[key] / peak * 56).toFixed(1)}`).join(' ')
  return <svg className="mt-1 h-[76px] w-full" viewBox="0 0 300 76" preserveAspectRatio="none" role="img" aria-label="Receive and transmit rates observed over the last 60 seconds">
    <polyline points={points('rx')} fill="none" stroke="var(--receive)" strokeWidth="1.8" vectorEffect="non-scaling-stroke" strokeLinecap="round" strokeLinejoin="round" />
    <polyline points={points('tx')} fill="none" stroke="var(--transmit)" strokeWidth="1.8" vectorEffect="non-scaling-stroke" strokeLinecap="round" strokeLinejoin="round" />
  </svg>
}

function ServiceCard({ kind, title, value, pending, label, state }: {
  kind: 'dhcp' | 'dns'; title: string; value?: number; pending: boolean; label: string; state: string
}) {
  const healthy = state === 'Running'
  const problem = ['Unavailable', 'Not running', 'Needs attention'].includes(state)
  return <section id={kind} className={cn(card, 'scroll-mt-20 flex min-h-[186px] flex-col px-6 py-5')} aria-label={title}>
    <div className="flex items-center gap-3"><NetworkIcon kind={kind} className="size-12 shrink-0" /><h2 className="text-base font-semibold">{title}</h2></div>
    <div className="mt-3 flex flex-wrap items-baseline gap-2">
      {pending ? <Skeleton className="h-7 w-20" /> : <strong className="text-2xl font-semibold tabular-nums">{value?.toLocaleString() ?? '—'}</strong>}
      <span className="text-xs text-muted-foreground">{label}</span>
    </div>
    <div className="mt-auto flex items-center justify-between gap-3 pt-2">
      <span className={cn('flex items-center gap-2 text-xs', healthy ? 'text-success-foreground' : problem ? 'text-warning-foreground' : 'text-muted-foreground')}><span className={cn('size-1.5 rounded-full', healthy ? 'bg-success' : problem ? 'bg-warning' : 'bg-muted-foreground/40')} aria-hidden />{state}</span>
      <Link to={`/gateway/${kind}/details`} className={cn(detailLink, 'text-xs')} aria-label={`${title} details`}>Details <ChevronRight className="size-3.5" aria-hidden /></Link>
    </div>
  </section>
}

function Fact({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return <div className="min-w-0"><span className="block text-xs text-muted-foreground">{label}</span><strong className={cn('mt-1 block text-xs font-medium [overflow-wrap:anywhere]', mono && 'font-mono')}>{value}</strong></div>
}

function Detail({ label, value }: { label: string; value: string }) {
  return <div className="flex justify-between gap-4 py-3"><dt className="shrink-0 text-muted-foreground">{label}</dt><dd className="text-right [overflow-wrap:anywhere]">{value}</dd></div>
}

function connectionState(row: InterfaceRow, role?: string) {
  return !row.present ? 'Not found' : !row.up ? 'Down' : !row.running ? 'Disconnected'
    : !role && !row.network && !row.address && !row.prefixes?.length ? 'Inactive' : 'Connected'
}

function linkSpeed(mbps?: number) {
  return !mbps ? 'Unknown' : mbps >= 1000 ? `${mbps / 1000} Gbps` : `${mbps} Mbps`
}
