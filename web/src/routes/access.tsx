import { ArrowDownLeft, ArrowUpRight, ChevronRight, Globe, LockKeyhole, Route, Server, Shield, Waypoints } from 'lucide-react'
import { Link } from 'react-router'

import { ACCESS_DEFINITIONS } from '@/features/access/catalog'
import type { AccessDirection, AccessKind } from '@/features/access/catalog'
import { forwardsSummary, gatewaySummary, ingressSummary, remoteSummary, socksOutSummary, systemSummary, tunnelBrokerSummary } from '@/features/access/summary'
import type { Summary } from '@/features/access/summary'
import { useDialConfig, useDialStatus, useTunnelBrokerStatus } from '@/features/dial/queries'
import { useGatewayConfig, useGatewayStatus } from '@/features/gateway/queries'
import { useIngressConfig, useIngressStatus } from '@/features/ingress/queries'
import { useForwardsStatus } from '@/features/nat/queries'
import { useRemoteConfig, useRemoteStatus, useShadowsocksStatus, useSocksStatus } from '@/features/remote/queries'
import { useSocksOutConfig, useSocksOutStatus } from '@/features/socksout/queries'
import { cn } from '@/lib/utils'

const ICONS = { gateway: Route, 'socks-out': Waypoints, tunnelbroker: Globe, wireguard: Shield, shadowsocks: LockKeyhole, 'socks-in': Waypoints, https: Server, 'port-forward': Route }

export function AccessPage() {
  const gateway = useGatewayConfig()
  const gatewayStatus = useGatewayStatus()
  const socksOut = useSocksOutConfig()
  const socksOutStatus = useSocksOutStatus()
  const dial = useDialConfig()
  const dialStatus = useDialStatus()
  const broker = useTunnelBrokerStatus()
  const remote = useRemoteConfig()
  const wireguard = useRemoteStatus()
  const shadowsocks = useShadowsocksStatus()
  const socks = useSocksStatus()
  const ingress = useIngressConfig()
  const ingressStatus = useIngressStatus()
  const forwards = useForwardsStatus()
  const summaries: Record<AccessKind, Summary> = {
    gateway: gatewaySummary(gateway, gatewayStatus),
    'socks-out': socksOutSummary(socksOut, socksOutStatus),
    tunnelbroker: tunnelBrokerSummary(dial, broker),
    wireguard: remoteSummary(remote, wireguard, shadowsocks, socks, 'wireguard'),
    shadowsocks: remoteSummary(remote, wireguard, shadowsocks, socks, 'shadowsocks'),
    'socks-in': remoteSummary(remote, wireguard, shadowsocks, socks, 'socks'),
    https: ingressSummary(ingress, ingressStatus),
    'port-forward': forwardsSummary(gateway, forwards),
  }
  const system = systemSummary(dialStatus)
  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-center justify-between gap-2 rounded-xl border bg-card p-4 text-sm">
        <Link to="/gateway/interfaces" className="font-medium underline-offset-4 hover:underline focus-visible:outline-ring">System default</Link>
        <span className={cn('min-w-0 break-words text-muted-foreground', system.attention && 'text-warning-foreground')}>{system.text}</span>
      </div>
      <AccessTiles direction="out" summaries={summaries} />
      <AccessTiles direction="in" summaries={summaries} />
    </div>
  )
}

function AccessTiles({ direction, summaries }: { direction: AccessDirection; summaries: Record<AccessKind, Summary> }) {
  const DirectionIcon = direction === 'out' ? ArrowUpRight : ArrowDownLeft
  return (
    <section className="space-y-4" aria-labelledby={`access-${direction}`}>
      <h2 id={`access-${direction}`} className="flex items-center gap-2 text-base font-semibold">
        <DirectionIcon className="size-4 text-muted-foreground" aria-hidden /> Add a way {direction}
      </h2>
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {ACCESS_DEFINITIONS.filter(entry => entry.direction === direction).map(entry => {
          const Icon = ICONS[entry.kind]
          const summary = summaries[entry.kind]
          return (
            <Link key={entry.kind} to={entry.to} className="group flex min-w-0 items-center gap-4 rounded-xl border bg-card p-4 text-card-foreground shadow-xs transition-colors duration-150 hover:border-primary/50 hover:bg-accent/30 active:bg-accent/50 focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring sm:p-5">
              <span className="flex size-12 shrink-0 items-center justify-center rounded-xl bg-accent text-primary sm:size-14"><Icon className="size-6 sm:size-7" strokeWidth={1.75} aria-hidden /></span>
              <div className="min-w-0 flex-1 space-y-1">
                <h3 className="text-lg leading-6 font-semibold">{entry.label}</h3>
                <p className="text-sm text-muted-foreground">{entry.description}</p>
                <p className={cn('text-sm text-muted-foreground', summary.attention && 'text-warning-foreground')}>{summary.text}</p>
              </div>
              <ChevronRight className="size-4 shrink-0 text-muted-foreground group-hover:text-primary" aria-hidden />
            </Link>
          )
        })}
      </div>
    </section>
  )
}
