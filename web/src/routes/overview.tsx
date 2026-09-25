import {
  AlertTriangle,
  ArrowDownUp,
  ChevronRight,
  Info,
  MonitorSmartphone,
  Plus,
  Search,
  SearchCheck,
  Waypoints,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Link } from 'react-router'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useDeviceActions } from '@/features/devices/device-actions'
import { useGroupActions } from '@/features/devices/group-actions'
import { useDeviceList, useDevicesConfig } from '@/features/devices/queries'
import { useDhcpConfig, useDhcpStatus } from '@/features/dhcp/queries'
import { useDnsStatus } from '@/features/dns/queries'
import { RELAY_UNIT, serviceOf } from '@/features/dns/units'
import { useGatewayStatus, useGatewayTraffic } from '@/features/gateway/queries'
import { FirstRun } from '@/features/setup/first-run'
import { NetworkMap, NO_NETWORK, Rates } from '@/features/topology/network-map'
import { useTrafficView, type TrafficView } from '@/features/topology/traffic'
import type { DeviceRow, DhcpStatus, DnsStatus, GatewayStatus, GatewayTraffic } from '@/lib/api-types'
import { cn, formatBytes } from '@/lib/utils'

/**
 * What the router is doing, on the page you land on.
 *
 * This used to be two cards and a placeholder saying the rest arrived with link
 * and dial — which stayed on screen after three more modules shipped, so an
 * operator's first view under-described their own router and the most urgent
 * fact it held, a way out that had stopped responding, appeared nowhere.
 *
 * The order is what an operator asks in order: is anything wrong, how much is
 * this network doing, and what is on it. Faults first and never merely implied
 * (design.md §5.6); then the counters; then the tree, which is both the only
 * place three modules' answers are joined into one picture and the only place
 * devices are listed.
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

  const actions = useDeviceActions()
  const groupActions = useGroupActions()
  const [filter, setFilter] = useState('')
  const [network, setNetwork] = useState('')

  // The filter offers the networks devices are actually on, not every network
  // the box has: choosing one with nobody on it would only empty the map.
  const networks = useMemo(() => {
    const rows = devices.data?.devices ?? []
    const names = [...new Set(rows.flatMap((d) => (d.network ? [d.network] : [])))].sort()
    return { names, unplaced: rows.some((d) => d.seen && !d.network) }
  }, [devices.data])

  const faults = collectFaults(dhcp.data, dns.data, gateway.data)

  return (
    <div className="space-y-6">
      {/* Above the faults, and above the counters, on exactly the boxes where
          all three are empty. It renders nothing once the router is doing
          something. */}
      <FirstRun />

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

      <Stats
        devices={devices.data?.devices}
        dns={dns.data}
        gateway={gateway.data}
        traffic={traffic.data}
        flows={flows}
      />

      <Card>
        <CardHeader>
          <CardTitle>Your network</CardTitle>
          {/* One line, not five. The caveat still has to be here — the diagram
              would otherwise be read as wiring — but an operator who wanted the
              long version would go looking, and one who did not was paying for
              it on every visit. */}
          <CardDescription>Your devices in the groups you made. Logical layout, not wiring.</CardDescription>
          <CardAction>
            <Button variant="outline" size="sm" onClick={groupActions.create}>
              <Plus aria-hidden /> New group
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent className="space-y-6">
          <div className="flex flex-wrap items-center gap-2 sm:flex-nowrap sm:gap-3">
            <div className="relative min-w-0 basis-full sm:flex-1">
              <Search
                className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground"
                aria-hidden
              />
              <Input
                className="pl-9"
                placeholder="Search by name, address or hardware"
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                aria-label="Search devices"
              />
            </div>
            {/* Networks are a filter now rather than the shape of the map. It
                only appears once there is more than one thing to choose. */}
            {networks.names.length + Number(networks.unplaced) > 1 && (
              <Select value={network || 'all'} onValueChange={(v) => setNetwork(!v || v === 'all' ? '' : v)}>
                <SelectTrigger className="w-40" aria-label="Network">
                  <SelectValue>
                    {(value: string) =>
                      value === 'all' ? 'All networks' : value === NO_NETWORK ? 'No network' : value
                    }
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All networks</SelectItem>
                  {networks.names.map((n) => (
                    <SelectItem key={n} value={n}>
                      <span className="font-mono">{n}</span>
                    </SelectItem>
                  ))}
                  {networks.unplaced && <SelectItem value={NO_NETWORK}>No network</SelectItem>}
                </SelectContent>
              </Select>
            )}
          </div>

          <NetworkMap
            devices={devices.data?.devices ?? []}
            groups={identity.data?.groups}
            traffic={flows}
            assignments={gateway.data?.assignments}
            exits={gateway.data?.exits}
            pools={dhcpConfig.data?.pools}
            pending={devices.isPending}
            filter={filter}
            network={network}
            onSelect={actions.select}
            onCreateGroup={groupActions.create}
            onRenameGroup={groupActions.rename}
            onDeleteGroup={groupActions.remove}
            onMoveDevice={groupActions.move}
          />

          <TrafficNote traffic={traffic.data} failed={traffic.isError} flows={flows} />
        </CardContent>
      </Card>

      {actions.dialogs}
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

/**
 * What the counters say, one tile each.
 *
 * Only what the data supports. There is no history behind any of these — olrd
 * keeps none — so there are no sparklines and no "+12% from last week", however
 * much a dashboard seems to want them. A trend line drawn from nothing would be
 * the one decoration on the page that is also a lie.
 */
function Stats({
  devices,
  dns,
  gateway,
  traffic,
  flows,
}: {
  devices?: DeviceRow[]
  dns?: DnsStatus
  gateway?: GatewayStatus
  traffic?: GatewayTraffic
  flows: TrafficView
}) {
  const here = devices?.filter((d) => d.online).length
  const probed = (gateway?.exits ?? []).filter((e) => e.probed)
  const working = probed.filter((e) => e.up).length
  const moved = traffic?.usage.reduce((sum, u) => sum + u.up_bytes + u.down_bytes, 0)

  return (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
      <Stat
        icon={MonitorSmartphone}
        label="Devices here"
        value={here === undefined ? undefined : String(here)}
        hint={devices ? `of ${devices.length} known` : undefined}
      />
      <Stat
        icon={SearchCheck}
        label="DNS lookups"
        value={dns ? (dns.stats ? dns.stats.queries.toLocaleString() : '—') : undefined}
        hint={dns?.stats ? `${dns.stats.blocked.toLocaleString()} blocked` : 'not answering'}
      />
      <Stat
        icon={ArrowDownUp}
        label="Through the router"
        value={moved === undefined ? undefined : formatBytes(moved)}
        hint={
          traffic && !traffic.counting ? (
            'not being counted'
          ) : flows.rated ? (
            // The one live figure on the row, and only once two samples exist.
            <Rates flow={flows.total} />
          ) : (
            'since counting started'
          )
        }
      />
      <Stat
        icon={Waypoints}
        label="Ways out working"
        // "0 of 0" reads as broken on a box with nothing to probe, so an
        // unprobed set says so rather than showing a ratio nobody measured.
        value={gateway ? (probed.length === 0 ? '—' : `${working} of ${probed.length}`) : undefined}
        hint={probed.length === 0 ? 'none being checked' : probed.map((e) => e.name).join(', ')}
        tone={probed.length > 0 && working < probed.length ? 'text-destructive' : undefined}
      />
    </div>
  )
}

function Stat({
  icon: Icon,
  label,
  value,
  hint,
  tone,
}: {
  icon: LucideIcon
  label: string
  value?: string
  hint?: React.ReactNode
  tone?: string
}) {
  return (
    <div className="flex min-w-0 items-start gap-3 rounded-xl border bg-card p-3 sm:p-4">
      <span className="hidden size-9 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground sm:flex">
        <Icon className="size-4.5" aria-hidden />
      </span>
      <div className="min-w-0 flex-1">
        <div className="truncate text-xs text-muted-foreground">{label}</div>
        {value === undefined ? (
          <Skeleton className="mt-1 h-7 w-16" />
        ) : (
          <div className={cn('truncate text-2xl leading-8 font-semibold tabular-nums', tone)}>
            {value}
          </div>
        )}
        {hint && <div className="truncate text-xs text-muted-foreground">{hint}</div>}
      </div>
    </div>
  )
}

/**
 * What the lines and bars are measuring, or why there are none.
 *
 * Both halves are said out loud. With counting off, the thin uniform edges are
 * "we do not know", and that needs a sentence and a way to fix it; with it on,
 * the numbers are traffic *through the router*, and a NAS busy serving a
 * laptop down the hall would read as idle without the caveat.
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
  if (!traffic && !failed) return null

  if (!flows.counting) {
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

  return (
    <p className="flex items-start gap-2 text-xs text-muted-foreground">
      <Info className="mt-px size-3.5 shrink-0" aria-hidden />
      <span>
        {flows.rated
          ? 'Rates are traffic through the router, averaged between the last two readings.'
          : 'Totals since counting started; rates appear after the next reading.'}{' '}
        Devices talking to each other on the same network are not counted.
      </span>
    </p>
  )
}
