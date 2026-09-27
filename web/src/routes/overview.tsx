import { AlertTriangle, ArrowDown, ArrowUp, Check, ChevronRight, Info } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { useDeviceActions } from '@/features/devices/device-actions'
import { useGroupActions } from '@/features/devices/group-actions'
import { useDeviceList, useDevicesConfig } from '@/features/devices/queries'
import { useDhcpConfig, useDhcpStatus } from '@/features/dhcp/queries'
import { useDnsStatus } from '@/features/dns/queries'
import { RELAY_UNIT, serviceOf } from '@/features/dns/units'
import { useGatewayStatus, useGatewayTraffic } from '@/features/gateway/queries'
import { FirstRun } from '@/features/setup/first-run'
import { shownExits } from '@/features/topology/model'
import { NetworkMap } from '@/features/topology/network-map'
import { useTrafficView, type TrafficView } from '@/features/topology/traffic'
import type { DeviceRow, DhcpStatus, DnsStatus, ExitStatus, GatewayStatus, GatewayTraffic } from '@/lib/api-types'
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
 * page's headline — one sentence, and when something is wrong it is that
 * thing, never a reassurance above a list of faults (design.md §5.6); the
 * faults follow in full. Then the counters; then the map, which is both the
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
  const identity = useDevicesConfig()
  const flows = useTrafficView(traffic.data, traffic.isError)
  const history = useRateHistory(flows)

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

      {!idle && (
        <Headline
          faults={faults}
          known={known}
          devices={devices.data?.devices}
          exits={gateway.data?.exits}
          flows={flows}
        />
      )}

      {faults.map((fault) => (
        <Alert key={fault.key} variant={fault.tone === 'bad' ? 'destructive' : 'default'}>
          <AlertTriangle />
          {/* A single fault is already the headline; saying it twice in a
              row is the page stammering. */}
          {faults.length > 1 && <AlertTitle>{fault.title}</AlertTitle>}
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

      <Stats
        devices={devices.data?.devices}
        dns={dns.data}
        gateway={gateway.data}
        traffic={traffic.data}
        flows={flows}
        history={history}
      />

      <section aria-label="Your network" className="space-y-4 pt-6">
        <NetworkMap
          devices={devices.data?.devices ?? []}
          groups={identity.data?.groups}
          traffic={flows}
          assignments={gateway.data?.assignments}
          exits={gateway.data?.exits}
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

/**
 * The page's one sentence: all is well, or the thing that is not.
 *
 * One fault is named; several are counted, and the alerts beneath say what
 * each one is. The live rates sit beside it once there are two readings to
 * make a rate from — before that there is nothing honest to show there.
 */
function Headline({
  faults,
  known,
  devices,
  exits,
  flows,
}: {
  faults: Fault[]
  known: boolean
  devices?: DeviceRow[]
  exits?: ExitStatus[]
  flows: TrafficView
}) {
  const bad = faults.some((f) => f.tone === 'bad')
  const title = !known
    ? undefined
    : faults.length === 0
      ? 'Everything’s running smoothly'
      : faults.length === 1
        ? faults[0].title
        : `${faults.length} things need you`

  const here = devices?.filter((d) => d.online).length
  const ways = shownExits(exits).filter((e) => e.probed)
  const facts = [
    devices && `${here} of ${devices.length} ${devices.length === 1 ? 'device' : 'devices'} here`,
    ways.length > 0 &&
      `${ways.filter((e) => e.up).length} of ${ways.length} ${ways.length === 1 ? 'way' : 'ways'} out working`,
  ].filter(Boolean)

  return (
    <div className="flex flex-wrap items-center gap-x-8 gap-y-5">
      <div className="flex min-w-0 flex-1 basis-80 items-center gap-4">
        {title === undefined ? (
          <Skeleton className="size-12 shrink-0 rounded-full" />
        ) : (
          <span
            className={cn(
              'flex size-12 shrink-0 items-center justify-center rounded-full',
              faults.length === 0
                ? 'bg-success text-white shadow-[0_6px_20px_-4px] shadow-success/60'
                : bad
                  ? 'bg-destructive text-white shadow-[0_6px_20px_-4px] shadow-destructive/50'
                  : 'bg-muted text-foreground',
            )}
          >
            {faults.length === 0 ? (
              <Check className="size-6" strokeWidth={2.75} aria-hidden />
            ) : (
              <AlertTriangle className="size-5.5" aria-hidden />
            )}
          </span>
        )}
        <div className="min-w-0">
          {title === undefined ? (
            <Skeleton className="h-7 w-72 max-w-full" />
          ) : (
            <h1 className="text-2xl font-bold tracking-tight text-balance sm:text-[28px]">{title}</h1>
          )}
          {facts.length > 0 && <p className="mt-0.5 text-sm text-muted-foreground sm:text-[15px]">{facts.join(' · ')}</p>}
        </div>
      </div>

      {flows.rated && (
        <div className="flex gap-7">
          <BigRate label="Download" icon={ArrowDown} rate={flows.total.downRate ?? 0} />
          <BigRate label="Upload" icon={ArrowUp} rate={flows.total.upRate ?? 0} />
        </div>
      )}
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
        <span className="text-[28px] leading-9 font-semibold tracking-tight">{value}</span>
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
function Stats({
  devices,
  dns,
  gateway,
  traffic,
  flows,
  history,
}: {
  devices?: DeviceRow[]
  dns?: DnsStatus
  gateway?: GatewayStatus
  traffic?: GatewayTraffic
  flows: TrafficView
  history: number[]
}) {
  const here = devices?.filter((d) => d.online).length
  const moved = traffic?.usage.reduce((sum, u) => sum + u.up_bytes + u.down_bytes, 0)
  const ways = shownExits(gateway?.exits)
  const rate = flows.rated ? formatRate((flows.total.downRate ?? 0) + (flows.total.upRate ?? 0)).split(' ') : undefined
  const share = dns?.stats && dns.stats.queries > 0 ? dns.stats.blocked / dns.stats.queries : undefined

  return (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-4 lg:gap-4">
      <Stat
        label="Traffic"
        title="Traffic through the router. Devices talking to each other on the same network are not counted."
        value={
          rate ? rate[0] : traffic === undefined ? undefined : traffic.counting && moved !== undefined ? formatBytes(moved) : '—'
        }
        unit={rate?.[1]}
        hint={
          traffic && !traffic.counting
            ? 'not being counted'
            : rate && moved !== undefined
              ? `${formatBytes(moved)} since counting started`
              : 'since counting started'
        }
      >
        {history.length > 1 && <Sparkline values={history} />}
      </Stat>
      <Stat
        label="Devices here"
        value={here === undefined ? undefined : String(here)}
        unit={devices ? `of ${devices.length}` : undefined}
      >
        {devices && devices.length > 0 && <Presence devices={devices} />}
      </Stat>
      <Stat
        label="DNS lookups"
        value={dns ? (dns.stats ? dns.stats.queries.toLocaleString() : '—') : undefined}
        hint={
          dns?.stats
            ? <>
                {dns.stats.blocked.toLocaleString()} blocked
                {/* The ring says the share on a wide tile; a narrow one has no
                    room for it beside the figure, so there it joins the words. */}
                {share !== undefined && <span className="sm:hidden"> · {Math.round(share * 100)}%</span>}
              </>
            : 'not answering'
        }
        aside={share !== undefined ? <Ring share={share} /> : undefined}
      />
      <Stat label="Ways out" value={gateway ? (ways.length === 0 ? '—' : undefined) : undefined} loading={!gateway}>
        {gateway &&
          (ways.length === 0 ? (
            <div className="mt-auto text-xs text-muted-foreground">none set up</div>
          ) : (
            <ExitList exits={ways} />
          ))}
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
  aside,
  loading = value === undefined,
  children,
}: {
  label: string
  title?: string
  value?: string
  unit?: string
  hint?: React.ReactNode
  aside?: React.ReactNode
  loading?: boolean
  children?: React.ReactNode
}) {
  return (
    <div
      title={title}
      className="relative flex min-h-32 min-w-0 flex-col overflow-hidden rounded-2xl bg-card p-4 shadow-xs ring-1 ring-foreground/[0.07]"
    >
      <div className="truncate text-[13px] font-medium text-muted-foreground">{label}</div>
      {aside && <div className="absolute top-3.5 right-3.5 hidden sm:block">{aside}</div>}
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
      {hint && <div className="relative z-10 truncate text-xs text-muted-foreground">{hint}</div>}
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

/** The blocked share of lookups. Neutral: blocking is the resolver working, not a fault. */
function Ring({ share }: { share: number }) {
  const r = 20
  const c = 2 * Math.PI * r
  return (
    <svg viewBox="0 0 48 48" className="size-12" role="img" aria-label={`${Math.round(share * 100)}% blocked`}>
      <circle cx="24" cy="24" r={r} fill="none" strokeWidth="4.5" className="stroke-muted" />
      <circle
        cx="24"
        cy="24"
        r={r}
        fill="none"
        strokeWidth="4.5"
        strokeLinecap="round"
        strokeDasharray={`${Math.max(share * c, share > 0 ? 2 : 0)} ${c}`}
        transform="rotate(-90 24 24)"
        className="stroke-foreground/70"
      />
      <text x="24" y="28" textAnchor="middle" className="fill-foreground text-[11px] font-semibold tabular-nums">
        {Math.round(share * 100)}%
      </text>
    </svg>
  )
}

/** Each way out and its state, in the same three words the map uses. */
function ExitList({ exits }: { exits: ExitStatus[] }) {
  const shown = exits.slice(0, 3)
  return (
    <ul className="mt-auto space-y-1.5 pt-3">
      {shown.map((e) => {
        const down = e.probed && !e.up
        return (
          <li key={e.name} className="flex items-center gap-2 text-sm">
            <span
              aria-hidden
              className={cn(
                'size-1.5 shrink-0 rounded-full',
                down ? 'bg-destructive' : e.probed ? 'bg-success' : 'bg-muted-foreground/30',
              )}
            />
            <span className="min-w-0 flex-1 truncate font-medium">{e.name}</span>
            <span className={cn('shrink-0 text-xs', down ? 'text-destructive' : 'text-muted-foreground')}>
              {down ? 'not responding' : e.probed ? 'working' : 'not checked'}
            </span>
          </li>
        )
      })}
      {exits.length > shown.length && (
        <li className="text-xs text-muted-foreground">+{exits.length - shown.length} more</li>
      )}
    </ul>
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
