import { Activity, ArrowDown, ArrowUp } from 'lucide-react'
import { Input } from '@/components/ui/input'
import { useState } from 'react'
import { Link, useParams } from 'react-router'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { useDnsConfig, useDnsQueries } from '@/features/dns/queries'
import { DeviceInspection } from '@/features/devices/inspection'
import { DeviceInlineEditor } from '@/features/devices/device-inline-editor'
import { useDeviceList } from '@/features/devices/queries'
import { useGatewayTraffic } from '@/features/gateway/queries'
import { DevicePolicy } from '@/features/qos/device-policy'
import { useTrafficView } from '@/features/topology/traffic'
import { formatBytes, formatRate } from '@/lib/utils'

export function DevicePage() {
  const { mac } = useParams()
  const devices = useDeviceList()
  const traffic = useGatewayTraffic()
  const dnsConfig = useDnsConfig()
  const dnsQueries = useDnsQueries()
  const flow = useTrafficView(traffic.data, traffic.isError)
  const [filter, setFilter] = useState('')
  const device = devices.data?.devices.find((d) => d.mac.toLowerCase() === mac?.toLowerCase())
  const usage = device && flow.flowOf(device)
  const addresses = new Set(device?.ips?.map((ip) => ip.toLowerCase()) ?? [])
  const matchingQueries = dnsQueries.data?.queries.filter((q) => addresses.has(q.client.toLowerCase())) ?? []
  const shownQueries = matchingQueries.filter((q) => !filter.trim() || [q.name, q.type, q.rcode, q.policy ?? '', ...(q.answers ?? [])].some((value) => value.toLowerCase().includes(filter.trim().toLowerCase())))

  if (devices.isPending) return <p className="py-16 text-sm text-muted-foreground">Loading device…</p>
  if (devices.isError) return <State title="Could not load devices" detail="Try again when the router is reachable." />
  if (!device) return <State title="Device not found" detail="It may have been forgotten or is no longer in the device list." />

  return (
    <div className="space-y-6 pb-12">
      <DeviceInlineEditor key={device.mac} device={device} traffic={
        <>
        <section aria-labelledby="device-traffic-title" className="space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h2 id="device-traffic-title" className="flex items-center gap-2 text-sm font-medium"><Activity className="size-4" aria-hidden /> Traffic</h2>
            <Button variant="outline" size="sm" nativeButton={false} render={<Link to="/gateway/usage">Usage settings</Link>} />
          </div>
            {flow.counting ? (
              <div className="grid grid-cols-2 gap-3">
                <Meter icon={ArrowDown} label="Received" bytes={usage?.down} rate={usage?.downRate} />
                <Meter icon={ArrowUp} label="Sent" bytes={usage?.up} rate={usage?.upRate} />
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">{traffic.isError ? 'Traffic counters are unavailable right now.' : 'Traffic counting is off or unavailable.'}</p>
            )}
            <p className="text-xs leading-relaxed text-muted-foreground">
              Through this router only · totals since counting started. Shared or changing IPs may affect attribution.
            </p>
        </section>
        <DevicePolicy mac={device.mac} />
        </>
      } />

      <Card>
        <CardHeader><CardTitle>DNS</CardTitle></CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between gap-4"><label htmlFor="device-dns-filter" className="shrink-0 text-sm font-medium">Filter queries</label><Input id="device-dns-filter" className="min-w-0 w-full max-w-sm" value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Name, answer, type or result" /></div>
          {dnsQueries.isError ? (
            <p className="text-sm text-muted-foreground">The DNS query log is unavailable. The resolver may be stopped.</p>
          ) : dnsQueries.isPending ? (
            <p className="text-sm text-muted-foreground">Loading recent queries…</p>
          ) : !dnsConfig.data?.query_log.enabled ? (
            <p className="text-sm text-muted-foreground">The DNS query log is off. Queries are answered but not kept.</p>
          ) : shownQueries.length === 0 ? (
            <p className="text-sm text-muted-foreground">{matchingQueries.length ? 'Nothing matches that filter.' : 'No matching queries in the recent sample.'}</p>
          ) : (
            <ul className="max-h-80 space-y-1 overflow-y-auto">
              {shownQueries.map((q, i) => <li key={`${q.at}-${q.client}-${q.name}-${i}`} className="flex flex-wrap items-center gap-3 px-3 py-2 text-sm">
                <time dateTime={q.at} className="w-20 shrink-0 font-mono text-xs text-muted-foreground">{new Date(q.at).toLocaleTimeString()}</time>
                <span className="min-w-0 flex-[1_1_10rem] break-all">{q.name}<span className="ml-2 text-xs text-muted-foreground">{q.type}</span></span>
                {q.blocked ? <Badge variant="secondary">Blocked{q.policy ? ` · ${q.policy}` : ''}</Badge> : q.rcode !== 'NOERROR' ? <Badge variant="warning">{q.rcode}</Badge> : q.answers?.length ? <DnsAnswers answers={q.answers} /> : <span className="text-xs text-muted-foreground">No answer</span>}
              </li>)}
            </ul>
          )}
        </CardContent>
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

function Meter({ icon: Icon, label, bytes, rate }: { icon: typeof ArrowDown; label: string; bytes?: number; rate?: number }) {
  return <div className="rounded-lg bg-muted/40 p-3">
    <div className="flex items-center gap-2 text-xs text-muted-foreground"><Icon className="size-4" aria-hidden />{label}</div>
    <div className="mt-2 text-xl font-semibold tabular-nums">{bytes === undefined ? 'No address match' : formatBytes(bytes)}</div>
    {rate !== undefined && <div className="mt-1 text-xs text-muted-foreground">{formatRate(rate)} now</div>}
  </div>
}

function DnsAnswers({ answers }: { answers: string[] }) {
  const remaining = answers.length - 3
  return <span className="flex w-full min-w-0 items-center font-mono text-xs text-muted-foreground sm:w-auto sm:max-w-[50%]" title={answers.join(', ')}>
    <span className="truncate">{answers.slice(0, 3).join(', ')}</span>
    {remaining > 0 && <span className="shrink-0">, {remaining} more</span>}
  </span>
}
