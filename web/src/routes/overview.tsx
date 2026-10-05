import { AlertTriangle, ArrowDown, ArrowUp, ChevronRight, Info, ShieldCheck } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Link } from 'react-router'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { useDeviceActions } from '@/features/devices/device-actions'
import { useGroupActions } from '@/features/devices/group-actions'
import { useDeviceList, useDevicesConfig } from '@/features/devices/queries'
import { useDhcpConfig, useDhcpStatus } from '@/features/dhcp/queries'
import { useDialStatus } from '@/features/dial/queries'
import { useDnsStatus } from '@/features/dns/queries'
import { RELAY_UNIT, serviceOf } from '@/features/dns/units'
import { useIngressConfig, useIngressStatus } from '@/features/ingress/queries'
import { useDiscoveredWeb } from '@/features/topology/discovered'
import { servicesByDevice } from '@/features/topology/services'
import { useGatewayLatency, useGatewayStatus, useGatewayTraffic } from '@/features/gateway/queries'
import { FirstRun } from '@/features/setup/first-run'
import { NetworkMap } from '@/features/topology/network-map'
import { buildOutside } from '@/features/topology/outside'
import { useTrafficView, type TrafficView } from '@/features/topology/traffic'
import type { DeviceRow, DhcpStatus, DnsStatus, GatewayLatency, GatewayStatus, GatewayTraffic } from '@/lib/api-types'
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
  const traffic = useGatewayTraffic()
  const latency = useGatewayLatency()
  const identity = useDevicesConfig()
  const ingress = useIngressConfig()
  const ingressStatus = useIngressStatus()
  const discovered = useDiscoveredWeb()
  const services = useMemo(() => servicesByDevice(ingress.data, devices.data?.devices ?? []), [ingress.data, devices.data])
  const flows = useTrafficView(traffic.data, traffic.isError)
  const history = useRateHistory(flows)
  const dial = useDialStatus()
  const outside = useMemo(() => buildOutside(dial.data, devices.data?.devices), [dial.data, devices.data])
  const actions = useDeviceActions()
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
      <Stats devices={devices.data?.devices} traffic={traffic.data} flows={flows} history={history}
        faults={faults} known={known} idle={idle} failed={dhcp.isError || dns.isError || gateway.isError}
        latency={latency.data} latencyReadAt={latency.dataUpdatedAt} latencyFailed={latency.isError} trafficFailed={traffic.isError} />

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

      <section aria-label="Your network" className="space-y-4 pt-6">
        <NetworkMap
          devices={devices.data?.devices ?? []}
          services={services}
          discovered={discovered.data}
          serviceDomain={ingressStatus.data?.domain}
          groups={identity.data?.groups}
          traffic={flows}
          assignments={gateway.data?.assignments}
          exits={gateway.data?.exits}
          outside={outside}
          pools={dhcpConfig.data?.pools}
          pending={devices.isPending}
          density="auto"
          onSelect={actions.select}
          onCreateGroup={groupActions.create}
          onRenameGroup={groupActions.rename}
          onDeleteGroup={groupActions.remove}
          onMoveGroup={groupActions.moveGroup}
          onMoveDevice={groupActions.move}
        />

        <TrafficNote traffic={traffic.data} failed={traffic.isError} flows={flows} />
      </section>

      {actions.dialogs}
      {groupActions.dialogs}
    </div>
  )
}

function BigRate({ label, icon: Icon, rate }: { label: string; icon: typeof ArrowDown; rate: number }) {
  const [value, unit] = formatRate(rate).split(' ')
  return (
    <div>
      <div className="flex items-center gap-1 text-xs text-muted-foreground">
        <Icon className="size-3" aria-hidden />
        {label}
      </div>
      <div className="tabular-nums">
        <span className="text-2xl leading-9 font-semibold tracking-tight">{value}</span>
        <span className="ml-1 text-sm text-muted-foreground">{unit}</span>
      </div>
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

/**
 * The total rate at each reading since the page was opened, newest last.
 *
 * olrd keeps no history, so this is the only kind of trend the page can draw
 * honestly: what this tab has itself seen. It starts empty and fills from the
 * right; nothing before the first reading is drawn, because nothing before it
 * was measured.
 */
const HISTORY = 60

function useRateHistory(flows: TrafficView): number[] {
  const [seen, setSeen] = useState<{ total: TrafficView['total'] | null; rates: number[] }>({
    total: null,
    rates: [],
  })
  // Appended during render when a new reading arrives — the same derive-from-
  // a-changing-value pattern useTrafficView uses, keyed on the total object,
  // which is new exactly when a new sample was taken.
  if (flows.rated && flows.total !== seen.total) {
    const rate = (flows.total.downRate ?? 0) + (flows.total.upRate ?? 0)
    setSeen({ total: flows.total, rates: [...seen.rates, rate].slice(-HISTORY) })
  }
  return flows.counting ? seen.rates : []
}

/**
 * What the counters say, one tile each.
 *
 * Only what the data supports. The one line on the row is the rate as this
 * tab has watched it (useRateHistory); there is no "+12% from last week",
 * because there is no last week to compare with.
 */
function Stats({ devices, traffic, flows, history, faults, known, idle, failed, latency, latencyReadAt, latencyFailed, trafficFailed }: {
  devices?: DeviceRow[]
  traffic?: GatewayTraffic
  flows: TrafficView
  history: number[]
  faults: Fault[]
  known: boolean
  idle: boolean
  failed: boolean
  latency?: GatewayLatency
  latencyReadAt: number
  latencyFailed: boolean
  trafficFailed: boolean
}) {
  const here = devices?.filter((d) => d.online).length
  const moved = traffic?.usage.reduce((sum, u) => sum + u.up_bytes + u.down_bytes, 0)
  const healthy = known && !failed && !idle && faults.length === 0
  const title = failed ? 'Status unavailable' : !known ? 'Checking…' : faults.length ? 'Needs attention' : idle ? 'Not set up' : 'All systems OK'
  const stale = latency?.checked_at ? latencyReadAt - Date.parse(latency.checked_at) > 90 * 1000 : false
  const measured = !latencyFailed && !stale && latency?.state === 'ok' && latency.milliseconds != null
  const latencyQuality = measured ? latency.milliseconds! < 100 ? 'Good' : latency.milliseconds! < 200 ? 'Fair' : 'Slow' : undefined
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4 lg:gap-4">
      <Stat label="Status" loading={false}>
        <div className="my-auto flex items-center gap-4 py-3">
          <span className={cn('flex size-14 shrink-0 items-center justify-center rounded-2xl', healthy
            ? 'bg-success/15 text-success ring-1 ring-success/25'
            : faults.length ? 'bg-destructive/10 text-destructive' : 'bg-muted text-muted-foreground')}>
            {healthy ? <ShieldCheck className="size-9" strokeWidth={1.8} aria-hidden /> : faults.length || failed ? <AlertTriangle className="size-8" aria-hidden /> : <Info className="size-8" aria-hidden />}
          </span>
          <div className="min-w-0">
            <p className="text-lg font-semibold tracking-tight">{title}</p>
            <p className="mt-1 text-xs text-muted-foreground">{failed ? 'Could not read router status' : !known ? 'Reading router status' : faults.length ? `${faults.length} ${faults.length === 1 ? 'issue' : 'issues'} to review below` : idle ? 'Configure your network to get started' : 'Router services are running smoothly'}</p>
          </div>
        </div>
      </Stat>
      <Stat label="Traffic" loading={false} title="Traffic through the router; local traffic within one network is not counted.">
        <div className="relative z-10 mt-2 mb-1 flex flex-wrap gap-x-5 gap-y-1">
          {flows.rated ? <>
            <BigRate label="Download" icon={ArrowDown} rate={flows.total.downRate ?? 0} />
            <BigRate label="Upload" icon={ArrowUp} rate={flows.total.upRate ?? 0} />
          </> : <p className="py-2 text-sm text-muted-foreground">{trafficFailed ? 'Traffic unavailable' : traffic?.counting ? 'Measuring traffic…' : traffic ? 'Not being counted' : 'Loading…'}</p>}
        </div>
        <p className="relative z-10 mb-6 text-xs text-muted-foreground">{!trafficFailed && traffic?.counting && moved !== undefined ? `${formatBytes(moved)} total · since counting started` : '— total'}</p>
        {history.length > 1 && <Sparkline values={history} />}
      </Stat>
      <Stat label="Latency" loading={!latency && !latencyFailed}
        value={measured ? latency.milliseconds!.toFixed(0) : '—'} unit={measured ? 'ms' : undefined}
        hint={latencyFailed || stale ? 'Measurement unavailable' : !measured ? latency?.state === 'unreachable' ? 'No response' : latency?.state === 'unavailable' ? 'Measurement unavailable' : 'Measuring…' : undefined}>
        {latencyQuality && <div className={cn('mt-2 flex items-center gap-2 text-xs font-medium',
          latencyQuality === 'Good' ? 'text-success-foreground' : latencyQuality === 'Fair' ? 'text-muted-foreground' : 'text-destructive')}>
          <span className={cn('size-1.5 rounded-full', latencyQuality === 'Good' ? 'bg-success' : latencyQuality === 'Fair' ? 'bg-muted-foreground' : 'bg-destructive')} aria-hidden />
          {latencyQuality}
        </div>}
      </Stat>
      <Stat label="Devices" value={here === undefined ? undefined : String(here)} unit={devices ? `of ${devices.length}` : undefined}
        hint={devices ? `${devices.length - (here ?? 0)} offline` : undefined}>
        {devices && devices.length > 0 && <Presence devices={devices} />}
      </Stat>
    </div>
  )
}

/**
 * One tile. `value` undefined is still loading, unless the tile says it has
 * something else to show instead of a figure.
 */
function Stat({
  label,
  title,
  value,
  unit,
  hint,
  loading = value === undefined,
  children,
}: {
  label: string
  title?: string
  value?: string
  unit?: string
  hint?: React.ReactNode
  loading?: boolean
  children?: React.ReactNode
}) {
  return (
    <div
      title={title}
      className="relative flex min-h-40 min-w-0 flex-col overflow-hidden rounded-2xl bg-card p-4 shadow-xs ring-1 ring-foreground/[0.07]"
    >
      <div className="truncate text-[13px] font-medium text-muted-foreground">{label}</div>
      {loading ? (
        <Skeleton className="mt-2 h-8 w-20" />
      ) : (
        value !== undefined && (
          <div className="mt-1 truncate tabular-nums">
            <span className="text-[28px] leading-9 font-semibold tracking-tight">{value}</span>
            {unit && <span className="ml-1 text-sm font-medium text-muted-foreground">{unit}</span>}
          </div>
        )
      )}
      {hint && <div className="relative z-10 text-xs text-muted-foreground">{hint}</div>}
      {children}
    </div>
  )
}

/**
 * The rate as this tab has seen it, as a filled line along the tile's foot,
 * first reading at the left edge and the latest at the right.
 */
function Sparkline({ values }: { values: number[] }) {
  const W = 300
  const H = 44
  // Headroom, so the line stays clear of the words above it.
  const max = Math.max(...values) * 1.6 || 1
  const pts = values.map((v, i) => [(i / (values.length - 1)) * W, H - 3 - (v / max) * (H - 6)])
  const line = pts.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(1)},${y.toFixed(1)}`).join(' ')
  const area = `${line} L${W},${H} L${pts[0][0].toFixed(1)},${H} Z`
  return (
    <svg
      aria-hidden
      viewBox={`0 0 ${W} ${H}`}
      preserveAspectRatio="none"
      className="pointer-events-none absolute inset-x-0 bottom-0 h-10 w-full text-foreground/40"
    >
      <path d={area} className="fill-foreground/[0.05]" />
      <path d={line} fill="none" stroke="currentColor" strokeWidth={1.5} vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
    </svg>
  )
}

/**
 * One dot per device, here ones filled. Past a few dozen the dots stop being
 * countable and become a bar, which says the same share without pretending
 * each dot can be told apart.
 */
function Presence({ devices }: { devices: DeviceRow[] }) {
  const here = devices.filter((d) => d.online).length
  if (devices.length > 40) {
    return (
      <div className="mt-auto pt-3">
        <div className="h-1.5 overflow-hidden rounded-full bg-muted">
          <div className="h-full rounded-full bg-success" style={{ width: `${(here / devices.length) * 100}%` }} />
        </div>
      </div>
    )
  }
  const sorted = [...devices].sort((a, b) => Number(b.online) - Number(a.online))
  return (
    <div className="mt-auto flex flex-wrap gap-1 pt-3">
      {sorted.map((d) => (
        <span
          key={d.mac}
          title={`${d.name || d.mac}${d.online ? '' : ' — away'}`}
          className={cn('size-2 rounded-full', d.online ? 'bg-success' : 'bg-muted-foreground/20')}
        />
      ))}
    </div>
  )
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
