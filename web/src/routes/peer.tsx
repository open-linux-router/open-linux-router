import { KeyRound } from 'lucide-react'
import { Link, useParams } from 'react-router'

import { Card } from '@/components/ui/card'
import { Heading } from '@/components/ui/heading'
import { MetricValue } from '@/components/ui/metric-value'
import { TechnicalValue } from '@/components/ui/property-row'
import { ClientQueries } from '@/features/dns/client-queries'
import { DeviceActivity } from '@/features/devices/activity'
import { DeviceSettingRow } from '@/features/devices/device-setting-row'
import { useGatewayTraffic } from '@/features/gateway/queries'
import { PeerBadge } from '@/features/remote/peer-badge'
import { useRemoteStatus } from '@/features/remote/queries'
import { WayMark } from '@/features/remote/way-mark'
import { useTrafficView, type Flow, type TrafficView } from '@/features/topology/traffic'
import { formatBytes, formatRate } from '@/lib/utils'
import type { RemotePeer, RemoteStatus } from '@/lib/api-types'

/**
 * One device that dialed in, at a URL shaped like a device on the LAN.
 *
 * The page is the tunnel's answer about a peer, and it is deliberately **not**
 * the same page as `/devices/:mac` (docs/remote-access.md §3.3). A peer has no
 * MAC, so it is not a `DeviceRow` and is not in the device list: no vendor, no
 * group, no address reservation, no per-device priority or speed limit — every
 * one of those is keyed by the hardware address this device does not have.
 *
 * What is left is what the tunnel can honestly say — address, key, what it
 * sends home, when it last handshaked and from where — beside the three things
 * that are keyed by *address* and therefore work exactly as they do for a
 * device: the activity history, the traffic counters, and the DNS query log.
 */
export function WireguardPeerPage() {
  const { name } = useParams()
  const status = useRemoteStatus()
  const traffic = useGatewayTraffic()
  const flow = useTrafficView(traffic.data, traffic.isError)
  const peer = status.data?.peers.find((p) => p.name === name)
  const usage = peer?.address ? flow.flowOfAddress(peer.address) : undefined

  if (status.isPending) return <p className="py-16 text-sm text-muted-foreground">Loading device…</p>
  if (status.isError) return <State title="Could not load the tunnel" detail="Try again when the router is reachable." />
  if (!peer) return <State title="Device not found" detail="No device by this name may dial in. It may have been renamed or removed on the tunnel page." />

  return (
    <div className="space-y-4 pb-12">
      <Card className="gap-0 p-4 sm:p-6">
        <div className="relative flex min-h-20 items-center gap-4 border-b pb-4">
          {peer.address && <DeviceActivity addresses={[peer.address]} downRate={usage?.downRate} upRate={usage?.upRate} />}
          <span className="flex size-12 shrink-0 items-center justify-center rounded-full bg-muted">
            <WayMark way="wireguard" fallback={KeyRound} className="size-5 brightness-0 opacity-75 dark:invert" />
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <Heading as="h1" size="lg" className="min-w-0 max-w-full [overflow-wrap:anywhere]">{peer.name}</Heading>
              <PeerBadge peer={peer} />
            </div>
          </div>
        </div>

        <div className="mt-4 grid gap-y-8 lg:grid-cols-2 lg:gap-x-16 lg:gap-y-0">
          <Facts peer={peer} status={status.data} />
          <Usage flow={flow} usage={usage} trafficFailed={traffic.isError} />
        </div>
      </Card>

      <ClientQueries addresses={peer.address ? [peer.address] : []} />

      {/* The reason stays in the tooltip rather than the page: an operator who
          inspected a device on the LAN will look for the switch here, and one
          word answers them — the detail is a property of the tunnel, not a
          setting somebody turned off. */}
      <section className="overflow-hidden rounded-xl border bg-card shadow-xs" aria-labelledby="peer-connections-title">
        <div className="flex h-18 items-center justify-between gap-3 px-4 sm:px-6">
          <Heading id="peer-connections-title">Connections</Heading>
          <span className="text-sm text-muted-foreground" title="Inspection matches a device's MAC address before its traffic reaches the network, and traffic arriving over the tunnel has none.">
            Not supported
          </span>
        </div>
      </section>

      <p className="text-sm text-muted-foreground">
        Its name, what it sends home and whether it may dial in are set on the{' '}
        <Link to="/access/wireguard" className="text-primary underline-offset-4 hover:underline">tunnel page</Link>.
      </p>
    </div>
  )
}

/**
 * What the tunnel knows, which is the whole of what can be said about a peer.
 *
 * The public key is on screen rather than hidden: it is not a secret — it is in
 * every configuration this router has handed out — and it is what matches this
 * page against `wg show` on the box, or against what the device itself shows.
 *
 * "Handshake" sits beside the badge on purpose. The badge answers whether it is
 * connected; this answers exactly when, which is the number to compare against
 * the router's clock when something is subtly wrong.
 */
function Facts({ peer, status }: { peer: RemotePeer; status?: RemoteStatus }) {
  return <dl className="min-w-0 text-sm">
    <DeviceSettingRow label="Address">
      {peer.address ? <TechnicalValue>{peer.address}</TechnicalValue> : '—'}
    </DeviceSettingRow>
    <DeviceSettingRow label="Public key">
      {peer.public_key ? <TechnicalValue>{peer.public_key}</TechnicalValue> : '—'}
    </DeviceSettingRow>
    <DeviceSettingRow label="Sends home">{routesLabel(peer.routes)}</DeviceSettingRow>
    <DeviceSettingRow label="Handshake">
      {peer.last_handshake
        ? <time dateTime={peer.last_handshake}>{new Date(peer.last_handshake).toLocaleString()}</time>
        : peer.unknown ? 'Unknown' : 'Never'}
    </DeviceSettingRow>
    {peer.endpoint && (
      <DeviceSettingRow label="Heard from"><TechnicalValue>{peer.endpoint}</TechnicalValue></DeviceSettingRow>
    )}
    <DeviceSettingRow label="Way in">
      <TechnicalValue>WireGuard · {status?.interface ?? '—'} · {status?.subnet ?? '—'}</TechnicalValue>
    </DeviceSettingRow>
  </dl>
}

/**
 * The counters the network map and a device page read, for one address.
 *
 * A rate needs two samples, so it is missing on the first poll, and that absence
 * is drawn as a dash: "no rate yet" and "idle" are different answers and a zero
 * would claim the second.
 */
function Usage({ flow, usage, trafficFailed }: { flow: TrafficView; usage?: Flow; trafficFailed: boolean }) {
  return <div className="min-w-0">
    <dl className="text-sm">
      <DeviceSettingRow label="Download">
        <MetricValue value={flow.rated && usage ? formatRate(Math.max(0, usage.downRate ?? 0)) : null} />
      </DeviceSettingRow>
      <DeviceSettingRow label="Upload">
        <MetricValue value={flow.rated && usage ? formatRate(Math.max(0, usage.upRate ?? 0)) : null} />
      </DeviceSettingRow>
      <DeviceSettingRow label="Total">
        <span className="inline-flex flex-wrap justify-end gap-3 tabular-nums">
          <span className="inline-flex items-center gap-0.5" aria-label="Download total"><span aria-hidden className="text-base font-normal text-muted-foreground">↓</span><MetricValue value={flow.counting ? formatBytes(usage?.down ?? 0) : null} /></span>
          <span className="inline-flex items-center gap-0.5" aria-label="Upload total"><span aria-hidden className="text-base font-normal text-muted-foreground">↑</span><MetricValue value={flow.counting ? formatBytes(usage?.up ?? 0) : null} /></span>
        </span>
      </DeviceSettingRow>
    </dl>
    {!flow.counting && (
      <p className="pt-2 text-xs leading-relaxed text-muted-foreground">
        {trafficFailed ? 'Traffic counters are unavailable right now.' : 'Traffic counting is off, so there is nothing to show here.'}
      </p>
    )}
  </div>
}

function routesLabel(routes: RemotePeer['routes']): string {
  return routes === 'everything' ? 'Everything the device does' : 'Only what is on my network'
}

function State({ title, detail }: { title: string; detail: string }) {
  return <div className="space-y-3 py-16 text-center">
    <Heading as="h1" size="lg">{title}</Heading>
    <p className="text-sm text-muted-foreground">{detail}</p>
    <Link to="/access/wireguard" className="text-sm text-primary underline-offset-4 hover:underline">Back to the tunnel page</Link>
  </div>
}
