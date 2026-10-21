import { t, useLocale } from '@/lib/i18n'
import {
  ArrowRightLeft,
  ArrowUpFromLine,
  ChevronRight,
  MonitorSmartphone,
  Network,
  Search,
  Signpost,
  type LucideIcon,
} from 'lucide-react'
import type { ReactNode } from 'react'
import { Link } from 'react-router'

import { Skeleton } from '@/components/ui/skeleton'
import { useDhcpConfig, useDhcpLeases, useDhcpStatus } from '@/features/dhcp/queries'
import { useDialStatus, useUplink } from '@/features/dial/queries'
import { useDnsConfig, useDnsStatus } from '@/features/dns/queries'
import { useGatewayConfig } from '@/features/gateway/queries'
import { useInterfaces } from '@/features/link/queries'
import type { RouterAdvertisementMode } from '@/lib/config-types'
import { cn } from '@/lib/utils'

import { dot, tone, type Health } from './status-tone'

type Family = 'v4' | 'v6'

/**
 * The gateway as two stacks, one per family, each cut across the modules.
 *
 * A normal operator asks "does IPv6 work, and where do I fix it", not "is dial
 * or dhcp configured". Each stack is that question answered in the order the
 * traffic takes: upstream, LAN addressing, how clients are configured,
 * forwarding, which way out, and name resolution. Every card names the one page
 * that changes it.
 *
 * The stacks hold no configuration of their own. A card is a link to the
 * module's own editor, not a second copy of it: one object, one plan, one apply
 * path. That is the whole reason two stacks can repeat the same module — DNS
 * appears in both — without the two views disagreeing.
 *
 * IPv6 is not a module (design.md §4.3), and this page does not set it across
 * modules either. It reports each module's own answer and sends you there.
 */
export function GatewayStacks() {
  useLocale()
  return (
    <div className="space-y-8">
      <Stack id="ipv4" title={t('IPv4')} family="v4" />
      <Stack id="ipv6" title={t('IPv6')} family="v6" />
    </div>
  )
}

/** The stages in the order a packet meets them; both families keep that order. */
const STAGES: { key: string; hash?: string; Stage: (props: StageProps) => ReactNode }[] = [
  { key: 'upstream', Stage: UpstreamStage },
  { key: 'lan', Stage: LanStage },
  { key: 'clients', hash: 'dhcp', Stage: ClientsStage },
  { key: 'forwarding', Stage: ForwardingStage },
  { key: 'exit', Stage: ExitStage },
  { key: 'dns', hash: 'dns', Stage: DnsStage },
]

function Stack({ id, title, family }: { id: string; title: string; family: Family }) {
  return (
    <section className="space-y-4" aria-labelledby={`stack-${id}`}>
      <h2 id={`stack-${id}`} className="text-2xl leading-8 font-semibold">{title}</h2>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {STAGES.map(({ key, hash, Stage }) => (
          // The hash targets the v4 stack only: `/gateway#dhcp` and `#dns` are
          // redirects from the settings trees, and both point at the v4 card.
          <Stage key={key} family={family} id={family === 'v4' ? hash : undefined} />
        ))}
      </div>
    </section>
  )
}

interface StageProps {
  family: Family
  id?: string
}

/* -------------------------------------------------------------------------- */

function UpstreamStage({ family, id }: StageProps) {
  useLocale()
  const uplink = useUplink()
  const dial = useDialStatus()
  const interfaces = useInterfaces()
  const owned = uplink.data?.uplink
  const route = dial.data?.route
  const version = family === 'v4' ? 4 : 6
  const routes = (dial.data?.default_routes ?? []).filter(item => item.family === version)
  const wan = owned?.interface ?? route?.dev
  const wan6 = interfaces.data?.interfaces
    .find(row => row.name === wan)?.prefixes?.find(prefix => prefix.includes(':'))

  let health: Health
  let detail: ReactNode
  let to = '/gateway/interfaces'

  if (uplink.isError || dial.isError) {
    health = { label: t('Unavailable'), tone: 'caution' }
    detail = t('Route readings unavailable')
  } else if (uplink.isPending || dial.isPending) {
    health = { label: t('Reading…'), tone: 'quiet' }
    detail = <Skeleton className="h-4 w-32" />
  } else if (family === 'v4') {
    const address = owned?.address ?? route?.addresses?.[0]
    const hop = owned?.gateway ?? route?.via
    const silent = (owned?.gateway_state ?? route?.gateway_state ?? routes[0]?.gateway_state) === 'silent'
    health = silent ? { label: t('Not answering'), tone: 'caution' }
      : route || routes.length ? { label: t('Connected'), tone: 'good' }
        : { label: t('No route'), tone: 'caution' }
    detail = address && hop && wan ? `${address} · ${hop} via ${wan}`
      : address ? address
        : hop && wan ? `${hop} via ${wan}`
          : wan ?? t('No internet route')
  } else if (owned?.delegation) {
    const prefix = owned.delegation.prefix
    health = prefix ? { label: t('Delegated'), tone: 'good' } : { label: t('Asking…'), tone: 'quiet' }
    detail = prefix ?? owned.delegation.error ?? t('Asking the ISP for a prefix')
  } else if (owned?.tunnel) {
    const tunnel = owned.tunnel
    const carrying = tunnel.present && tunnel.up && tunnel.route_dev === tunnel.interface
    health = carrying ? { label: t('Tunnel'), tone: 'good' } : { label: t('Not in use'), tone: 'caution' }
    detail = `6in4 · ${tunnel.address}`
    to = '/access/tunnelbroker'
  } else if (routes.length) {
    const via = routes[0].via
    health = routes[0].gateway_state === 'silent'
      ? { label: t('Not answering'), tone: 'caution' }
      : { label: t('Connected'), tone: 'good' }
    detail = wan6 ?? (via ? `${via} via ${routes[0].dev}` : `${routes[0].dev}`)
  } else {
    health = { label: t('No IPv6'), tone: 'quiet' }
    detail = t('No IPv6 default route')
  }

  return <StageCard id={id} title={t('Upstream')} icon={ArrowUpFromLine} to={to} health={health} detail={detail} />
}

function LanStage({ family, id }: StageProps) {
  useLocale()
  const interfaces = useInterfaces()
  const leases = useDhcpLeases()
  const networks = interfaces.data?.networks ?? []
  const v4 = networks.filter(network => network.subnet)
  const v6 = networks.filter(network => network.subnet6)
  const waiting = networks.filter(network => !network.subnet6 && network.delegated !== undefined)
  const primary = (family === 'v4' ? v4 : v6)[0]
  const to = primary?.members[0]
    ? `/gateway/interfaces/${encodeURIComponent(primary.members[0])}`
    : '/gateway/interfaces'

  let health: Health
  let detail: ReactNode

  if (interfaces.isError) {
    health = { label: t('Unavailable'), tone: 'caution' }
    detail = t('Network settings unavailable')
  } else if (interfaces.isPending) {
    health = { label: t('Reading…'), tone: 'quiet' }
    detail = <Skeleton className="h-4 w-32" />
  } else if (family === 'v4') {
    if (!primary) {
      health = { label: t('No network'), tone: 'caution' }
      detail = t('No IPv4 network is configured')
    } else {
      const used = leases.data?.usage?.find(entry => entry.network === primary.name)
      health = { label: t('Configured'), tone: 'good' }
      detail = `${primary.subnet}${v4.length > 1 ? ` +${v4.length - 1}` : ''}${used
        ? ` · ${t('{active} of {size} in use', { active: used.active, size: used.size })}`
        : ''}`
    }
  } else if (primary) {
    health = { label: t('Configured'), tone: 'good' }
    detail = `${primary.subnet6}${v6.length > 1 ? ` +${v6.length - 1}` : ''}`
  } else if (waiting.length > 0) {
    health = { label: t('Not configured'), tone: 'quiet' }
    detail = t('Delegated networks have no /64 yet')
  } else {
    health = { label: t('No IPv6'), tone: 'quiet' }
    detail = t('No network has an IPv6 subnet')
  }

  return <StageCard id={id} title={t('LAN')} icon={Network} to={to} health={health} detail={detail} />
}

function ClientsStage({ family, id }: StageProps) {
  useLocale()
  const dhcp = useDhcpStatus()
  const dhcpConfig = useDhcpConfig()
  const leases = useDhcpLeases()
  const interfaces = useInterfaces()
  const networks = interfaces.data?.networks ?? []

  let health: Health
  let detail: ReactNode

  if (family === 'v4') {
    health = dhcpHealth(dhcp)
    if (leases.isError) {
      detail = t('Leases unavailable')
    } else if (leases.isPending) {
      detail = <Skeleton className="h-4 w-24" />
    } else {
      const usage = leases.data?.usage ?? []
      const active = usage.reduce((sum, entry) => sum + entry.active, 0)
      const size = usage.reduce((sum, entry) => sum + entry.size, 0)
      detail = size > 0
        ? t('{active} of {size} in use', { active, size })
        : t('No address ranges')
    }
    return <StageCard id={id} title={t('Clients')} icon={MonitorSmartphone} to="/gateway/dhcp/details" health={health} detail={detail} />
  }

  // IPv6 clients are configured by router advertisement, not by a lease: the
  // mode and MTU live on the pool, the prefix comes from the network.
  const pools = (dhcpConfig.data?.pools ?? []).filter(pool => (pool.ipv6?.mode ?? 'off') !== 'off')
  const total = networks.length || pools.length

  if (dhcpConfig.isError) {
    health = { label: t('Unavailable'), tone: 'caution' }
    detail = t('Advertisement settings unavailable')
  } else if (dhcpConfig.isPending) {
    health = { label: t('Reading…'), tone: 'quiet' }
    detail = <Skeleton className="h-4 w-32" />
  } else if (dhcp.data && !dhcp.data.enabled) {
    health = { label: t('Off'), tone: 'quiet' }
    detail = t('Nothing is advertised while DHCP is off')
  } else if (pools.length === 0) {
    const lan6 = networks.some(network => network.subnet6)
    health = lan6 ? { label: t('Not configured'), tone: 'caution' } : { label: t('No IPv6'), tone: 'quiet' }
    detail = lan6 ? t('No network advertises IPv6') : t('No network has an IPv6 subnet')
  } else {
    const modes = [...new Set(pools.map(pool => pool.ipv6?.mode))].map(mode => advertisementMode(mode))
    const mtu = pools.find(pool => pool.ipv6?.mtu)?.ipv6?.mtu
    health = { label: t('Advertising'), tone: 'good' }
    detail = `${modes.join(', ')} · ${t('{advertising} of {total} networks', { advertising: pools.length, total })}${mtu ? ` · MTU ${mtu}` : ''}`
  }

  return <StageCard id={id} title={t('Clients')} icon={MonitorSmartphone} to="/gateway/dhcp/ranges" health={health} detail={detail} />
}

function ForwardingStage({ family, id }: StageProps) {
  useLocale()
  const gateway = useGatewayConfig()

  let health: Health
  let detail: ReactNode

  if (gateway.isError) {
    health = { label: t('Unavailable'), tone: 'caution' }
    detail = t('Gateway settings unavailable')
  } else if (gateway.isPending) {
    health = { label: t('Reading…'), tone: 'quiet' }
    detail = <Skeleton className="h-4 w-24" />
  } else if (family === 'v4') {
    health = gateway.data.enabled ? { label: t('On'), tone: 'good' } : { label: t('Off'), tone: 'caution' }
    detail = gateway.data.enabled
      ? t('IPv4 forwarding is written')
      : t('No device behind this router reaches the internet')
  } else if (!gateway.data.enabled) {
    health = { label: t('Off'), tone: 'caution' }
    detail = t('Nothing is applied while gateway routing is off')
  } else if (gateway.data.ipv6_forwarding === true) {
    health = { label: t('On'), tone: 'good' }
    detail = t('IPv6 forwarding is written')
  } else if (gateway.data.ipv6_forwarding === false) {
    health = { label: t('Off'), tone: 'caution' }
    detail = t('IPv6 stops at this router')
  } else {
    health = { label: t('Not managed'), tone: 'quiet' }
    detail = t('This machine’s own setting is left alone')
  }

  return <StageCard id={id} title={t('Forwarding')} icon={ArrowRightLeft} to="/gateway/interfaces#forwarding" health={health} detail={detail} />
}

function ExitStage({ family, id }: StageProps) {
  useLocale()
  const gateway = useGatewayConfig()
  const config = gateway.data
  const exits = config?.exits ?? []
  const chosen = config?.default ? exits.find(exit => exit.name === config.default) : undefined
  const overrides = (config?.interfaces?.length ?? 0) + (config?.devices?.length ?? 0)
  const nat = config?.snat ? t('NAT on') : t('NAT off')
  const overrideNote = overrides > 0 ? ` · ${t('{count} overrides', { count: overrides })}` : ''

  let health: Health
  let detail: ReactNode
  let to = '/gateway/interfaces#forwarding'

  if (gateway.isError) {
    health = { label: t('Unavailable'), tone: 'caution' }
    detail = t('Gateway settings unavailable')
  } else if (gateway.isPending || !config) {
    health = { label: t('Reading…'), tone: 'quiet' }
    detail = <Skeleton className="h-4 w-28" />
  } else if (family === 'v4') {
    if (config.default && !chosen) {
      health = { label: t('Unknown exit'), tone: 'caution' }
      detail = config.default
    } else if (chosen) {
      health = { label: t('Via exit'), tone: 'good' }
      detail = `${chosen.name} · ${nat}${overrideNote}`
    } else {
      health = { label: t('Direct'), tone: 'quiet' }
      detail = `${t('This router’s own connection')} · ${nat}${overrideNote}`
    }
  } else {
    to = '/access/gateway'
    // IPv6 has no NAT, so the only question an exit answers for it is whether
    // it carries the traffic or drops it. `direct` is the leak: v6 leaves the
    // ordinary way while v4 goes through the exit.
    if (!exits.length) {
      health = { label: t('Direct'), tone: 'quiet' }
      detail = t('No NAT · IPv6 is not translated')
    } else if (!chosen) {
      health = { label: t('Direct'), tone: 'quiet' }
      detail = `${t('No default exit')} · ${exits.length} ${t('exits')}`
    } else if (chosen.ipv6 === 'via') {
      health = { label: t('Via exit'), tone: 'good' }
      detail = t('{name} carries IPv6', { name: chosen.name })
    } else if (chosen.ipv6 === 'direct') {
      health = { label: t('Direct'), tone: 'caution' }
      detail = t('{name} sends IPv6 around it', { name: chosen.name })
    } else {
      health = { label: t('Blocked'), tone: 'caution' }
      detail = t('{name} blocks IPv6', { name: chosen.name })
    }
  }

  return <StageCard id={id} title={t('Exit')} icon={Signpost} to={to} health={health} detail={detail} />
}

function DnsStage({ family, id }: StageProps) {
  useLocale()
  const dns = useDnsStatus()
  const dnsConfig = useDnsConfig()
  const dial = useDialStatus()

  let health: Health
  let detail: ReactNode
  let to = '/gateway/dns/details'

  if (family === 'v4') {
    health = dnsHealth(dns)
    if (dial.isError) detail = t('Resolvers unavailable')
    else if (dial.isPending) detail = <Skeleton className="h-4 w-28" />
    else detail = dial.data.resolvers?.length ? dial.data.resolvers.join(', ') : t('No resolvers')
  } else {
    to = '/gateway/dns/resolving'
    if (dnsConfig.isError) {
      health = { label: t('Unavailable'), tone: 'caution' }
      detail = t('DNS settings unavailable')
    } else if (dnsConfig.isPending) {
      health = { label: t('Reading…'), tone: 'quiet' }
      detail = <Skeleton className="h-4 w-28" />
    } else if (dnsConfig.data.filter_aaaa) {
      health = { label: t('AAAA filtered'), tone: 'caution' }
      detail = t('IPv6 lookups return nothing')
    } else {
      health = { label: t('AAAA answered'), tone: 'good' }
      detail = t('Clients can resolve IPv6')
    }
  }

  return <StageCard id={id} title={t('DNS')} icon={Search} to={to} health={health} detail={detail} />
}

/* -------------------------------------------------------------------------- */

function StageCard({ id, title, icon: Icon, to, health, detail }: {
  id?: string
  title: string
  icon: LucideIcon
  to: string
  health: Health
  detail: ReactNode
}) {
  return (
    <Link
      id={id}
      to={to}
      className={cn(
        'flex min-w-0 gap-3 rounded-xl border bg-card p-4 transition-colors duration-150 hover:bg-accent/60 active:bg-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring',
        id && 'scroll-mt-20',
      )}
    >
      <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
        <Icon className="size-4.5" strokeWidth={1.75} aria-hidden />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-sm font-semibold">{title}</span>
          <ChevronRight className="ml-auto size-4 shrink-0 text-muted-foreground/60" aria-hidden />
        </div>
        <span className={cn('mt-0.5 flex items-center gap-1.5 text-[0.8125rem] font-medium', tone[health.tone])}>
          <span className={cn('size-1.5 shrink-0 rounded-full', dot[health.tone])} aria-hidden />
          {health.label}
        </span>
        <div className="mt-1 text-[0.8125rem] leading-5 text-muted-foreground [overflow-wrap:anywhere]">{detail}</div>
      </div>
    </Link>
  )
}

/* -------------------------------------------------------------------------- */

function dhcpHealth(dhcp: ReturnType<typeof useDhcpStatus>): Health {
  if (dhcp.isError) return { label: t('Unavailable'), tone: 'caution' }
  if (!dhcp.data) return { label: t('Reading…'), tone: 'quiet' }
  if (!dhcp.data.enabled) return { label: t('Off'), tone: 'quiet' }
  if (dhcp.data.service_error) return { label: t('Unavailable'), tone: 'caution' }
  if (dhcp.data.drift_error || dhcp.data.blockers?.length) return { label: t('Needs attention'), tone: 'caution' }
  if (!dhcp.data.service?.active) return { label: t('Not running'), tone: 'caution' }
  return dhcp.data.drifted ? { label: t('Needs attention'), tone: 'caution' } : { label: t('Running'), tone: 'good' }
}

function dnsHealth(dns: ReturnType<typeof useDnsStatus>): Health {
  if (dns.isError) return { label: t('Unavailable'), tone: 'caution' }
  if (!dns.data) return { label: t('Reading…'), tone: 'quiet' }
  if (!dns.data.enabled) return { label: t('Off'), tone: 'quiet' }
  if (dns.data.services.some(service => service.error)) return { label: t('Unavailable'), tone: 'caution' }
  if (dns.data.drift_error || dns.data.blockers?.length) return { label: t('Needs attention'), tone: 'caution' }
  const running = dns.data.services.length > 0 && dns.data.services.every(service => service.status?.active)
  if (!running) return { label: t('Not running'), tone: 'caution' }
  return dns.data.drifted ? { label: t('Needs attention'), tone: 'caution' } : { label: t('Running'), tone: 'good' }
}

function advertisementMode(mode?: RouterAdvertisementMode) {
  return mode === 'stateful' ? t('DHCPv6 stateful') : t('SLAAC')
}
