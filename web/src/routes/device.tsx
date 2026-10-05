import { Activity, ArrowDown, ArrowLeft, ArrowUp, Clock3, LockKeyhole, Pencil, ShieldAlert } from 'lucide-react'
import { Link, useParams } from 'react-router'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { DeviceIcon } from '@/features/devices/device-icon'
import { useDeviceActions } from '@/features/devices/device-actions'
import { useDeviceList } from '@/features/devices/queries'
import { useGatewayTraffic } from '@/features/gateway/queries'
import { useTrafficView } from '@/features/topology/traffic'
import { formatAgo, formatBytes, formatRate } from '@/lib/utils'

export function DevicePage() {
  const { mac } = useParams()
  const devices = useDeviceList()
  const traffic = useGatewayTraffic()
  const flow = useTrafficView(traffic.data, traffic.isError)
  const actions = useDeviceActions()
  const device = devices.data?.devices.find((d) => d.mac.toLowerCase() === mac?.toLowerCase())
  const usage = device && flow.flowOf(device)

  if (devices.isPending) return <p className="py-16 text-sm text-muted-foreground">Loading device…</p>
  if (devices.isError) return <State title="Could not load devices" detail="Try again when the router is reachable." />
  if (!device) return <State title="Device not found" detail="It may have been forgotten or is no longer in the device list." />

  return (
    <div className="mx-auto max-w-5xl space-y-6 pb-12">
      <Link to="/" className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground">
        <ArrowLeft className="size-4" aria-hidden /> Network overview
      </Link>

      <header className="flex flex-col gap-5 rounded-2xl border bg-card p-5 sm:flex-row sm:items-center sm:p-7">
        <DeviceIcon icon={device.icon} category={device.category} vendor={device.vendor}
          vendorKey={device.vendor_key} online={device.online} size="lg" />
        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="break-words text-2xl font-semibold tracking-tight sm:text-3xl">{device.name || device.mac}</h1>
            <Badge variant={device.online ? 'success' : device.seen ? 'secondary' : 'outline'}>
              {device.online ? 'Online' : device.seen ? 'Offline' : 'Never seen'}
            </Badge>
          </div>
          <p className="font-mono text-xs text-muted-foreground">{device.mac}</p>
          <p className="text-sm text-muted-foreground">
            {device.network || 'Network unknown'}{device.group ? ` · ${device.group}` : ''}
            {device.last_seen ? ` · Last heard ${formatAgo(device.last_seen)}` : ''}
          </p>
        </div>
        <Button variant="outline" onClick={() => actions.select(device)}>
          <Pencil className="size-4" aria-hidden /> Edit device
        </Button>
      </header>

      <div className="grid gap-4 md:grid-cols-[1.1fr_0.9fr]">
        <Card>
          <CardHeader><CardTitle>What the router knows</CardTitle></CardHeader>
          <CardContent>
            <dl className="divide-y text-sm">
              <Fact label="Addresses" value={device.ips?.length ? device.ips.join(', ') : 'None observed'} mono />
              <Fact label="Fixed address" value={device.fixed_ip || 'Not reserved'} mono={Boolean(device.fixed_ip)} />
              <Fact label="Calls itself" value={device.hostname || 'Not reported'} />
              <Fact label="Vendor" value={device.vendor || 'Unknown'} />
              <Fact label="Network" value={device.network || 'Unknown'} />
              <Fact label="Seen by" value={device.sources?.length ? device.sources.map((s) => s === 'dhcp-lease' ? 'DHCP lease' : s === 'arp' ? 'Network traffic' : s).join(', ') : 'Not observed'} />
              <Fact label="Lease expires" value={device.expires ? new Date(device.expires).toLocaleString() : 'No expiring lease'} />
            </dl>
            {device.notes && <p className="mt-4 rounded-lg bg-muted/60 p-3 text-sm">{device.notes}</p>}
            <p className="mt-4 text-xs text-muted-foreground">Addresses and presence are observations, not settings. Edit the name, group, route or reservation above.</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader><CardTitle className="flex items-center gap-2"><Activity className="size-4" aria-hidden /> Traffic through this router</CardTitle></CardHeader>
          <CardContent className="space-y-5">
            {flow.counting ? (
              <div className="grid grid-cols-2 gap-3">
                <Meter icon={ArrowDown} label="Received" bytes={usage?.down} rate={usage?.downRate} />
                <Meter icon={ArrowUp} label="Sent" bytes={usage?.up} rate={usage?.upRate} />
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">{traffic.isError ? 'Traffic counters are unavailable right now.' : 'Traffic counting is off or unavailable.'}</p>
            )}
            <p className="text-xs leading-relaxed text-muted-foreground">
              Counts start when usage counting is enabled. They cover traffic forwarded by this router, not traffic between devices on the same network. Shared or changing IP addresses can affect attribution.
            </p>
            <Button variant="outline" size="sm" render={<Link to="/gateway/usage">Usage settings</Link>} />
          </CardContent>
        </Card>
      </div>

      <section className="overflow-hidden rounded-2xl border bg-card" aria-labelledby="inspection-title">
        <div className="border-b bg-muted/40 px-5 py-5 sm:px-7">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <p className="mb-1 text-xs font-semibold uppercase tracking-widest text-muted-foreground">Advanced · private by default</p>
              <h2 id="inspection-title" className="text-xl font-semibold tracking-tight">Inspect network requests</h2>
            </div>
            <Badge variant="outline">Not available yet</Badge>
          </div>
          <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
            OLR does not decrypt or save this device’s requests. Temporary, per-device inspection is planned; there is no capture session or request log on this page yet.
          </p>
        </div>
        <div className="grid gap-6 p-5 sm:p-7 lg:grid-cols-3">
          <Guide icon={Clock3} number="01" title="Start a short session">
            Select one device for a 15-minute session. The router must confirm its current addresses before intercepting only supported TCP HTTP(S) traffic.
          </Guide>
          <Guide icon={LockKeyhole} number="02" title="Trust the debugging CA">
            HTTPS inspection requires installing and trusting this router’s debugging CA on the device. No proxy address is needed. HTTP needs no CA.
          </Guide>
          <Guide icon={ShieldAlert} number="03" title="Stop and clear">
            A session will stop automatically or on demand, remove interception rules and clear captured data. Stopping does not remove the CA from your device.
          </Guide>
        </div>
        <div className="border-t px-5 py-4 text-xs leading-relaxed text-muted-foreground sm:px-7">
          Planned results will separate inspected requests, connections that could not be inspected, and other connection metadata. Certificate pinning, apps that reject user CAs, non-HTTP protocols and HTTP/3 may remain invisible. Blocking UDP/443 to encourage TCP fallback can break some apps and will require an explicit choice. Request and response bodies will remain off by default.
        </div>
      </section>
      {actions.dialogs}
    </div>
  )
}

function State({ title, detail }: { title: string; detail: string }) {
  return <div className="mx-auto max-w-5xl space-y-3 py-16 text-center">
    <h1 className="text-xl font-semibold">{title}</h1>
    <p className="text-sm text-muted-foreground">{detail}</p>
    <Button variant="outline" render={<Link to="/">Back to overview</Link>} />
  </div>
}

function Fact({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return <div className="grid gap-1 py-2.5 sm:grid-cols-[9rem_1fr]">
    <dt className="text-muted-foreground">{label}</dt>
    <dd className={`min-w-0 break-all ${mono ? 'font-mono text-xs' : ''}`}>{value}</dd>
  </div>
}

function Meter({ icon: Icon, label, bytes, rate }: { icon: typeof ArrowDown; label: string; bytes?: number; rate?: number }) {
  return <div className="rounded-xl border bg-muted/30 p-4">
    <div className="flex items-center gap-2 text-xs text-muted-foreground"><Icon className="size-4" aria-hidden />{label}</div>
    <div className="mt-3 text-xl font-semibold tabular-nums">{bytes === undefined ? 'No address match' : formatBytes(bytes)}</div>
    {rate !== undefined && <div className="mt-1 text-xs text-muted-foreground">{formatRate(rate)} now</div>}
  </div>
}

function Guide({ icon: Icon, number, title, children }: { icon: typeof Clock3; number: string; title: string; children: React.ReactNode }) {
  return <div className="space-y-2">
    <div className="flex items-center gap-2 text-sm font-medium"><Icon className="size-4 text-muted-foreground" aria-hidden /><span className="text-xs text-muted-foreground">{number}</span>{title}</div>
    <p className="text-sm leading-relaxed text-muted-foreground">{children}</p>
  </div>
}
