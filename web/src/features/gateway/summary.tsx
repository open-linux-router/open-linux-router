import { t, useLocale } from '@/lib/i18n'
import { AlertTriangle, ChevronRight } from 'lucide-react'
import { useEffect, useState, type ReactNode } from 'react'
import { Link, useLocation } from 'react-router'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { ListEmpty } from '@/components/ui/list'
import { MetricValue } from '@/components/ui/metric-value'
import { PropertyRow, TechnicalValue } from '@/components/ui/property-row'
import { Skeleton } from '@/components/ui/skeleton'
import { useDhcpLeases, useDhcpStatus } from '@/features/dhcp/queries'
import { useDialStatus, useUplink } from '@/features/dial/queries'
import { useDnsStatus } from '@/features/dns/queries'
import { useGatewayStatus } from '@/features/gateway/queries'
import { useInterfaceTraffic, type RateSample } from '@/features/link/interface-traffic'
import { NetworkIcon } from '@/features/link/network-icon'
import { useInterfaces } from '@/features/link/queries'
import type { DialStatus, InterfaceList, InterfaceRow } from '@/lib/api-types'
import { cn, formatBytes, formatRate } from '@/lib/utils'

/**
 * The gateway, on the page you land on.
 *
 * The objects only: what this box is plugged into, how it reaches the internet,
 * and what it runs for the network. There is no status strip and there are no
 * switches. Routing's own state — on, off, behind its saved settings, or
 * unreadable — is a detail of the routing settings, and it lives there, at
 * `interfaces#forwarding`, beside the switches that change it and the sentences
 * that say what turning them off closes. The one entry to that page is the
 * Interfaces heading; Overview is where a gateway fault is reported. A landing
 * page that repeats the module's banner above its objects read as a second
 * status page rather than as this section's own.
 */
export function GatewaySummary() {
  useLocale()
  const { hash } = useLocation()

  useEffect(() => {
    if (!hash) return
    const frame = requestAnimationFrame(() => document.getElementById(hash.slice(1))?.scrollIntoView())
    return () => cancelAnimationFrame(frame)
  }, [hash])

  return (
    <div className="space-y-6">
      <InterfacesCard />
      <div className="grid gap-4 lg:grid-cols-2">
        <WayOutCard />
        <ServicesCard />
      </div>
    </div>
  )
}

/* -------------------------------------------------------------------------- */

const tone = {
  good: 'text-success-foreground',
  caution: 'text-warning-foreground',
  quiet: 'text-muted-foreground',
} as const

const dot = {
  good: 'bg-success',
  caution: 'bg-warning',
  quiet: 'bg-muted-foreground/40',
} as const

interface Health {
  label: string
  tone: keyof typeof tone
}

/* -------------------------------------------------------------------------- */

function InterfacesCard() {
  useLocale()
  const interfaces = useInterfaces()
  const uplink = useUplink()
  const dial = useDialStatus()
  const traffic = useInterfaceTraffic(interfaces.data, interfaces.isError)
  const [selected, setSelected] = useState<string | null>(null)
  const rows = (interfaces.data?.interfaces ?? []).filter(row => row.physical)
  const chosen = rows.find(row => row.name === selected)
  // The configured uplink when olr owns it, otherwise the interface the observed
  // default route leaves by — the WAN is a fact, not only a setting.
  const wan = uplink.isError ? undefined : (uplink.data?.uplink?.interface ?? dial.data?.route?.dev)
  const chosenRole = chosen ? roleOf(chosen, wan, interfaces.data) : undefined

  return (
    <>
      <Card id="interfaces" aria-labelledby="interfaces-heading" className="scroll-mt-20">
        <CardHeader>
          <CardTitle id="interfaces-heading">{t('Interfaces')}</CardTitle>
          <CardAction>
            <Link to="/gateway/interfaces" className={sectionLink}>
              Interfaces &amp; routing <ChevronRight className="size-3.5" aria-hidden />
            </Link>
          </CardAction>
        </CardHeader>
        <CardContent>
          {interfaces.isPending && (
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {[0, 1, 2].map(i => <Skeleton key={i} className="h-[136px] rounded-xl" />)}
            </div>
          )}
          {interfaces.isError && (
            <Alert variant="destructive">
              <AlertTriangle />
              <AlertTitle>{t('Could not load interfaces')}</AlertTitle>
              <AlertDescription>{interfaces.error.message}</AlertDescription>
            </Alert>
          )}
          {interfaces.isSuccess && rows.length === 0 && (
            <ListEmpty> {t('No physical network interfaces found.')}{' '}
              <Link to="/gateway/interfaces" className="underline underline-offset-4">{t('Open interface settings')}</Link>
            </ListEmpty>
          )}
          {rows.length > 0 && (
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {rows.map(row => (
                <InterfaceTile
                  key={row.name}
                  row={row}
                  role={roleOf(row, wan, interfaces.data)}
                  current={traffic.get(row.name)?.history.at(-1)}
                  failed={Boolean(interfaces.isError)}
                  onSelect={() => setSelected(row.name)}
                />
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <Dialog open={selected !== null} onOpenChange={open => !open && setSelected(null)}>
        <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{selected}</DialogTitle>
            <DialogDescription>
              {interfaces.isError ? t('Interface readings unavailable.')
                : chosen ? `${connectionState(chosen, chosenRole)}${chosenRole ? ` · ${chosenRole}` : ''}`
                  : 'This interface is no longer present.'}
            </DialogDescription>
          </DialogHeader>
          {interfaces.isError && <p className="text-sm text-destructive">{t('Interface readings unavailable.')}</p>}
          {chosen && (
            <dl className="divide-y text-sm">
              <Detail label={t('Network')} value={chosen.network ?? interfaces.data?.networks.find(n => n.members.includes(chosen.name))?.name ?? 'Not assigned'} />
              <Detail label={t('Physical link')} value={interfaces.isError ? t('Unavailable') : !chosen.present ? t('Not found') : !chosen.up ? t('Down') : chosen.running ? 'Detected' : t('Disconnected')} />
              <Detail label={t('Link speed')} value={interfaces.isError ? '—' : linkSpeed(chosen.speed_mbps)} />
              <Detail label="MAC" value={chosen.mac ?? '—'} />
              <Detail label={t('Addresses')} value={chosen.prefixes?.join(', ') || 'Not assigned'} />
              <Detail label={t('Receive total')} value={!interfaces.isError && chosen.rx_bytes !== undefined ? formatBytes(chosen.rx_bytes) : '—'} />
              <Detail label={t('Transmit total')} value={!interfaces.isError && chosen.tx_bytes !== undefined ? formatBytes(chosen.tx_bytes) : '—'} />
            </dl>
          )}
          {chosen && !interfaces.isError && !chosen.speed_mbps && (
            <p className="text-sm text-muted-foreground">
              Link speed is unavailable from the system. Some drivers, especially virtual network
              drivers, do not report it. Traffic rates are measured separately.
            </p>
          )}
          <Link to={chosen ? `/gateway/interfaces/${encodeURIComponent(chosen.name)}` : '/gateway/interfaces'} className={detailLink}> {t('Interface settings')} <ChevronRight className="size-3.5" aria-hidden />
          </Link>
          <Link to="/gateway/interfaces" className={detailLink}> {t('All interfaces and routing')} <ChevronRight className="size-3.5" aria-hidden />
          </Link>
        </DialogContent>
      </Dialog>
    </>
  )
}

/**
 * One interface, three lines: what it is, what address it holds, what it is
 * moving.
 *
 * The role moved out of the picture. Drawn inside the 48px glyph it was 9px
 * text and two 3px squares that had no name, which read as decoration because
 * it was. Link speed left too: on a box whose drivers do not report it, the
 * card said "Unknown" three times and taught nobody anything, while the dialog
 * still has the value and the sentence that explains its absence.
 */
function InterfaceTile({ row, role, current, failed, onSelect }: {
  row: InterfaceRow
  role?: string
  current?: RateSample
  failed: boolean
  onSelect: () => void
}) {
  useLocale()
  const state = failed ? t('Unavailable') : connectionState(row, role)
  const connected = state === 'Connected'
  const address = row.address || row.prefixes?.[0]?.split('/')[0]
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-label={`${row.name}${role ? `, ${role}` : ''}: ${state}${address ? `, ${address}` : ''}. Details`}
      className="flex h-full min-w-0 flex-col rounded-xl bg-muted p-4 text-left transition-colors duration-150 hover:bg-accent active:bg-accent/80 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
    >
      <div className="flex items-start gap-3">
        <NetworkIcon kind="interface" className={cn('size-10 shrink-0', !connected && 'opacity-60 grayscale')} />
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-1.5">
            <span className="truncate text-sm font-semibold">{row.name}</span>
            {role && (
              <span className="shrink-0 rounded-md bg-card px-1.5 py-0.5 text-xs leading-4 font-medium text-muted-foreground">
                {role}
              </span>
            )}
          </div>
          <span className={cn('mt-0.5 block truncate text-[0.8125rem]', tone[stateTone(state)])}>{state}</span>
        </div>
        <ChevronRight className="mt-1 size-4 shrink-0 text-muted-foreground/50" aria-hidden />
      </div>
      <div className="mt-3 min-w-0 text-[0.8125rem] leading-5">
        {address
          ? <span className="block font-mono [overflow-wrap:anywhere]">{address}</span>
          : <span className="block text-muted-foreground">{t('No address')}</span>}
      </div>
      <div className="mt-auto flex flex-wrap items-center gap-x-4 gap-y-1 pt-3 text-sm font-medium tabular-nums">
        {failed ? <span className="text-muted-foreground">No readings</span>
          : !connected ? <span className="text-muted-foreground">No traffic</span>
            : current ? (
              <>
                <span className="text-receive" aria-label={`Receive ${formatRate(current.rx)}`}>↓ <MetricValue value={formatRate(current.rx)} /></span>
                <span className="text-transmit" aria-label={`Transmit ${formatRate(current.tx)}`}>↑ <MetricValue value={formatRate(current.tx)} /></span>
              </>
            ) : <span className="text-muted-foreground">Measuring…</span>}
      </div>
    </button>
  )
}

/* -------------------------------------------------------------------------- */

function WayOutCard() {
  useLocale()
  const dial = useDialStatus()
  const status = useGatewayStatus()
  const foreign = status.data?.foreign?.length ?? 0
  const routes = dial.data?.default_routes ?? []

  return (
    <Card aria-labelledby="way-out-heading">
      <CardHeader><CardTitle id="way-out-heading">{t('Way out')}</CardTitle></CardHeader>
      <CardContent>
        <dl>
          <RouteRow label="IPv4" routes={routes.filter(route => route.family === 4)} dial={dial} />
          <RouteRow label="IPv6" routes={routes.filter(route => route.family === 6)} dial={dial} />
          <PropertyRow label="Resolvers">
            {dial.isError ? <span className="text-muted-foreground">{t('Unavailable')}</span>
              : !dial.data ? <span className="text-muted-foreground">Reading…</span>
                : dial.data.resolvers?.length
                  ? <TechnicalValue>{dial.data.resolvers.join(', ')}</TechnicalValue>
                  : <span className="text-muted-foreground">{t('None')}</span>}
          </PropertyRow>
          {foreign > 0 && (
            <PropertyRow label="Other rules">
              <Link to="/gateway/unmanaged" className="inline-flex items-center gap-1 text-primary underline-offset-4 hover:underline">
                {foreign} rule{foreign === 1 ? '' : 's'} <ChevronRight className="size-3.5" aria-hidden />
              </Link>
            </PropertyRow>
          )}
        </dl>
      </CardContent>
    </Card>
  )
}

function RouteRow({ label, routes, dial }: {
  label: string
  routes: DialStatus['default_routes']
  dial: ReturnType<typeof useDialStatus>
}) {
  useLocale()
  return (
    <PropertyRow label={label}>
      {dial.isError ? <span className="text-muted-foreground">{t('Unavailable')}</span>
        : !dial.data ? <span className="text-muted-foreground">Reading…</span>
          : routes.length === 0 ? <span className="text-muted-foreground">{t('None')}</span>
            : (
              <span className="flex flex-col gap-1">
                {routes.map((route, i) => (
                  <span key={i} className="flex flex-wrap items-baseline justify-end gap-x-2">
                    <TechnicalValue>{route.via ? `${route.via} via ${route.dev}` : `Direct on ${route.dev}`}</TechnicalValue>
                    {route.gateway_state === 'silent' && (
                      <span className="text-xs font-medium text-warning-foreground">not answering</span>
                    )}
                  </span>
                ))}
              </span>
            )}
    </PropertyRow>
  )
}

/* -------------------------------------------------------------------------- */

function ServicesCard() {
  useLocale()
  const dhcp = useDhcpStatus()
  const leases = useDhcpLeases()
  const dns = useDnsStatus()

  const leaseCount = leases.isError ? undefined : leases.data?.leases.filter(lease => lease.active).length
  const queries = dns.isError || dns.data?.stats_error ? undefined : dns.data?.stats?.queries

  return (
    <Card aria-labelledby="services-heading">
      <CardHeader><CardTitle id="services-heading">{t('Services')}</CardTitle></CardHeader>
      <CardContent className="px-0">
        <div className="divide-y">
          <ServiceRow
            id="dhcp"
            kind="dhcp"
            name="DHCP"
            to="/gateway/dhcp/details"
            health={describeDhcp(dhcp)}
            note={leases.isPending
              ? <Skeleton className="h-4 w-24" />
              : leaseCount === undefined ? 'Active leases unavailable'
                : leaseCount === 0 ? 'No active leases'
                  : `${leaseCount.toLocaleString()} active lease${leaseCount === 1 ? '' : 's'}`}
          />
          <ServiceRow
            id="dns"
            kind="dns"
            name="DNS"
            to="/gateway/dns/details"
            health={describeDns(dns)}
            note={dns.isPending
              ? <Skeleton className="h-4 w-24" />
              : queries === undefined ? 'Query count unavailable'
                : `${queries.toLocaleString()} quer${queries === 1 ? 'y' : 'ies'} since restart`}
          />
        </div>
      </CardContent>
    </Card>
  )
}

/** The count is the fact a visit comes for; the state is the answer, so it sits
 *  where the eye lands last and both rows line up.
 *
 *  A row of the list contract, built inside the card rather than as a List of
 *  its own: the List primitive draws its own border and radius, and a second
 *  panel around two rows is the nested surface this page set out to remove. */
function ServiceRow({ id, kind, name, note, health, to }: {
  id: string
  kind: 'dhcp' | 'dns'
  name: string
  note: ReactNode
  health: Health
  to: string
}) {
  useLocale()
  return (
    <Link
      id={id}
      to={to}
      className="flex min-h-16 scroll-mt-20 items-center gap-3 px-4 py-3 transition-colors duration-150 hover:bg-accent/60 active:bg-accent focus-visible:-outline-offset-2 focus-visible:outline-2 focus-visible:outline-ring sm:px-6"
    >
      <NetworkIcon kind={kind} className="size-8 shrink-0" />
      <div className="min-w-0 flex-1">
        <div className="text-sm text-foreground">{name}</div>
        <div className="mt-0.5 text-[0.8125rem] leading-relaxed text-muted-foreground">{note}</div>
      </div>
      <span className={cn('flex shrink-0 items-center gap-1.5 text-[0.8125rem] font-medium', tone[health.tone])}>
        <span className={cn('size-1.5 rounded-full', dot[health.tone])} aria-hidden />
        {health.label}
      </span>
      <ChevronRight className="size-4 shrink-0 text-muted-foreground" aria-hidden />
    </Link>
  )
}

function describeDhcp(dhcp: ReturnType<typeof useDhcpStatus>): Health {
  if (dhcp.isError) return { get label() { return t('Unavailable') }, tone: 'caution' }
  if (!dhcp.data) return { label: 'Reading…', tone: 'quiet' }
  if (!dhcp.data.enabled) return { get label() { return t('Off') }, tone: 'quiet' }
  if (dhcp.data.service_error) return { get label() { return t('Unavailable') }, tone: 'caution' }
  if (dhcp.data.drift_error || dhcp.data.blockers?.length) return { get label() { return t('Needs attention') }, tone: 'caution' }
  if (!dhcp.data.service?.active) return { get label() { return t('Not running') }, tone: 'caution' }
  return dhcp.data.drifted ? { get label() { return t('Needs attention') }, tone: 'caution' } : { get label() { return t('Running') }, tone: 'good' }
}

function describeDns(dns: ReturnType<typeof useDnsStatus>): Health {
  if (dns.isError) return { get label() { return t('Unavailable') }, tone: 'caution' }
  if (!dns.data) return { label: 'Reading…', tone: 'quiet' }
  if (!dns.data.enabled) return { get label() { return t('Off') }, tone: 'quiet' }
  if (dns.data.services.some(service => service.error)) return { get label() { return t('Unavailable') }, tone: 'caution' }
  if (dns.data.drift_error || dns.data.blockers?.length) return { get label() { return t('Needs attention') }, tone: 'caution' }
  const running = dns.data.services.length > 0 && dns.data.services.every(service => service.status?.active)
  if (!running) return { get label() { return t('Not running') }, tone: 'caution' }
  return dns.data.drifted ? { get label() { return t('Needs attention') }, tone: 'caution' } : { get label() { return t('Running') }, tone: 'good' }
}

/* -------------------------------------------------------------------------- */

const sectionLink = 'inline-flex min-h-11 items-center gap-1 text-sm font-medium text-primary underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring sm:min-h-9'
const detailLink = 'inline-flex min-h-11 items-center gap-1 text-sm font-medium text-primary underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring'

function Detail({ label, value }: { label: string; value: string }) {
  useLocale()
  return <PropertyRow label={label} className="py-3">{label === 'MAC' || label === 'Addresses' ? <TechnicalValue>{value}</TechnicalValue> : value}</PropertyRow>
}

function roleOf(row: InterfaceRow, wan: string | undefined, data?: InterfaceList) {
  if (row.name === wan) return 'WAN'
  if (row.network || data?.networks.some(network => network.members.includes(row.name))) return 'LAN'
  return undefined
}

function stateTone(state: string): keyof typeof tone {
  if (state === 'Connected') return 'good'
  if (state === 'Not found' || state === 'Unavailable') return 'caution'
  return 'quiet'
}

function connectionState(row: InterfaceRow, role?: string) {
  return !row.present ? t('Not found') : !row.up ? t('Down') : !row.running ? t('Disconnected')
    : !role && !row.network && !row.address && !row.prefixes?.length ? 'Inactive' : t('Connected')
}

function linkSpeed(mbps?: number) {
  return !mbps ? t('Unknown') : mbps >= 1000 ? `${mbps / 1000} Gbps` : `${mbps} Mbps`
}
