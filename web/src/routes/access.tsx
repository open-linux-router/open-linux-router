import { ArrowDownLeft, ArrowUpRight, LockKeyhole, Route, Server, Waypoints } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'

import connections from '@/assets/access-connections.webp'
import { groupOf } from '@/components/layout/sections'
import { List, ListRow } from '@/components/ui/list'
import { forwardsSummary, gatewaySummary, ingressSummary, remoteSummary, socksOutSummary } from '@/features/access/summary'
import type { Summary } from '@/features/access/summary'
import { useGatewayConfig, useGatewayStatus } from '@/features/gateway/queries'
import { useIngressConfig, useIngressStatus } from '@/features/ingress/queries'
import { useForwardsStatus } from '@/features/nat/queries'
import { useRemoteConfig, useRemoteStatus, useShadowsocksStatus, useSocksStatus } from '@/features/remote/queries'
import { useSocksOutConfig, useSocksOutStatus } from '@/features/socksout/queries'
import { cn } from '@/lib/utils'

export function AccessPage() {
  const gateway = useGatewayConfig()
  const gatewayStatus = useGatewayStatus()
  const socksOut = useSocksOutConfig()
  const socksOutStatus = useSocksOutStatus()
  const remote = useRemoteConfig()
  const wireguard = useRemoteStatus()
  const shadowsocks = useShadowsocksStatus()
  const socks = useSocksStatus()
  const ingress = useIngressConfig()
  const ingressStatus = useIngressStatus()
  const forwards = useForwardsStatus()

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-4 px-1">
        <div className="space-y-1">
          <h2 className="text-lg font-semibold tracking-tight">Connections, in both directions</h2>
          <p className="max-w-prose text-sm leading-relaxed text-muted-foreground">
            Reach another network from home, connect back to yours, or publish a service.
          </p>
        </div>
        <img src={connections} alt="" width={96} height={96} decoding="async" className="size-16 shrink-0 object-contain sm:size-24" />
      </div>

      <section className="space-y-2" aria-labelledby="access-from-home">
        <h2 id="access-from-home" className="flex items-center gap-2 px-1 text-sm font-medium text-muted-foreground">
          <ArrowUpRight className="size-4" aria-hidden /> From home
        </h2>
        <List>
          <AccessRow
            title="Another gateway or VPN"
            description="Route selected devices through another gateway or an existing VPN tunnel."
            icon={Route}
            to="/gateway/exits"
            summary={gatewaySummary(gateway, gatewayStatus)}
          />
          <AccessRow
            slug="socks5"
            description="Connect home devices through an external SOCKS5 proxy."
            icon={Waypoints}
            summary={socksOutSummary(socksOut, socksOutStatus)}
          />
        </List>
        <p className="px-1 text-xs leading-relaxed text-muted-foreground">
          Routing is managed in Gateway → Ways out. VPN tunnels must already be set up outside OLR.
        </p>
      </section>

      <section className="space-y-2" aria-labelledby="access-from-outside">
        <h2 id="access-from-outside" className="flex items-center gap-2 px-1 text-sm font-medium text-muted-foreground">
          <ArrowDownLeft className="size-4" aria-hidden /> From outside
        </h2>
        <List>
          <AccessRow
            slug="remote"
            description="Connect to home with WireGuard, or use its internet connection through a proxy."
            icon={LockKeyhole}
            summary={remoteSummary(remote, wireguard, shadowsocks, socks)}
          />
          <AccessRow
            slug="ingress"
            description="Make selected web services reachable by name over HTTPS."
            icon={Server}
            summary={ingressSummary(ingress, ingressStatus)}
          />
          <AccessRow
            slug="forwards"
            description="Let an incoming connection reach a device directly by port."
            icon={Route}
            summary={forwardsSummary(gateway, forwards)}
          />
        </List>
      </section>
    </div>
  )
}

function AccessRow({ slug, title, description, icon: Icon, to, summary }: {
  slug?: string
  title?: string
  description: string
  icon: LucideIcon
  to?: string
  summary: Summary
}) {
  const label = slug ? groupOf('/access', slug).group.label : title
  const state = <span className={cn('block max-w-sm whitespace-normal', summary.attention && 'text-warning-foreground')}>{summary.text}</span>

  return (
    <ListRow
      title={label}
      subtitle={<><span>{description}</span><span className="mt-1 block sm:hidden">{state}</span></>}
      leading={<Icon className="size-4 shrink-0 text-muted-foreground" aria-hidden />}
      trailing={state}
      to={to ?? `/access/${slug}`}
      wrap
    />
  )
}
