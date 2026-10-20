import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Heading } from '@/components/ui/heading'
import { Input } from '@/components/ui/input'
import { MetricValue } from '@/components/ui/metric-value'
import { TechnicalValue } from '@/components/ui/property-row'
import { DeviceActivity } from '@/features/devices/activity'
import { CategoryPicker, type IconChoice } from '@/features/devices/category-picker'
import { DeviceHeader } from '@/features/devices/device-header'
import { DeviceIcon } from '@/features/devices/device-icon'
import { DeviceSettingRow } from '@/features/devices/device-setting-row'
import { ClientQueries } from '@/features/dns/client-queries'
import { useGatewayTraffic } from '@/features/gateway/queries'
import { DeviceServices } from '@/features/ingress/device-services'
import { PeerBadge } from '@/features/remote/peer-badge'
import { usePatchPeer, useRemoteStatus } from '@/features/remote/queries'
import { useTrafficView, type Flow, type TrafficView } from '@/features/topology/traffic'
import { ApiError } from '@/lib/api'
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
 *
 * The two labels a device has and a peer used to lack are here too: its name and
 * its picture. Both are stored on the peer entry rather than inferred, both are
 * editable in place the way a device's are, and neither reaches the kernel — a
 * rename does not re-issue a configuration and a picture does not even reach
 * the tunnel.
 */
export function WireguardPeerPage() {
  const { name } = useParams()
  const status = useRemoteStatus()
  const traffic = useGatewayTraffic()
  const flow = useTrafficView(traffic.data, traffic.isError)
  const patch = usePatchPeer()
  const navigate = useNavigate()
  const peers = status.data?.peers ?? []
  // The URL carries the name, which is the key. A link that somehow says the
  // label instead — a card opened by an older build, a hand-typed address —
  // finds the device and is corrected below rather than 404ing on somebody who
  // did nothing wrong.
  const peer =
    peers.find((p) => p.name === name) ??
    peers.find((p) => p.display_name && p.display_name.toLowerCase() === (name ?? '').toLowerCase())
  const usage = peer?.address ? flow.flowOfAddress(peer.address) : undefined

  useEffect(() => {
    if (peer && peer.name !== name) {
      navigate(`/devices/wireguard/${encodeURIComponent(peer.name)}`, { replace: true })
    }
  }, [peer, name, navigate])

  const [renaming, setRenaming] = useState(false)
  const [draft, setDraft] = useState('')

  const [picture, setPicture] = useState<IconChoice>({ category: '', icon: '' })
  const [choosing, setChoosing] = useState(false)
  // A ref, not state: the second save is the one blur fires after Enter has
  // already sent the first, and it has to be dropped before it renames twice.
  const saved = useRef(false)

  function beginRename() {
    if (!peer) return
    saved.current = false
    setDraft(peer.display_name ?? '')
    setRenaming(true)
  }

  /**
   * Saves the label, not the name.
   *
   * The name is the key — the URL this page lives at, the argument every
   * command takes, the local name `olr dns` would be told to answer — so it
   * stays a DNS label and is edited where the rest of the device is (the tunnel
   * page's dialog). What is edited here is what a person reads, which is why
   * it can be "Li's Macbook Pro". Empty puts the name back.
   */
  async function saveLabel(discard = false) {
    if (!peer || saved.current) return
    saved.current = true
    const next = draft.trim()
    setRenaming(false)
    if (discard || next === (peer.display_name ?? '')) return
    try {
      await patch.mutateAsync({ name: peer.name, patch: { display_name: next } })
      toast.success(next === '' ? 'Name shown again' : 'Name saved')
    } catch (error) {
      toast.error(failureText(error))
    }
  }

  function beginPicture() {
    if (!peer) return
    setPicture({ category: peer.category ?? '', icon: peer.icon ?? '' })
    setChoosing(true)
  }

  async function savePicture() {
    if (!peer) return
    try {
      await patch.mutateAsync({ name: peer.name, patch: { category: picture.category, icon: picture.icon } })
      setChoosing(false)
      toast.success('Icon saved')
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : String(error))
    }
  }

  if (status.isPending) return <p className="py-16 text-sm text-muted-foreground">Loading device…</p>
  if (status.isError) return <State title="Could not load the tunnel" detail="Try again when the router is reachable." />
  if (!peer) return <State title="Device not found" detail="No device by this name may dial in. It may have been renamed or removed on the tunnel page." />

  return (
    <div className="space-y-4 pb-12">
      <Card className="gap-0 p-4 sm:p-6">
        <DeviceHeader
          activity={peer.address && <DeviceActivity addresses={[peer.address]} downRate={usage?.downRate} upRate={usage?.upRate} />}
          picture={<DeviceIcon icon={peer.icon} category={peer.category ?? ''} online={peer.online} size="lg" />}
          pictureBusy={patch.isPending}
          onPicture={beginPicture}
          title={renaming ? (
            <Input
              autoFocus
              aria-label="What this device is called"
              placeholder={peer.name}
              className="h-auto w-80 max-w-full py-1 text-2xl font-semibold"
              value={draft}
              disabled={patch.isPending}
              onChange={(event) => { saved.current = false; setDraft(event.target.value) }}
              onBlur={() => void saveLabel()}
              onKeyDown={(event) => {
                if (event.nativeEvent.isComposing) return
                if (event.key === 'Enter' || event.key === 'Escape') {
                  event.preventDefault()
                  void saveLabel(event.key === 'Escape')
                }
              }}
            />
          ) : (
            <button
              type="button"
              onClick={beginRename}
              aria-label={`Change what ${peer.display_name || peer.name} is called`}
              className="w-full break-words rounded-md text-left hover:text-primary focus-visible:outline-2 focus-visible:outline-ring"
            >
              {peer.display_name || peer.name}
            </button>
          )}
          status={<PeerBadge peer={peer} />}
        />

        <div className="mt-4 grid gap-y-8 lg:grid-cols-2 lg:gap-x-16 lg:gap-y-0">
          <Facts peer={peer} status={status.data} services={
            // Published by its tunnel address, which is the only name for this
            // device that `ingress` has: a peer is not in `devices`, so there is
            // no MAC to reference and no reservation to require
            // (docs/ingress.md §1.2, docs/remote-access.md §3.3).
            <DeviceServices addresses={peer.address ? [peer.address] : []} add={
              peer.address ? `/advanced/caddy?host=${encodeURIComponent(peer.address)}` : '/advanced/caddy'
            } />
          } />
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

      <Dialog open={choosing} onOpenChange={setChoosing}>
        <DialogContent className="max-h-[90svh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Device icon</DialogTitle>
            <DialogDescription>
              Choose an icon for {peer.name}. It changes nothing about what the device is or how it connects.
            </DialogDescription>
          </DialogHeader>
          <CategoryPicker value={picture} onChange={setPicture} />
          <DialogFooter>
            <Button variant="ghost" disabled={patch.isPending} onClick={() => setChoosing(false)}>Cancel</Button>
            <Button disabled={patch.isPending} onClick={() => void savePicture()}>Save icon</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

/**
 * What the tunnel knows, which is the whole of what can be said about a peer.
 *
 * The name leads, because it is the key: the address bar, every command and
 * `wg show` all use it, while the title above may show a friendlier label that
 * hides it. It is a fact in the table rather than a chip beside the title, the
 * same place the device page puts its hardware address.
 *
 * The public key is on screen rather than hidden: it is not a secret — it is in
 * every configuration this router has handed out — and it is what matches this
 * page against `wg show` on the box, or against what the device itself shows.
 *
 * "Last heard" is the handshake, and it is called "Last heard" rather than
 * "Handshake" for the reader who came from a device list, where that is the
 * column they saw. The tunnel's word for it is in the tooltip, where a
 * `wg show` reader will look for it. The badge above answers whether it is
 * connected; this answers exactly when, which is the number to compare against
 * the router's clock when something is subtly wrong.
 */
function Facts({ peer, status, services }: { peer: RemotePeer; status?: RemoteStatus; services: React.ReactNode }) {
  return <dl className="min-w-0 text-sm">
    <DeviceSettingRow label="Name" title="The name the address bar and every command use">
      <TechnicalValue>{peer.name}</TechnicalValue>
    </DeviceSettingRow>
    <DeviceSettingRow label="Address">
      {peer.address ? <TechnicalValue>{peer.address}</TechnicalValue> : '—'}
    </DeviceSettingRow>
    <DeviceSettingRow label="Public key">
      {peer.public_key ? <TechnicalValue>{peer.public_key}</TechnicalValue> : '—'}
    </DeviceSettingRow>
    <DeviceSettingRow label="Sends home">{routesLabel(peer.routes)}</DeviceSettingRow>
    <DeviceSettingRow label="Last heard" title="The last WireGuard handshake: the tunnel's own record of hearing from this device">
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
    <DeviceSettingRow label="Services">{services}</DeviceSettingRow>
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

/**
 * The problem, not just the headline.
 *
 * A 422 from this module names the field it refused ("a name may use letters,
 * digits and hyphens", at `display_name`), and a toast that said only "invalid
 * remote-access configuration" would leave the operator guessing at a rule the
 * daemon had already quoted.
 */
function failureText(error: unknown): string {
  if (!(error instanceof ApiError)) return String(error)
  const problems = error.problems.map((p) => [p.path, p.message].filter(Boolean).join(' '))
  return problems.length ? `${error.message} — ${problems.join('; ')}` : error.message
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
