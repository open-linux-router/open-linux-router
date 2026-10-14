import { Input } from '@/components/ui/input'
import { useRef, useState } from 'react'
import { Plus, Search, X } from 'lucide-react'
import { Link, useParams } from 'react-router'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableRow } from '@/components/ui/table'
import { useDnsConfig, useDnsQueries } from '@/features/dns/queries'
import { DeviceInspection } from '@/features/devices/inspection'
import { DeviceActivity } from '@/features/devices/activity'
import { DeviceInlineEditor } from '@/features/devices/device-inline-editor'
import { useDeviceList } from '@/features/devices/queries'
import { useGatewayTraffic } from '@/features/gateway/queries'
import { DevicePolicy } from '@/features/qos/device-policy'
import { useTrafficView } from '@/features/topology/traffic'
import { useIngressConfig, useIngressStatus } from '@/features/ingress/queries'
import { ServiceIcon, serviceIconVersion } from '@/features/topology/nodes'
import { formatBytes } from '@/lib/utils'

export function DevicePage() {
  const { mac } = useParams()
  const devices = useDeviceList()
  const traffic = useGatewayTraffic()
  const ingress = useIngressConfig()
  const ingressStatus = useIngressStatus()
  const serviceDomain = ingressStatus.data?.domain
  const dnsConfig = useDnsConfig()
  const [dnsInspection, setDnsInspection] = useState(false)
  const [searchOpen, setSearchOpen] = useState(false)
  const searchInput = useRef<HTMLInputElement>(null)
  const searchButton = useRef<HTMLButtonElement>(null)
  const dnsQueries = useDnsQueries(dnsInspection)
  const flow = useTrafficView(traffic.data, traffic.isError)
  const [filter, setFilter] = useState('')
  const device = devices.data?.devices.find((d) => d.mac.toLowerCase() === mac?.toLowerCase())
  const usage = device && flow.flowOf(device)
  const services = (ingress.data?.services ?? []).filter((s) =>
    (device?.name && s.upstream.device === device.name) ||
    (s.upstream.host && [device?.fixed_ip, ...(device?.ips ?? [])].some((ip) => ip?.toLowerCase() === s.upstream.host?.toLowerCase())),
  )
  const addresses = new Set(device?.ips?.map((ip) => ip.toLowerCase()) ?? [])
  const matchingQueries = dnsQueries.data?.queries.filter((q) => addresses.has(q.client.toLowerCase())) ?? []
  const shownQueries = matchingQueries.filter((q) => !filter.trim() || [q.name, q.type, q.rcode, q.policy ?? '', ...(q.answers ?? [])].some((value) => value.toLowerCase().includes(filter.trim().toLowerCase())))

  if (devices.isPending) return <p className="py-16 text-sm text-muted-foreground">Loading device…</p>
  if (devices.isError) return <State title="Could not load devices" detail="Try again when the router is reachable." />
  if (!device) return <State title="Device not found" detail="It may have been forgotten or is no longer in the device list." />

  return (
    <div className="space-y-4 pb-12">
      <DeviceInlineEditor key={device.mac} device={device} services={
        <div className="flex flex-wrap items-center justify-end gap-2">
          {ingress.isError ? <span className="text-destructive">Unavailable</span> : ingress.isPending ? <span className="text-muted-foreground">Loading…</span> : services.map((service) => {
            const version = serviceIconVersion(service, serviceDomain)
            const icon = <ServiceIcon key={version} name={service.name} version={version} choice={service.icon} />
            const label = `${service.name}.${serviceDomain}${service.link_path ?? ''}`
            const className = 'flex size-7 shrink-0 items-center justify-center rounded-md bg-muted transition-colors hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring'
            return serviceDomain ? <a key={service.name} href={`https://${label}`} target="_blank" rel="noopener noreferrer" title={`Open ${label}`} aria-label={`Open ${label}`} className={className}>{icon}</a>
              : <span key={service.name} title={ingressStatus.isError ? `${service.name}: service address unavailable` : service.name} className={className}>{icon}</span>
          })}
          <Link to={`/access/ingress?device=${encodeURIComponent(device.mac)}`} aria-label="Add service for this device" title="Add service" className="flex size-7 shrink-0 items-center justify-center rounded-md border transition-colors hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring"><Plus className="size-4" /></Link>
        </div>
      } total={<span className="inline-flex flex-wrap justify-end gap-3 tabular-nums" title="Traffic through this router since counters were reset">
        <span className="inline-flex items-center gap-0.5" aria-label="Download total"><span aria-hidden className="text-base font-normal text-muted-foreground">↓</span>{flow.counting ? formatBytes(usage?.down ?? 0) : '—'}</span>
        <span className="inline-flex items-center gap-0.5 text-muted-foreground" aria-label="Upload total"><span aria-hidden className="text-base font-normal text-muted-foreground">↑</span>{flow.counting ? formatBytes(usage?.up ?? 0) : '—'}</span>
      </span>} activity={<DeviceActivity addresses={device.ips ?? []} downRate={usage?.downRate} upRate={usage?.upRate} />} traffic={
        <DevicePolicy mac={device.mac} downRate={usage?.downRate} upRate={usage?.upRate}>
            {!flow.counting && (
              <p className="text-sm text-muted-foreground">{traffic.isError ? 'Traffic counters are unavailable right now.' : 'Traffic counting is off or unavailable.'}</p>
            )}
        </DevicePolicy>
      } />

      <Card className="gap-0 py-0 [overflow-anchor:none]">
        <CardHeader className="mx-4 flex h-18 flex-row items-center justify-between gap-3 rounded-none px-0 sm:mx-6">
          <CardTitle>DNS</CardTitle>
          <div className="flex items-center gap-3">
            <label htmlFor="device-dns-inspection" className="sr-only text-sm sm:not-sr-only">Inspection</label>
            <Switch id="device-dns-inspection" checked={dnsInspection} onCheckedChange={(checked) => { setDnsInspection(checked); setSearchOpen(false); setFilter('') }} />
            {dnsInspection && <Button ref={searchButton} className="order-last shrink-0" variant="ghost" size="icon" aria-label={searchOpen ? 'Close DNS search' : 'Search DNS queries'} aria-expanded={searchOpen} aria-controls="device-dns-search" onClick={() => { setSearchOpen(!searchOpen); setFilter(''); if (!searchOpen) requestAnimationFrame(() => searchInput.current?.focus()) }}>{searchOpen ? <X className="size-5" /> : <Search className="size-5" />}</Button>}
          {dnsInspection && <div id="device-dns-search" className={`overflow-hidden transition-[width,opacity] duration-200 motion-reduce:transition-none ${searchOpen ? 'w-28 opacity-100 sm:w-56' : 'w-0 opacity-0'}`} inert={!searchOpen}>
            <div className="overflow-hidden">
              <Input ref={searchInput} id="device-dns-filter" aria-label="Filter DNS queries" className="w-full" value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Search queries" onKeyDown={(e) => { if (e.key === 'Escape') { setSearchOpen(false); setFilter(''); searchButton.current?.focus() } }} />
            </div>
          </div>}
          </div>
        </CardHeader>
        {dnsInspection && <CardContent className="mx-4 space-y-4 border-t px-0 py-4 sm:mx-6 sm:py-6">
          {dnsQueries.isError ? (
            <p className="text-sm text-muted-foreground">The DNS query log is unavailable. The resolver may be stopped.</p>
          ) : dnsQueries.isPending ? (
            <p className="text-sm text-muted-foreground">Loading recent queries…</p>
          ) : dnsConfig.isError ? (
            <p role="alert" className="text-sm text-muted-foreground">The DNS query log configuration is unavailable.</p>
          ) : dnsConfig.isPending ? (
            <p className="text-sm text-muted-foreground">Loading DNS configuration…</p>
          ) : !dnsConfig.data?.query_log.enabled ? (
            <p className="text-sm text-muted-foreground">The DNS query log is off. Queries are answered but not kept.</p>
          ) : shownQueries.length === 0 ? (
            <p className="text-sm text-muted-foreground">{matchingQueries.length ? 'Nothing matches that filter.' : 'No matching queries in the recent sample.'}</p>
          ) : (
            <div className="max-h-80 overflow-y-auto">
              <Table aria-label="Recent DNS queries: time, record, type, answer" className="[&_td:last-child]:text-right">
                <TableBody>{shownQueries.map((q, i) => <TableRow key={`${q.at}-${q.client}-${q.name}-${i}`}>
                  <TableCell><time dateTime={q.at} className="font-mono text-xs text-muted-foreground">{new Date(q.at).toLocaleTimeString()}</time></TableCell>
                  <TableCell className="min-w-32 whitespace-normal break-all">{q.name}</TableCell>
                  <TableCell className="text-muted-foreground">{q.type}</TableCell>
                  <TableCell className="min-w-40 whitespace-normal break-all">{q.blocked ? <Badge variant="secondary">Blocked{q.policy ? ` · ${q.policy}` : ''}</Badge> : q.rcode !== 'NOERROR' ? <Badge variant="warning">{q.rcode}</Badge> : q.answers?.length ? <DnsAnswers answers={q.answers} /> : <span className="text-xs text-muted-foreground">No answer</span>}</TableCell>
                </TableRow>)}</TableBody>
              </Table>
            </div>
          )}
        </CardContent>}
      </Card>

      <DeviceInspection key={device.mac} mac={device.mac} addresses={device.ips ?? []} />
    </div>
  )
}

function State({ title, detail }: { title: string; detail: string }) {
  return <div className="space-y-3 py-16 text-center">
    <h1 className="text-xl font-semibold">{title}</h1>
    <p className="text-sm text-muted-foreground">{detail}</p>
    <Button variant="outline" nativeButton={false} render={<Link to="/">Back to overview</Link>} />
  </div>
}

function DnsAnswers({ answers }: { answers: string[] }) {
  return <span className="font-mono text-xs text-muted-foreground">{answers.join(', ')}</span>
}
