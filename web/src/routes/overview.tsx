import { AlertTriangle, ChevronRight, Search } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Input } from '@/components/ui/input'
import { useDeviceActions } from '@/features/devices/device-actions'
import { useDeviceList } from '@/features/devices/queries'
import { useDhcpConfig, useDhcpStatus } from '@/features/dhcp/queries'
import { useDnsStatus } from '@/features/dns/queries'
import { RELAY_UNIT, serviceOf } from '@/features/dns/units'
import { useRoutingStatus, useRoutingTraffic } from '@/features/routing/queries'
import { NetworkMap } from '@/features/topology/network-map'
import type {
  DeviceRow,
  DhcpStatus,
  DnsStatus,
  RoutingStatus,
  RoutingTraffic,
} from '@/lib/api-types'
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
  const routing = useRoutingStatus()
  const traffic = useRoutingTraffic()

  const actions = useDeviceActions()
  const [filter, setFilter] = useState('')

  const faults = collectFaults(dhcp.data, dns.data, routing.data)

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl font-semibold tracking-tight">Overview</h1>
        <p className="text-sm text-muted-foreground">
          Your network, what it is doing, and anything that needs you.
        </p>
      </header>

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
        routing={routing.data}
        traffic={traffic.data}
      />

      <Card>
        <CardHeader>
          <CardTitle>Your network</CardTitle>
          {/* One line, not five. The caveat still has to be here — the diagram
              would otherwise be read as wiring — but an operator who wanted the
              long version would go looking, and one who did not was paying for
              it on every visit. */}
          <CardDescription>Grouped by network. Logical layout, not wiring.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center gap-3">
            <div className="relative min-w-0 flex-1">
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
            {devices.data && (
              <div className="hidden shrink-0 text-sm text-muted-foreground sm:block">
                {devices.data.devices.filter((d) => d.online).length} of{' '}
                {devices.data.devices.length} here
              </div>
            )}
          </div>

          <NetworkMap
            devices={devices.data?.devices ?? []}
            assignments={routing.data?.assignments}
            exits={routing.data?.exits}
            pools={dhcpConfig.data?.pools}
            pending={devices.isPending}
            filter={filter}
            onSelect={actions.select}
          />
        </CardContent>
      </Card>

      {actions.dialogs}
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
function collectFaults(dhcp?: DhcpStatus, dns?: DnsStatus, routing?: RoutingStatus): Fault[] {
  const out: Fault[] = []

  for (const exit of routing?.exits ?? []) {
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

  if (routing && !routing.known) {
    out.push({
      key: 'routing-unknown',
      title: 'The gateway settings are saved but not in force',
      detail:
        'This router could not read its own routing configuration, so nothing on the gateway screen is actually running. On Linux this usually means the daemon lacks permission to change routing.',
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
        'DNS is turned on but the server is stopped, so no device here can look up a name. To the people using them it looks like the internet is down.',
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
        'DHCP is turned on but the server is stopped. Devices already here keep their address until it expires; anything joining now gets none.',
      tone: 'bad',
      to: '/dhcp',
      action: 'Open DHCP',
    })
  }

  // A unit that runs but is not enabled costs nothing until the power goes out,
  // and then costs the whole network at once. Worth saying while it is cheap.
  for (const service of dns?.services ?? []) {
    if (!service.status || service.status.enabled) continue
    out.push({
      key: `boot-${service.unit}`,
      title: 'DNS will not come back after a reboot',
      detail: `${service.unit} is running, but is not set to start at boot.`,
      tone: 'warn',
      to: '/dns',
      action: 'Open DNS',
    })
  }

  const drifted: { name: string; to: string }[] = []
  if (dhcp?.drifted && !dhcp.drift_error) drifted.push({ name: 'DHCP', to: '/dhcp' })
  if (dns?.drifted && !dns.drift_error) drifted.push({ name: 'DNS', to: '/dns' })
  if (routing?.drifted) drifted.push({ name: 'The gateway', to: '/gateway' })
  for (const d of drifted) {
    out.push({
      key: `drift-${d.name}`,
      title: `${d.name} is not doing what its settings say`,
      detail:
        'Something changed it outside olr, or a change was never applied. Saving any change on that screen puts it back.',
      tone: 'warn',
      to: d.to,
      action: 'Take a look',
    })
  }

  return out
}

/* -------------------------------------------------------------------------- */

function Stats({
  devices,
  dns,
  routing,
  traffic,
}: {
  devices?: DeviceRow[]
  dns?: DnsStatus
  routing?: RoutingStatus
  traffic?: RoutingTraffic
}) {
  const here = devices?.filter((d) => d.online).length
  const probed = (routing?.exits ?? []).filter((e) => e.probed)
  const working = probed.filter((e) => e.up).length
  const moved = traffic?.usage.reduce((sum, u) => sum + u.up_bytes + u.down_bytes, 0)

  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
      <Stat
        label="Devices here"
        value={here === undefined ? undefined : String(here)}
        hint={devices ? `of ${devices.length} known` : undefined}
      />
      <Stat
        label="Looked up"
        value={dns ? (dns.stats ? dns.stats.queries.toLocaleString() : '—') : undefined}
        hint={dns?.stats ? `${dns.stats.blocked.toLocaleString()} blocked` : 'not answering'}
      />
      <Stat
        label="Through the router"
        value={moved === undefined ? undefined : formatBytes(moved)}
        hint={traffic && !traffic.counting ? 'not being counted' : 'since it started'}
      />
      <Stat
        label="Ways out working"
        // "0 of 0" reads as broken on a box with nothing to probe, so an
        // unprobed set says so rather than showing a ratio nobody measured.
        value={routing ? (probed.length === 0 ? '—' : `${working} of ${probed.length}`) : undefined}
        hint={probed.length === 0 ? 'none being checked' : undefined}
        tone={probed.length > 0 && working < probed.length ? 'text-destructive' : undefined}
      />
    </div>
  )
}

function Stat({
  label,
  value,
  hint,
  tone,
}: {
  label: string
  value?: string
  hint?: string
  tone?: string
}) {
  return (
    <div className="rounded-lg border px-3 py-2">
      {value === undefined ? (
        <Skeleton className="h-7 w-16" />
      ) : (
        <div className={cn('truncate text-xl font-semibold tabular-nums', tone)}>{value}</div>
      )}
      <div className="truncate text-xs text-muted-foreground">{label}</div>
      {hint && <div className="truncate text-xs text-muted-foreground/70">{hint}</div>}
    </div>
  )
}
