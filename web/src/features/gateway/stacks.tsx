import { t, useLocale } from '@/lib/i18n'
import {
  ArrowUpFromLine,
  MonitorSmartphone,
  Network,
  Route,
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
import { RowChevron } from '@/components/ui/row-action'
import { cn } from '@/lib/utils'
import { state, type Health } from '@/lib/state'
import { StatusInline } from '@/components/ui/status'

type Family = 'v4' | 'v6'

/**
 * The gateway as two stacks, one per family, each cut across the modules.
 *
 * A normal operator asks "does IPv6 work, and where do I fix it", not "is dial
 * or dhcp configured". Each stack is that question answered in the order the
 * traffic takes: upstream, LAN addressing, DHCP, routing, which way out, and
 * name resolution. Every card names the one page that changes it.
 *
 * The clients stage is titled for what an operator came to change: DHCP on
 * IPv4 — the module's own name and the word they arrive with (design.md §4.2) —
 * and the protocol actually running on IPv6, SLAAC or DHCPv6. Those are the
 * terms network operators already use; a stage label of our own would be one
 * more thing to learn before the card could be used.
 *
 * The routing stage carries the module's own name for the same reason. The
 * switch at interfaces#forwarding is titled "Gateway routing", and it is what
 * this card reports: turning it off takes every network off the internet and
 * closes every port forward. Calling the card Forwarding named the kernel
 * mechanism instead — the one part of it the operator never touches — and left
 * the page with no card for the switch that decides whether the LAN gets out at
 * all.
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
  { key: 'dhcp', hash: 'dhcp', Stage: DhcpStage },
  { key: 'routing', Stage: RoutingStage },
  { key: 'exit', Stage: ExitStage },
  { key: 'dns', hash: 'dns', Stage: DnsStage },
]

function Stack({ id, title, family }: { id: string; title: string; family: Family }) {
  return (
    <section className="space-y-4" aria-labelledby={`stack-${id}`}>
      <h2 id={`stack-${id}`} className="text-2xl leading-8 font-semibold">{title}</h2>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {STAGES.map(({ key, hash, Stage }) => (
          // The hash targets the v4 stack only. `/gateway#dhcp` and `#dns` used
          // to be the redirects from the settings trees; the trees now open
          // `/gateway/dhcp` and `/gateway/dns` themselves, but the anchors stay
          // for links made while they pointed here.
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
    health = state('Unavailable')
    detail = t('Route readings unavailable')
  } else if (uplink.isPending || dial.isPending) {
    health = state('Reading…')
    detail = <Skeleton className="h-4 w-32" />
  } else if (family === 'v4') {
    const address = owned?.address ?? route?.addresses?.[0]
    const hop = owned?.gateway ?? route?.via
    const silent = (owned?.gateway_state ?? route?.gateway_state ?? routes[0]?.gateway_state) === 'silent'
    health = silent ? state('Not answering')
      : route || routes.length ? state('Connected')
        : state('No route')
    detail = address && hop && wan ? `${address} · ${hop} via ${wan}`
      : address ? address
        : hop && wan ? `${hop} via ${wan}`
          : wan ?? t('No internet route')
  } else if (owned?.delegation) {
    const prefix = owned.delegation.prefix
    health = prefix ? state('Delegated') : state('Asking…')
    detail = prefix ?? owned.delegation.error ?? t('Asking the ISP for a prefix')
  } else if (owned?.tunnel) {
    const tunnel = owned.tunnel
    const carrying = tunnel.present && tunnel.up && tunnel.route_dev === tunnel.interface
    health = carrying ? state('Tunnel') : state('Not in use')
    detail = `6in4 · ${tunnel.address}`
    to = '/access/tunnelbroker'
  } else if (routes.length) {
    const via = routes[0].via
    health = routes[0].gateway_state === 'silent'
      ? state('Not answering')
      : state('Connected')
    detail = wan6 ?? (via ? `${via} via ${routes[0].dev}` : `${routes[0].dev}`)
  } else {
    health = state('No IPv6')
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
    health = state('Unavailable')
    detail = t('Network settings unavailable')
  } else if (interfaces.isPending) {
    health = state('Reading…')
    detail = <Skeleton className="h-4 w-32" />
  } else if (family === 'v4') {
    if (!primary) {
      health = state('No network')
      detail = t('No IPv4 network is configured')
    } else {
      const used = leases.data?.usage?.find(entry => entry.network === primary.name)
      health = state('Configured')
      detail = `${primary.subnet}${v4.length > 1 ? ` +${v4.length - 1}` : ''}${used
        ? ` · ${t('{active} of {size} in use', { active: used.active, size: used.size })}`
        : ''}`
    }
  } else if (primary) {
    health = state('Configured')
    detail = `${primary.subnet6}${v6.length > 1 ? ` +${v6.length - 1}` : ''}`
  } else if (waiting.length > 0) {
    health = state('Waiting')
    detail = t('Delegated networks have no /64 yet')
  } else {
    health = state('No IPv6')
    detail = t('No network has an IPv6 subnet')
  }

  return <StageCard id={id} title={t('LAN')} icon={Network} to={to} health={health} detail={detail} />
}

function DhcpStage({ family, id }: StageProps) {
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
    return <StageCard id={id} title={t('DHCP')} icon={MonitorSmartphone} to="/gateway/dhcp" health={health} detail={detail} />
  }

  // IPv6 clients are configured by router advertisement, not by a lease: the
  // mode and MTU live on the pool, the prefix comes from the network. The card
  // is named for the protocol that is running — SLAAC or DHCPv6 — because that
  // is what an operator knows; "clients" or a coined pair name would only send
  // them to the documentation to find out what it meant.
  const pools = (dhcpConfig.data?.pools ?? []).filter(pool => (pool.ipv6?.mode ?? 'off') !== 'off')
  const total = networks.length || pools.length
  const modes = new Set(pools.map(pool => pool.ipv6?.mode))
  const title = modes.has('stateful')
    ? (modes.has('slaac') ? t('SLAAC, DHCPv6') : t('DHCPv6'))
    : t('SLAAC')

  if (dhcpConfig.isError) {
    health = state('Unavailable')
    detail = t('Advertisement settings unavailable')
  } else if (dhcpConfig.isPending) {
    health = state('Reading…')
    detail = <Skeleton className="h-4 w-32" />
  } else if (dhcp.data && !dhcp.data.enabled) {
    health = state('Off')
    detail = t('Nothing is advertised while DHCP is off')
  } else if (pools.length === 0) {
    const lan6 = networks.some(network => network.subnet6)
    health = lan6 ? state('Not configured') : state('No IPv6')
    detail = lan6 ? t('No network advertises IPv6') : t('No network has an IPv6 subnet')
  } else {
    const mtu = pools.find(pool => pool.ipv6?.mtu)?.ipv6?.mtu
    health = state('Advertising')
    detail = `${t('{advertising} of {total} networks', { advertising: pools.length, total })}${mtu ? ` · MTU ${mtu}` : ''}`
  }

  return <StageCard id={id} title={title} icon={MonitorSmartphone} to="/gateway/dhcp/ranges" health={health} detail={detail} />
}

function RoutingStage({ family, id }: StageProps) {
  useLocale()
  const gateway = useGatewayConfig()
  const config = gateway.data
  const forwards = config?.forwards?.length ?? 0

  let health: Health
  let detail: ReactNode

  if (gateway.isError) {
    health = state('Unavailable')
    detail = t('Gateway settings unavailable')
  } else if (gateway.isPending || !config) {
    health = state('Reading…')
    detail = <Skeleton className="h-4 w-24" />
  } else if (family === 'v4') {
    health = config.enabled ? state('On') : state('LAN offline')
    // The routing switch is this card's subject: it is the one control that
    // takes every network off the internet and closes every port forward. The
    // detail says what that means for the network and what the routing carries
    // — NAT, port forwards — which are the facts an operator checks here, and
    // neither is anywhere else on this page.
    detail = config.enabled
      ? [
        t('Devices behind this router reach the internet'),
        config.snat ? t('NAT on') : t('NAT off'),
        forwards > 0 ? t('{count} port forwards', { count: forwards }) : null,
      ].filter(Boolean).join(' · ')
      : t('No device behind this router reaches the internet')
  } else if (!config.enabled) {
    health = state('Off')
    detail = t('IPv6 cannot leave this router while routing is off')
  } else if (config.ipv6_forwarding === true) {
    health = state('On')
    detail = t('IPv6 from behind this router goes out')
  } else if (config.ipv6_forwarding === false) {
    health = state('Off')
    detail = t('IPv6 stops at this router')
  } else {
    // Unmanaged forwarding is not an answer about traffic at all, so the card
    // says who decides rather than borrowing a state it cannot observe.
    health = state('Not managed')
    detail = t('The system decides whether IPv6 is forwarded')
  }

  return <StageCard id={id} title={t('Routing')} icon={Route} to="/gateway/interfaces#forwarding" health={health} detail={detail} />
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
    health = state('Unavailable')
    detail = t('Gateway settings unavailable')
  } else if (gateway.isPending || !config) {
    health = state('Reading…')
    detail = <Skeleton className="h-4 w-28" />
  } else if (family === 'v4') {
    if (config.default && !chosen) {
      health = state('Unknown exit')
      detail = config.default
    } else if (chosen) {
      health = state('Via exit')
      detail = `${chosen.name} · ${nat}${overrideNote}`
    } else {
      health = state('Direct')
      detail = `${t('This router’s own connection')} · ${nat}${overrideNote}`
    }
  } else {
    to = '/access/gateway'
    // IPv6 has no NAT, so the only question an exit answers for it is whether
    // it carries the traffic or drops it. `direct` is the leak: v6 leaves the
    // ordinary way while v4 goes through the exit.
    if (!exits.length) {
      health = state('Direct')
      detail = t('No NAT · IPv6 is not translated')
    } else if (!chosen) {
      health = state('Direct')
      detail = `${t('No default exit')} · ${exits.length} ${t('exits')}`
    } else if (chosen.ipv6 === 'via') {
      health = state('Via exit')
      detail = t('{name} carries IPv6', { name: chosen.name })
    } else if (chosen.ipv6 === 'direct') {
      health = state('Bypasses the exit')
      detail = t('{name} sends IPv6 around it', { name: chosen.name })
    } else {
      health = state('Blocked')
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
  const to = '/gateway/dns'

  if (family === 'v4') {
    health = dnsHealth(dns)
    if (dial.isError) detail = t('Resolvers unavailable')
    else if (dial.isPending) detail = <Skeleton className="h-4 w-28" />
    else detail = dial.data.resolvers?.length ? dial.data.resolvers.join(', ') : t('No resolvers')
  } else {
    // The AAAA switch leads the DNS page, so the IPv6 card opens the section
    // rather than the upstream page it used to point at: the question is DNS's
    // answer for this family, not which server gives it.
    //
    // The label states the intent — IPv6 name resolution is enabled or not —
    // rather than the mechanism that carries it out. `filter_aaaa` is one way
    // to turn it off, and a card that reports "AAAA filtered" asks the
    // operator to know the record type and to translate the mechanism back
    // into the thing they set before they can answer "is IPv6 off?". The
    // config has one switch, so the card says what that switch means.
    if (dnsConfig.isError) {
      health = state('Unavailable')
      detail = t('DNS settings unavailable')
    } else if (dnsConfig.isPending) {
      health = state('Reading…')
      detail = <Skeleton className="h-4 w-28" />
    } else if (dnsConfig.data.filter_aaaa) {
      health = state('IPv6 disabled')
      detail = t('IPv6 lookups return nothing')
    } else {
      health = state('IPv6 enabled')
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
          <RowChevron className="ml-auto" />
        </div>
        <StatusInline tone={health.tone} className="mt-0.5">{health.label}</StatusInline>
        <div className="mt-1 text-[0.8125rem] leading-5 text-muted-foreground [overflow-wrap:anywhere]">{detail}</div>
      </div>
    </Link>
  )
}

/* -------------------------------------------------------------------------- */

function dhcpHealth(dhcp: ReturnType<typeof useDhcpStatus>): Health {
  if (dhcp.isError) return state('Unavailable')
  if (!dhcp.data) return state('Reading…')
  if (!dhcp.data.enabled) return state('Off')
  if (dhcp.data.service_error) return state('Unavailable')
  if (dhcp.data.drift_error || dhcp.data.blockers?.length) return state('Needs attention')
  if (!dhcp.data.service?.active) return state('Not running')
  return dhcp.data.drifted ? state('Needs attention') : state('Running')
}

function dnsHealth(dns: ReturnType<typeof useDnsStatus>): Health {
  if (dns.isError) return state('Unavailable')
  if (!dns.data) return state('Reading…')
  if (!dns.data.enabled) return state('Off')
  if (dns.data.services.some(service => service.error)) return state('Unavailable')
  if (dns.data.drift_error || dns.data.blockers?.length) return state('Needs attention')
  const running = dns.data.services.length > 0 && dns.data.services.every(service => service.status?.active)
  if (!running) return state('Not running')
  return dns.data.drifted ? state('Needs attention') : state('Running')
}
