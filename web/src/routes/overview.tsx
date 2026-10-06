import { AlertTriangle, ChevronRight, Info, Plus, Trash2, GripVertical } from 'lucide-react'
import { useEffect, useMemo, useState, type DragEvent } from 'react'
import { Link, useNavigate } from 'react-router'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Label } from '@/components/ui/label'
import { useGroupActions } from '@/features/devices/group-actions'
import { useDeviceList, useDevicesConfig } from '@/features/devices/queries'
import { useDhcpConfig, useDhcpStatus } from '@/features/dhcp/queries'
import { useDialStatus } from '@/features/dial/queries'
import { useDnsStatus } from '@/features/dns/queries'
import { RELAY_UNIT, serviceOf } from '@/features/dns/units'
import { useIngressConfig, useIngressStatus } from '@/features/ingress/queries'
import { servicesByDevice } from '@/features/topology/services'
import { useHostMetrics, type HostMetrics } from '@/features/system/queries'
import thesvgSlugs from '@/features/gateway/thesvg-slugs.json'
import { useGatewayConfig, useGatewayLatency, useSaveLatencySites, useGatewayStatus, useGatewayTraffic } from '@/features/gateway/queries'
import { FirstRun } from '@/features/setup/first-run'
import { NetworkMap } from '@/features/topology/network-map'
import { buildOutside } from '@/features/topology/outside'
import { useTrafficView, type TrafficView } from '@/features/topology/traffic'
import type { DeviceRow, DhcpStatus, DnsStatus, GatewayLatency, GatewayStatus, GatewayTraffic } from '@/lib/api-types'
import { getToken } from '@/lib/api'
import { cn, formatBytes, formatRate } from '@/lib/utils'

/**
 * What the router is doing, on the page you land on.
 *
 * This used to be two cards and a placeholder saying the rest arrived with link
 * and dial — which stayed on screen after three more modules shipped, so an
 * operator's first view under-described their own router and the most urgent
 * fact it held, a way out that had stopped responding, appeared nowhere.
 *
 * The order is what an operator asks in order: is anything wrong, how much is
 * this network doing, and what is on it. The answer to the first is the
 * first card; faults follow the cards in full. Then the map, which is both the
 * only place three modules' answers are joined into one picture and the only
 * place devices are listed.
 *
 * The map has no toolbar. A search box, a network filter, a density switch and
 * a New group button sat between the counters and the picture on every visit,
 * for things done now and then; the picture chooses its own density, a new
 * group is made from any device's menu, and the other two have their own pages.
 *
 * The tree is organised by the operator's own groups, and is where they are
 * made, renamed and removed: a group only changes this picture, so the picture
 * is the place to change it, and a separate Groups page would be a screen whose
 * only effect is on a different screen.
 *
 * One device surface, deliberately. A flat list under the tree would have shown
 * the same devices twice on one screen — the exact duplication that took the
 * connected-devices card off the DHCP page — so the tree carries the search box
 * and opens the detail sheet itself.
 *
 * It polls more endpoints than any other screen. That is the cost of being the
 * one screen that saves you visiting the other three.
 */
export function OverviewPage() {
  const devices = useDeviceList()
  const dhcp = useDhcpStatus()
  const dhcpConfig = useDhcpConfig()
  const dns = useDnsStatus()
  const gateway = useGatewayStatus()
  const gatewayConfig = useGatewayConfig()
  // Only the live rates need a fast sample; the other observed data stays at 5s.
  const traffic = useGatewayTraffic(1000)
  const latency = useGatewayLatency()
  const identity = useDevicesConfig()
  const ingress = useIngressConfig()
  const ingressStatus = useIngressStatus()
  const services = useMemo(() => servicesByDevice(ingress.data, devices.data?.devices ?? []), [ingress.data, devices.data])
  const flows = useTrafficView(traffic.data, traffic.isError)
  const host = useHostMetrics()
  const dial = useDialStatus()
  const outside = useMemo(() => buildOutside(dial.data, devices.data?.devices), [dial.data, devices.data])
  const navigate = useNavigate()
  const groupActions = useGroupActions()

  const faults = collectFaults(dhcp.data, dns.data, gateway.data)
  // Only a verdict once every module that can raise a fault has answered:
  // "running smoothly" said before DNS has reported is a guess. And none on a
  // box where nothing is switched on — there are no faults because nothing is
  // running, which is what the first-run panel is there to say.
  const known = Boolean(dhcp.data && dns.data && gateway.data)
  const idle = known && !dhcp.data!.enabled && !dns.data!.enabled && !gateway.data!.enabled

  return (
    <div className="space-y-6">
      {/* Above everything, on exactly the boxes where all three are empty. It
          renders nothing once the router is doing something. */}
      <FirstRun />

      <h1 className="sr-only">Network overview</h1>
      <Stats devices={devices.data?.devices} flows={flows} host={host.data}
        faults={faults} known={known} idle={idle} failed={dhcp.isError || dns.isError || gateway.isError}
        latency={latency.data} exits={gatewayConfig.data?.enabled ? gatewayConfig.data.exits?.map((exit) => exit.name) ?? [] : []} latencyFailed={latency.isError} trafficFailed={traffic.isError} />

      {faults.map((fault) => (
        <Alert key={fault.key} variant={fault.tone === 'bad' ? 'destructive' : 'default'}>
          <AlertTriangle />
          <AlertTitle>{fault.title}</AlertTitle>
          <AlertDescription className="space-y-2">
            <p>{fault.detail}</p>
            {fault.to && (
              <Link
                to={fault.to}
                className="inline-flex items-center gap-1 text-sm font-medium underline underline-offset-4"
              >
                {fault.action}
                <ChevronRight className="size-3.5" aria-hidden />
              </Link>
            )}
          </AlertDescription>
        </Alert>
      ))}

      <section aria-label="Your network" className="space-y-4 pt-2">
        <NetworkMap
          devices={devices.data?.devices ?? []}
          services={services}
          serviceDomain={ingressStatus.data?.domain}
          groups={identity.data?.groups}
          traffic={flows}
          assignments={gateway.data?.assignments}
          exits={gateway.data?.exits}
          outside={outside}
          pools={dhcpConfig.data?.pools}
          pending={devices.isPending}
          density="auto"
          onSelect={(device) => navigate(`/devices/${device.mac}`)}
          onCreateGroup={groupActions.create}
          onRenameGroup={groupActions.rename}
          onDeleteGroup={groupActions.remove}
          onMoveGroup={groupActions.moveGroup}
        />

        <TrafficNote traffic={traffic.data} failed={traffic.isError} flows={flows} />
      </section>

      {groupActions.dialogs}
    </div>
  )
}

/* -------------------------------------------------------------------------- */

interface Fault {
  key: string
  title: string
  detail: string
  tone: 'bad' | 'warn'
  to?: string
  action?: string
}

/**
 * Everything wrong right now.
 *
 * Each one names the consequence rather than the mechanism. An operator reading
 * "Office VPN is not responding" still has to be told that work0 is cut off by
 * it, because that is the part they will act on.
 */
function collectFaults(dhcp?: DhcpStatus, dns?: DnsStatus, gateway?: GatewayStatus): Fault[] {
  const out: Fault[] = []

  for (const exit of gateway?.exits ?? []) {
    // Never probed is not the same as up (§5.6), and an exit nobody checks must
    // not be reported as broken either.
    if (!exit.probed || exit.up) continue
    const affected = exit.used_by ?? []
    out.push({
      key: `exit-${exit.name}`,
      title: `${exit.name} is not responding`,
      detail: affected.length
        ? `${affected.join(', ')} ${affected.length === 1 ? 'goes' : 'go'} out this way, so traffic from ${affected.length === 1 ? 'it' : 'them'} is not getting through.`
        : 'Nothing is using it at the moment, so nothing is cut off yet.',
      tone: affected.length ? 'bad' : 'warn',
      to: '/gateway',
      action: 'Open Gateway',
    })
  }

  if (gateway && !gateway.known) {
    out.push({
      key: 'gateway-unknown',
      title: 'The gateway settings are saved but not in force',
      detail:
        'Nothing on the gateway screen is actually running. On Linux this usually means the daemon lacks permission to change routing.',
      tone: 'bad',
      to: '/gateway',
      action: 'Open Gateway',
    })
  }

  const relay = dns ? serviceOf(dns, RELAY_UNIT) : undefined
  if (dns?.enabled && relay?.status && !relay.status.active) {
    out.push({
      key: 'dns-down',
      title: 'Nothing is answering DNS',
      detail:
        'DNS is on and the server is stopped. To the people using this network it looks like the internet is down.',
      tone: 'bad',
      to: '/dns',
      action: 'Open DNS',
    })
  }

  if (dhcp?.enabled && dhcp.service && !dhcp.service.active) {
    out.push({
      key: 'dhcp-down',
      title: 'Addresses are not being handed out',
      detail:
        'DHCP is on and the server is stopped. Devices here keep their address until it expires; anything joining now gets none.',
      tone: 'bad',
      to: '/dhcp',
      action: 'Open DHCP',
    })
  }

  // A unit that runs but is not enabled costs nothing until the power goes out,
  // and then costs the whole network at once. Worth saying while it is cheap.
  //
  // `active` is half the condition and was missing: a unit that is stopped and
  // not enabled is not this warning at all. The dns-down card above already
  // says nothing is answering, and following it with "is running, but is not
  // set to start at boot" contradicted it on the same screen — which is what a
  // brand-new install saw, since nothing is enabled or running until the first
  // successful apply.
  for (const service of dns?.services ?? []) {
    if (!service.status || service.status.enabled || !service.status.active) continue
    out.push({
      key: `boot-${service.unit}`,
      title: 'DNS will not come back after a reboot',
      detail: `${service.unit} is running, but is not set to start at boot.`,
      tone: 'warn',
      to: '/dns',
      action: 'Open DNS',
    })
  }

  // Settings that no longer validate, which the drift check below leaves out
  // because it cannot run against them. A different and worse state than
  // drift: the backend keeps the last settings that did validate, and nothing
  // saved takes effect until what broke them is fixed. StuckSettings has the
  // rest, and the box that had it.
  const stuck: { name: string; to: string; error?: string }[] = [
    { name: 'DHCP', to: '/dhcp', error: dhcp?.enabled ? dhcp.drift_error : undefined },
    { name: 'DNS', to: '/dns', error: dns?.enabled ? dns.drift_error : undefined },
  ]
  for (const s of stuck) {
    if (!s.error) continue
    out.push({
      key: `stuck-${s.name}`,
      title: `${s.name} is running on settings it can no longer apply`,
      detail: 'Something those settings depend on was removed or changed. Its page says what.',
      tone: 'warn',
      to: s.to,
      action: 'Take a look',
    })
  }

  // Only for a module that is switched on, and that is not a way of hiding
  // drift. A disabled module's rendered files differing from its intent has no
  // consequence — nothing is running, and enabling rewrites them on the way —
  // whereas on a box that has never applied anything it is *always* true, which
  // is how a brand-new install came to greet its owner with two warnings that
  // something had changed the configuration behind their back. Nothing had.
  // The section's own page still shows it, where the claim is narrower.
  const drifted: { name: string; to: string }[] = []
  if (dhcp?.enabled && dhcp.drifted && !dhcp.drift_error) drifted.push({ name: 'DHCP', to: '/dhcp' })
  if (dns?.enabled && dns.drifted && !dns.drift_error) drifted.push({ name: 'DNS', to: '/dns' })
  if (gateway?.drifted) drifted.push({ name: 'The gateway', to: '/gateway' })
  for (const d of drifted) {
    out.push({
      key: `drift-${d.name}`,
      title: `${d.name} is not doing what its settings say`,
      // Not "something changed it outside olr", which this used to assert and
      // which is only one of the two ways a module gets here. The other is a
      // backend that is enabled and was never started — nothing changed
      // anything, there is no file to put back, and the old advice sent the
      // operator to a screen with no change left to save.
      detail: 'Some of what you have set is not in force yet. That screen has a button to run it.',
      tone: 'warn',
      to: d.to,
      action: 'Take a look',
    })
  }

  return out
}

/* -------------------------------------------------------------------------- */

function uptime(seconds: number): string {
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  return `${days}d ${hours}h ${minutes}m`
}

// Limits are personal display settings; no router configuration is changed.
const LIMITS_KEY = 'olr-overview-bandwidth-limits'

function readLimits(): { down: number; up: number } {
  try {
    const saved = JSON.parse(localStorage.getItem(LIMITS_KEY) ?? '{}')
    return {
      down: Number.isFinite(saved.down) && saved.down > 0 ? saved.down : 0,
      up: Number.isFinite(saved.up) && saved.up > 0 ? saved.up : 0,
    }
  } catch { return { down: 0, up: 0 } }
}

type Direction = 'down' | 'up'

function BandwidthLimitDialog({ direction, limits, onClose, onSave }: {
  direction: Direction | null
  limits: { down: number; up: number }
  onClose: () => void
  onSave: (limits: { down: number; up: number }) => void
}) {
  const [value, setValue] = useState(direction && limits[direction] ? String(limits[direction]) : '')
  const valid = value.trim() === '' || (Number.isFinite(Number(value)) && Number(value) > 0)
  return <Dialog open={direction !== null} onOpenChange={(open) => { if (!open) onClose() }}>
    <DialogContent>
      <DialogHeader><DialogTitle>{direction === 'down' ? 'Download' : 'Upload'} limit</DialogTitle>
        <DialogDescription>Enter your plan speed to see utilization. Saved in this browser only.</DialogDescription></DialogHeader>
      <form onSubmit={(event) => {
        event.preventDefault()
        if (!direction || !valid) return
        onSave({ ...limits, [direction]: Number(value) })
        onClose()
      }} className="space-y-4">
        <div className="space-y-2"><Label htmlFor="bandwidth-limit">Limit (Mbps)</Label>
          <Input id="bandwidth-limit" autoFocus type="number" min="0.001" step="any" value={value} onChange={(event) => setValue(event.target.value)} />
        </div>
        <DialogFooter><Button type="submit" disabled={!valid}>Save</Button></DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
}

function Meter({ label, current, maximum, percent, tone = 'blue', arrow, onClick }: {
  label: string
  current?: string
  maximum?: string
  percent?: number
  tone?: 'blue' | 'amber' | 'neutral'
  arrow?: '↓' | '↑'
  onClick?: () => void
}) {
  const content = <>
    {percent != null && <span aria-hidden className={cn('absolute inset-y-0 left-0 border-r transition-[width] duration-700 ease-out',
      tone === 'amber' ? 'border-amber-400/50 bg-amber-200/40' : tone === 'neutral' ? 'border-slate-400/40 bg-slate-300/35' : 'border-cyan-500/40 bg-cyan-200/45')}
      style={{ width: `${Math.min(100, Math.max(0, percent))}%` }} />}
    <span className="relative z-10 flex min-w-0 items-center gap-2 truncate text-sm font-semibold tabular-nums">
      {arrow && <span aria-hidden className="text-base font-normal text-muted-foreground">{arrow}</span>}{current ?? '—'}
    </span>
    <span className="relative z-10 shrink-0 text-xs text-muted-foreground tabular-nums">{maximum ? `of ${maximum}` : onClick ? 'Set limit' : '—'}</span>
  </>
  const classes = 'relative flex h-10 w-full items-center justify-between gap-2 overflow-hidden rounded-xl bg-muted/65 px-3 text-left ring-1 ring-foreground/[0.06] shadow-[inset_0_1px_2px_rgba(0,0,0,0.03)]'
  return <div className={cn('min-w-0', onClick ? 'py-1' : 'py-1.5')}>
    {onClick ? <button type="button" onClick={onClick} aria-label={`${label}: ${current ?? 'unavailable'}. ${maximum ? `Limit ${maximum}` : 'Set limit'}`}
      className={cn(classes, 'cursor-pointer transition-colors hover:bg-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring')}>
      {content}
    </button> : <>
      <div className="mb-2 flex items-center justify-between gap-2 text-xs text-muted-foreground"><span>{label}</span><span className="tabular-nums">{percent == null ? '—' : `${percent.toFixed(0)}%`}</span></div>
      <div role={percent == null ? undefined : 'progressbar'} aria-label={label} aria-valuenow={percent == null ? undefined : Math.min(100, Math.max(0, Math.round(percent)))} aria-valuemin={0} aria-valuemax={100} className={classes}>
        {content}
      </div>
    </>}
  </div>
}

function StatCard({ title, children }: { title: React.ReactNode; children: React.ReactNode }) {
  return <section className="min-w-0 rounded-2xl bg-card p-4 shadow-xs ring-1 ring-foreground/[0.07]">
    <h2 className="mb-3 text-sm font-semibold">{title}</h2>{children}
  </section>
}

function MetricPill({ label, value, detail, percent, tone = 'blue', health }: {
  label?: string
  value: string
  detail?: string
  percent?: number
  tone?: 'blue' | 'neutral'
  health?: 'good' | 'bad' | 'unknown'
}) {
  return <div className="min-w-0 py-1"><div role={percent == null ? undefined : 'progressbar'} aria-label={label}
    aria-valuenow={percent == null ? undefined : Math.min(100, Math.max(0, Math.round(percent)))}
    aria-valuemin={0} aria-valuemax={100}
    className="relative flex h-10 min-w-0 items-center justify-between gap-2 overflow-hidden rounded-xl bg-muted/65 px-3 ring-1 ring-foreground/[0.06] shadow-[inset_0_1px_2px_rgba(0,0,0,0.03)]">
    {percent != null && <span aria-hidden className={cn('absolute inset-y-0 left-0 border-r transition-[width] duration-700 ease-out', tone === 'neutral' ? 'border-slate-400/40 bg-slate-300/35' : 'border-cyan-500/40 bg-cyan-200/45')}
      style={{ width: `${Math.min(100, Math.max(0, percent))}%` }} />}
    <span className="relative z-10 flex min-w-0 items-center gap-2 truncate text-sm">
      {health && <span aria-hidden className={cn('size-2 shrink-0 rounded-full', health === 'good' ? 'bg-success' : health === 'bad' ? 'bg-destructive' : 'bg-muted-foreground')} />}
      {label && <span className="shrink-0 text-xs text-muted-foreground">{label}</span>}
      <span className={cn('truncate font-semibold tabular-nums', health === 'bad' && 'text-destructive')}>{value}</span>
    </span>
    {detail && <span className="relative z-10 shrink-0 text-xs text-muted-foreground tabular-nums">{detail}</span>}
  </div></div>
}

function SiteIcon({ name, url, icon: iconChoice }: { name: string; url: string; icon?: string }) {
  const [autoIcon, setAutoIcon] = useState<string>()
  useEffect(() => {
    if (iconChoice?.startsWith('data:')) return
    const controller = new AbortController()
    let objectURL: string | undefined
    const token = getToken()
    fetch(`/api/gateway/latency/sites/${encodeURIComponent(name)}/icon?v=${encodeURIComponent(`${url}:${iconChoice ?? 'auto'}`)}`, {
      signal: controller.signal,
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    }).then(async (response) => {
      if (!response.ok) return
      objectURL = URL.createObjectURL(await response.blob())
      if (!controller.signal.aborted) setAutoIcon(objectURL)
      else URL.revokeObjectURL(objectURL)
    }).catch(() => {})
    return () => {
      controller.abort()
      if (objectURL) URL.revokeObjectURL(objectURL)
    }
  }, [name, url, iconChoice])
  const preview = iconChoice?.startsWith('data:') ? iconChoice : iconChoice?.startsWith('thesvg:') ? `https://raw.githubusercontent.com/GLINCKER/thesvg/main/public/icons/${iconChoice.slice(7)}/default.svg` : autoIcon
  return preview ? <img src={preview} alt="" className="size-5 shrink-0 rounded-sm object-contain" />
    : <span aria-hidden className="flex size-5 shrink-0 items-center justify-center rounded-md bg-foreground/10 text-[10px] font-bold uppercase">{name.slice(0, 2)}</span>
}

type MonitoredSite = { name: string; url: string; exit?: string; icon?: string }
type DraftSite = MonitoredSite & { id: string }
const DEFAULT_ROUTE = ' default'

function LatencySitesDialog({ sites, exits, onClose }: { sites: MonitoredSite[]; exits: string[]; onClose: () => void }) {
  const [draft, setDraft] = useState<DraftSite[]>(() => sites.map(({ name, url, exit, icon }) => ({ id: crypto.randomUUID(), name, url, exit, icon })))
  const [name, setName] = useState('')
  const [url, setUrl] = useState('')
  const [exit, setExit] = useState(DEFAULT_ROUTE)
  const [editing, setEditing] = useState<number | null>(null)
  const [iconFor, setIconFor] = useState<number | null>(null)
  const [search, setSearch] = useState('')
  const [iconError, setIconError] = useState('')
  const [dragging, setDragging] = useState<number | null>(null)
  const save = useSaveLatencySites()
  const update = (index: number, changes: Partial<MonitoredSite>) => setDraft((current) => current.map((site, i) => i === index ? { ...site, ...changes } : site))
  const add = () => {
    if (!name.trim() || !url.trim() || draft.length >= 12) return
    setDraft([...draft, { id: crypto.randomUUID(), name: name.trim(), url: url.trim(), exit: exit === DEFAULT_ROUTE ? undefined : exit, icon: undefined }])
    setName('')
    setUrl('')
    setExit(DEFAULT_ROUTE)
  }
  const drop = (target: number) => {
    if (dragging == null || dragging === target) return
    const next = [...draft]
    const [site] = next.splice(dragging, 1)
    next.splice(target, 0, site)
    setDraft(next)
    setDragging(null)
    setEditing(null)
    setIconFor(null)
  }
  const upload = async (file?: File) => {
    if (iconFor == null || !file) return
    setIconError('')
    if (!['image/png', 'image/jpeg', 'image/webp'].includes(file.type) || file.size > 2 * 1024 * 1024) {
      setIconError('Choose a PNG, JPEG or WebP under 2 MiB')
      return
    }
    try {
      const image = await createImageBitmap(file)
      const canvas = document.createElement('canvas')
      const scale = Math.min(1, 128 / Math.max(image.width, image.height))
      canvas.width = Math.max(1, Math.round(image.width * scale))
      canvas.height = Math.max(1, Math.round(image.height * scale))
      canvas.getContext('2d')!.drawImage(image, 0, 0, canvas.width, canvas.height)
      image.close()
      const data = canvas.toDataURL('image/png')
      if (data.length > 130000) throw new Error('Image is too large after resizing')
      update(iconFor, { icon: data })
      setIconFor(null)
    } catch (error) { setIconError(error instanceof Error ? error.message : 'Could not read image') }
  }
  const matches = search.trim() ? thesvgSlugs.filter((slug) => slug.includes(search.trim().toLowerCase())).slice(0, 32) : []
  return <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
    <DialogContent className="sm:max-w-2xl">
      <DialogHeader><DialogTitle>Sites to monitor</DialogTitle><DialogDescription>
        HTTPS response time from this router. Any completed HTTP response counts, even a login redirect or error page. Drag to reorder, select a site to edit, or select its icon to customize it.
      </DialogDescription></DialogHeader>
      <div className="max-h-[45vh] space-y-2 overflow-y-auto pr-1">
        {draft.map((site, i) => <div key={site.id} className="rounded-xl bg-muted/60 p-2 text-sm"
          onDragOver={(event: DragEvent<HTMLDivElement>) => event.preventDefault()} onDrop={(event) => { event.preventDefault(); drop(i) }}>
          <div className="flex items-center gap-2">
            <span draggable onDragStart={() => setDragging(i)} onDragEnd={() => setDragging(null)} aria-label={`Drag ${site.name} to reorder`} className="cursor-grab touch-none text-muted-foreground" title="Drag to reorder"><GripVertical className="size-4" /></span>
            <button type="button" onClick={() => { setIconFor(i); setSearch(''); setIconError('') }} aria-label={`Change ${site.name} icon`} className="rounded-md p-1 hover:bg-background"><SiteIcon name={site.name} url={site.url} icon={site.icon} /></button>
            <div className="flex flex-col sm:hidden"><button type="button" aria-label={`Move ${site.name} up`} disabled={i === 0} onClick={() => { const next = [...draft]; [next[i - 1], next[i]] = [next[i], next[i - 1]]; setDraft(next) }}>↑</button><button type="button" aria-label={`Move ${site.name} down`} disabled={i === draft.length - 1} onClick={() => { const next = [...draft]; [next[i], next[i + 1]] = [next[i + 1], next[i]]; setDraft(next) }}>↓</button></div>
            <button type="button" onClick={() => setEditing(editing === i ? null : i)} className="min-w-0 flex-1 truncate text-left font-medium" title={site.url}>{site.name}</button>
            <Select value={site.exit || DEFAULT_ROUTE} onValueChange={(value) => update(i, { exit: !value || value === DEFAULT_ROUTE ? undefined : value })}>
              <SelectTrigger size="sm" className="max-w-32" aria-label={`Internet via, for ${site.name}`}><SelectValue /></SelectTrigger>
              <SelectContent><SelectItem value={DEFAULT_ROUTE}>Default</SelectItem>{exits.map((value) => <SelectItem key={value} value={value}>{value}</SelectItem>)}
                {site.exit && !exits.includes(site.exit) && <SelectItem value={site.exit}>{site.exit} (unavailable)</SelectItem>}
              </SelectContent>
            </Select>
            <Button variant="ghost" size="icon" aria-label={`Remove ${site.name}`} onClick={() => { setDraft(draft.filter((_, index) => index !== i)); setEditing(null); setIconFor(null) }}><Trash2 className="size-4" /></Button>
          </div>
          {editing === i && <div className="mt-2 grid gap-2 sm:grid-cols-[1fr_2fr]">
            <Input aria-label={`Name for ${site.name}`} value={site.name} maxLength={40} onChange={(event) => update(i, { name: event.target.value })} />
            <Input aria-label={`URL for ${site.name}`} value={site.url} onChange={(event) => update(i, { url: event.target.value })} />
          </div>}
        </div>)}
      </div>
      <div className="flex flex-wrap gap-2 sm:flex-nowrap">
        <Input className="min-w-24 flex-1 sm:w-24" aria-label="Site name" placeholder="Name" maxLength={40} value={name} onChange={(event) => setName(event.target.value)} />
        <Input className="min-w-40 flex-[2] sm:w-52" aria-label="HTTPS URL" placeholder="https://example.com/" value={url} onChange={(event) => setUrl(event.target.value)} />
        <Select value={exit} onValueChange={(value) => setExit(value ?? DEFAULT_ROUTE)}>
          <SelectTrigger className="w-28 shrink-0" aria-label="Internet via for new site"><SelectValue /></SelectTrigger>
          <SelectContent><SelectItem value={DEFAULT_ROUTE}>Default</SelectItem>{exits.map((value) => <SelectItem key={value} value={value}>{value}</SelectItem>)}</SelectContent>
        </Select>
        <Button variant="outline" onClick={add} disabled={!name.trim() || !url.startsWith('https://') || draft.length >= 12}>Add</Button>
      </div>
      {save.isError && <p role="alert" className="text-sm text-destructive">{save.error.message}</p>}
      <DialogFooter><Button variant="outline" onClick={onClose}>Cancel</Button><Button disabled={save.isPending} onClick={() => save.mutate(draft.map(({ name, url, exit, icon }) => ({ name, url, exit, icon })), { onSuccess: onClose })}>Save sites</Button></DialogFooter>
    </DialogContent>
    {iconFor != null && <Dialog open onOpenChange={(open) => { if (!open) setIconFor(null) }}>
      <DialogContent className="sm:max-w-md"><DialogHeader><DialogTitle>Icon for {draft[iconFor]?.name}</DialogTitle><DialogDescription>Upload an image or search theSVG library. Auto uses the site favicon.</DialogDescription></DialogHeader>
        <div className="flex gap-2"><Button variant="outline" onClick={() => { update(iconFor, { icon: undefined }); setIconFor(null) }}>Auto</Button>
          <label className="inline-flex h-8 cursor-pointer items-center rounded-lg border px-3 text-sm">Upload image<input className="sr-only" type="file" accept="image/png,image/jpeg,image/webp" onChange={(event) => upload(event.target.files?.[0])} /></label></div>
        <Input aria-label="Search theSVG icons" placeholder="Search theSVG (e.g. wechat)" value={search} onChange={(event) => setSearch(event.target.value)} />
        <div className="grid max-h-52 grid-cols-4 gap-2 overflow-y-auto">{matches.map((slug) => <button key={slug} type="button" className="flex min-w-0 flex-col items-center gap-1 rounded-lg border p-2 text-xs hover:bg-muted" title={slug}
          onClick={() => { update(iconFor, { icon: `thesvg:${slug}` }); setIconFor(null) }}>
          <img src={`https://raw.githubusercontent.com/GLINCKER/thesvg/main/public/icons/${slug}/default.svg`} alt="" className="size-7" loading="lazy" />
          <span className="w-full truncate">{slug}</span></button>)}</div>
        {iconError && <p role="alert" className="text-sm text-destructive">{iconError}</p>}
        <p className="text-xs text-muted-foreground">Icons by theSVG (MIT). Only the selected ID is saved.</p>
      </DialogContent>
    </Dialog>}
  </Dialog>
}

function Stats({ devices, flows, host, faults, known, idle, failed, latency, exits, latencyFailed, trafficFailed }: {
  devices?: DeviceRow[]
  flows: TrafficView
  host?: HostMetrics
  faults: Fault[]
  known: boolean
  idle: boolean
  failed: boolean
  latency?: GatewayLatency
  exits: string[]
  latencyFailed: boolean
  trafficFailed: boolean
}) {
  const [limits, setLimits] = useState(readLimits)
  const [editing, setEditing] = useState<Direction | null>(null)
  const [sitesOpen, setSitesOpen] = useState(false)
  const saveLimits = (next: typeof limits) => {
    setLimits(next)
    try { localStorage.setItem(LIMITS_KEY, JSON.stringify(next)) } catch { /* private mode can disable storage */ }
  }
  const here = devices?.filter((d) => d.online).length
  const title = failed ? 'Status unavailable' : !known ? 'Checking…' : faults.length ? 'Needs attention' : idle ? 'Not set up' : 'All systems OK'
  const measured = !latencyFailed && latency?.state === 'ok' && latency.milliseconds != null
  const cpuPercent = host?.cpu_used_cores != null && host.cpu_cores > 0 ? host.cpu_used_cores / host.cpu_cores * 100 : undefined
  const memoryPercent = host?.memory_total_bytes ? host.memory_used_bytes / host.memory_total_bytes * 100 : undefined
  const dnsMeasured = !latencyFailed && latency?.dns_milliseconds != null
  const latencyValue = measured ? `${latency.milliseconds!.toFixed(0)} ms` : latency?.state === 'unreachable' ? 'No response' : '—'
  const rate = (direction: 'downRate' | 'upRate') => !trafficFailed && flows.rated ? formatRate(flows.total[direction] ?? 0) : trafficFailed ? 'Unavailable' : '—'
  return <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4 lg:gap-4">
    <StatCard title="Status">
      <div className="space-y-3 pt-2">
        <MetricPill value={title} health={faults.length || failed ? 'bad' : known && !idle ? 'good' : 'unknown'} />
        <MetricPill label="Uptime" value={host ? uptime(host.uptime_seconds) : '—'} detail={devices ? `${here} / ${devices.length} online` : '— online'} />
      </div>
    </StatCard>
    <StatCard title="Traffic">
      <div className="space-y-3 pt-2">
        <Meter label="Download" arrow="↓" onClick={() => setEditing('down')} current={rate('downRate')} maximum={limits.down ? formatRate(limits.down * 1_000_000 / 8) : undefined}
          percent={flows.rated && !trafficFailed && limits.down ? (flows.total.downRate ?? 0) * 8 / (limits.down * 1_000_000) * 100 : undefined} />
        <Meter label="Upload" arrow="↑" onClick={() => setEditing('up')} current={rate('upRate')} maximum={limits.up ? formatRate(limits.up * 1_000_000 / 8) : undefined}
          percent={flows.rated && !trafficFailed && limits.up ? (flows.total.upRate ?? 0) * 8 / (limits.up * 1_000_000) * 100 : undefined} tone="amber" />
      </div>
    </StatCard>
    <StatCard title={<span className="inline-flex items-center gap-1.5">Latency
      <button type="button" className="text-muted-foreground hover:text-foreground focus-visible:rounded-sm focus-visible:outline-2 focus-visible:outline-ring"
        title="Time for this router to receive the full HTTPS response, including DNS, TLS and download. Even a login redirect or error page counts; this does not prove the app works. Images, scripts and browser rendering are not included."
        aria-label="About latency measurements"><Info className="size-3.5" /></button></span>}>
      <div className="space-y-3 pt-2">
        <MetricPill label="Internet" value={latencyValue} detail={measured ? latency.milliseconds! < 1000 ? 'Good' : latency.milliseconds! < 3000 ? 'Fair' : 'Slow' : undefined} />
        <MetricPill label="DNS lookup" value={dnsMeasured ? `${latency.dns_milliseconds!.toFixed(0)} ms` : '—'} detail={dnsMeasured ? latency.dns_milliseconds! < 50 ? 'Good' : latency.dns_milliseconds! < 150 ? 'Fair' : 'Slow' : undefined} />
        <div className="flex flex-wrap items-center gap-2">
          {latency?.custom?.map((site) => {
            const measuredSite = Boolean(site.checked_at && !site.checked_at.startsWith('0001-'))
            const ms = site.milliseconds
            const tone = !measuredSite ? 'bg-muted text-muted-foreground' : ms == null ? 'bg-destructive/10 text-destructive'
              : ms < 1000 ? 'bg-success/15 text-success-foreground' : ms < 3000 ? 'bg-amber-500/10 text-amber-700 dark:text-amber-300' : 'bg-destructive/10 text-destructive'
            const description = `${site.name} · HTTPS response from ${new URL(site.url).hostname} via ${site.exit || 'router default'} · ${!measuredSite ? 'Waiting for first probe' : ms == null ? site.error || 'Failed' : `${ms.toFixed(0)} ms`}`
            return <span key={site.name} tabIndex={0} aria-label={description} title={description}
              className={cn('inline-flex min-h-9 items-center gap-1.5 rounded-xl px-2.5 text-xs font-semibold tabular-nums', tone)}>
              <SiteIcon name={site.name} url={site.url} icon={site.icon} />
              <span>{!measuredSite ? '—' : ms == null ? (site.error?.startsWith('HTTP ') ? site.error : site.error === 'Timed out' ? 'Timeout' : site.error === 'DNS lookup failed' ? 'DNS' : site.error === 'Gateway exit unavailable' ? 'Exit' : 'Failed') : `${ms.toFixed(0)} ms`}</span>
            </span>
          })}
          <button className="inline-flex min-h-9 items-center gap-1 rounded-xl px-2 text-xs text-muted-foreground hover:bg-muted hover:text-foreground"
            onClick={() => setSitesOpen(true)} aria-label="Monitor sites" title="Monitor sites"><Plus className="size-4" /><span className="sr-only">Monitor sites</span></button>
        </div>
      </div>
    </StatCard>
    <StatCard title="System">
      <div className="space-y-3 pt-2">
        <MetricPill label="CPU" value={host?.cpu_used_cores == null ? '—' : `${host.cpu_used_cores.toFixed(1)} cores`}
          detail={host?.cpu_cores ? `${cpuPercent?.toFixed(0)}% of ${host.cpu_cores} cores` : undefined} percent={cpuPercent} />
        <MetricPill label="Memory" value={host ? formatBytes(host.memory_used_bytes) : '—'}
          detail={host ? `${memoryPercent?.toFixed(0)}% of ${formatBytes(host.memory_total_bytes)}` : undefined} percent={memoryPercent} tone="neutral" />
      </div>
    </StatCard>
    {sitesOpen && <LatencySitesDialog sites={latency?.custom ?? []} exits={exits} onClose={() => setSitesOpen(false)} />}
    <BandwidthLimitDialog key={editing ?? 'closed'} direction={editing} limits={limits} onClose={() => setEditing(null)} onSave={saveLimits} />
  </div>
}

/**
 * Why the lines carry no traffic, when they do not.
 *
 * Only then. With counting on, the caveat about what is measured — traffic
 * through the router, not between two devices on one network — is on the
 * Traffic tile, where the number it qualifies is; a sentence under the map on
 * every visit saying the numbers were fine was read once and then only
 * scrolled past.
 */
function TrafficNote({
  traffic,
  failed,
  flows,
}: {
  traffic?: GatewayTraffic
  failed: boolean
  flows: TrafficView
}) {
  if ((!traffic && !failed) || flows.counting) return null
  return (
    <p className="flex items-start gap-2 text-xs text-muted-foreground">
      <Info className="mt-px size-3.5 shrink-0" aria-hidden />
      <span>
        {failed
          ? 'Traffic counts could not be read, so the lines do not show how busy each group is.'
          : 'Traffic is not being counted, so the lines do not show how busy each group is.'}{' '}
        <Link to="/gateway/usage" className="font-medium text-foreground underline-offset-4 hover:underline">
          Usage settings
        </Link>
      </span>
    </p>
  )
}
