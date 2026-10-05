import { AlertTriangle, ChevronRight, Info } from 'lucide-react'
import { useMemo } from 'react'
import { Link, useNavigate } from 'react-router'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { useGroupActions } from '@/features/devices/group-actions'
import { useDeviceList, useDevicesConfig } from '@/features/devices/queries'
import { useDhcpConfig, useDhcpStatus } from '@/features/dhcp/queries'
import { useDialStatus } from '@/features/dial/queries'
import { useDnsStatus } from '@/features/dns/queries'
import { RELAY_UNIT, serviceOf } from '@/features/dns/units'
import { useIngressConfig, useIngressStatus } from '@/features/ingress/queries'
import { servicesByDevice } from '@/features/topology/services'
import { useHostMetrics, type HostMetrics } from '@/features/system/queries'
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
      <Stats devices={devices.data?.devices} traffic={traffic.data} flows={flows} host={host.data}
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
          serviceDomain={ingressStatus.data?.domain}
          groups={identity.data?.groups}
          traffic={flows}
          assignments={gateway.data?.assignments}
          exits={gateway.data?.exits}
          outside={outside}
          pools={dhcpConfig.data?.pools}
          pending={devices.isPending}
          density="auto"
          onSelect={(device) => navigate(`/devices/${encodeURIComponent(device.mac)}`)}
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

function Meter({ label, current, maximum, percent, tone = 'blue' }: {
  label: string
  current?: string
  maximum?: string
  percent?: number
  tone?: 'blue' | 'amber' | 'neutral'
}) {
  return <div className="min-w-0 py-2.5">
    <div className="mb-2 flex items-center justify-between gap-2 text-xs text-muted-foreground">
      <span>{label}</span><span className="tabular-nums">{percent == null ? '—' : `${percent.toFixed(0)}%`}</span>
    </div>
    <div role={percent == null ? undefined : 'progressbar'} aria-label={label} aria-valuenow={percent == null ? undefined : Math.round(percent)} aria-valuemin={0} aria-valuemax={100}
      className="relative flex h-11 items-center justify-between gap-2 overflow-hidden rounded-xl bg-muted/65 px-3 ring-1 ring-foreground/[0.06] shadow-[inset_0_1px_2px_rgba(0,0,0,0.03)]">
      {percent != null && <span aria-hidden className={cn('absolute inset-y-0 left-0 border-r',
        tone === 'amber' ? 'border-amber-400/50 bg-amber-200/40' : tone === 'neutral' ? 'border-slate-400/40 bg-slate-300/35' : 'border-cyan-500/40 bg-cyan-200/45')}
        style={{ width: `${Math.min(100, Math.max(0, percent))}%` }} />}
      <span className="relative z-10 truncate text-sm font-semibold tabular-nums">{current ?? '—'}</span>
      <span className="relative z-10 shrink-0 text-xs text-muted-foreground tabular-nums">{maximum ? `of ${maximum}` : 'Limit not set'}</span>
    </div>
  </div>
}

function StatCard({ title, children }: { title: string; children: React.ReactNode }) {
  return <section className="min-w-0 rounded-2xl bg-card p-5 shadow-xs ring-1 ring-foreground/[0.07]">
    <h2 className="mb-4 text-sm font-semibold">{title}</h2>{children}
  </section>
}

function Stats({ devices, traffic, flows, host, faults, known, idle, failed, latency, latencyReadAt, latencyFailed, trafficFailed }: {
  devices?: DeviceRow[]
  traffic?: GatewayTraffic
  flows: TrafficView
  host?: HostMetrics
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
  const title = failed ? 'Status unavailable' : !known ? 'Checking…' : faults.length ? 'Needs attention' : idle ? 'Not set up' : 'All systems OK'
  const stale = latency?.checked_at ? latencyReadAt - Date.parse(latency.checked_at) > 90_000 : false
  const measured = !latencyFailed && !stale && latency?.state === 'ok' && latency.milliseconds != null
  const cpuPercent = host?.cpu_used_cores != null && host.cpu_cores > 0 ? host.cpu_used_cores / host.cpu_cores * 100 : undefined
  const memoryPercent = host?.memory_total_bytes ? host.memory_used_bytes / host.memory_total_bytes * 100 : undefined
  const dnsMeasured = !latencyFailed && !stale && latency?.dns_milliseconds != null
  const latencyValue = measured ? `${latency.milliseconds!.toFixed(0)} ms` : latency?.state === 'unreachable' ? 'No response' : '—'
  const rate = (direction: 'downRate' | 'upRate') => flows.rated ? formatRate(flows.total[direction] ?? 0) : trafficFailed ? 'Unavailable' : '—'
  return <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4 lg:gap-4">
    <StatCard title="Status">
      <div className="min-h-24 py-3"><p className={cn('text-lg font-semibold', faults.length || failed ? 'text-destructive' : 'text-foreground')}>
        <span className={cn('mr-2 inline-block size-2 rounded-full align-middle', faults.length || failed ? 'bg-destructive' : known && !idle ? 'bg-success' : 'bg-muted-foreground')} />{title}</p></div>
      <div className="py-3"><p className="text-xs text-muted-foreground">Uptime</p><p className="mt-2 text-2xl font-semibold tabular-nums tracking-tight">{host ? uptime(host.uptime_seconds) : '—'}</p>
        <p className="mt-3 flex justify-between text-xs text-muted-foreground"><span>Devices online</span><span className="tabular-nums">{devices ? `${here} / ${devices.length}` : '—'}</span></p></div>
    </StatCard>
    <StatCard title="Traffic">
      <Meter label="↓ Download" current={rate('downRate')} />
      <Meter label="↑ Upload" current={rate('upRate')} tone="amber" />
      <p className="mt-1 text-xs text-muted-foreground">Since counting started <span className="ml-1 font-medium text-foreground">{!trafficFailed && traffic?.counting && moved != null ? formatBytes(moved) : '—'}</span></p>
    </StatCard>
    <StatCard title="Latency">
      <div className="py-2.5"><div className="flex justify-between text-xs text-muted-foreground"><span>Internet</span>{measured && <span className={cn('font-medium', latency.milliseconds! < 100 ? 'text-success-foreground' : latency.milliseconds! < 200 ? 'text-amber-600' : 'text-destructive')}>{latency.milliseconds! < 100 ? 'Good' : latency.milliseconds! < 200 ? 'Fair' : 'Slow'}</span>}</div><p className="mt-3 text-2xl font-semibold tabular-nums tracking-tight">{latencyValue}</p></div>
      <div className="py-2.5"><div className="flex justify-between text-xs text-muted-foreground"><span>DNS</span>{dnsMeasured && <span className={cn('font-medium', latency.dns_milliseconds! < 50 ? 'text-success-foreground' : latency.dns_milliseconds! < 150 ? 'text-amber-600' : 'text-destructive')}>{latency.dns_milliseconds! < 50 ? 'Good' : latency.dns_milliseconds! < 150 ? 'Fair' : 'Slow'}</span>}</div><p className="mt-3 text-2xl font-semibold tabular-nums tracking-tight">{dnsMeasured ? `${latency.dns_milliseconds!.toFixed(0)} ms` : '—'}</p></div>
    </StatCard>
    <StatCard title="System">
      <Meter label="CPU" current={host?.cpu_used_cores == null ? undefined : `${host.cpu_used_cores.toFixed(1)} cores`} maximum={host?.cpu_cores ? `${host.cpu_cores} cores` : undefined} percent={cpuPercent} />
      <Meter label="Memory" current={host ? formatBytes(host.memory_used_bytes) : undefined} maximum={host ? formatBytes(host.memory_total_bytes) : undefined} percent={memoryPercent} tone="neutral" />
    </StatCard>
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
