import { ArrowDownLeft, ArrowUpRight, LockKeyhole, Route, Server, Waypoints } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'

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
      <section className="space-y-2" aria-labelledby="access-from-home">
        <h2 id="access-from-home" className="flex items-center gap-2 px-1 text-sm font-medium text-muted-foreground">
          <ArrowUpRight className="size-4" aria-hidden /> From home
        </h2>
        <List>
          <AccessRow
            title="Another gateway or VPN"
            description="Existing gateway or VPN interface"
            icon={Route}
            to="/gateway/exits"
            summary={gatewaySummary(gateway, gatewayStatus)}
          />
          <AccessRow
            slug="socks5"
            icon={Waypoints}
            summary={socksOutSummary(socksOut, socksOutStatus)}
          />
        </List>
      </section>

      <section className="space-y-2" aria-labelledby="access-from-outside">
        <h2 id="access-from-outside" className="flex items-center gap-2 px-1 text-sm font-medium text-muted-foreground">
          <ArrowDownLeft className="size-4" aria-hidden /> From outside
        </h2>
        <List>
          <AccessRow
            slug="remote"
            description="WireGuard, Shadowsocks and SOCKS5"
            icon={LockKeyhole}
            summary={remoteSummary(remote, wireguard, shadowsocks, socks)}
          />
          <AccessRow
            slug="ingress"
            description="HTTPS by domain name"
            icon={Server}
            summary={ingressSummary(ingress, ingressStatus)}
          />
          <AccessRow
            slug="forwards"
            description="Direct access by port"
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
  description?: string
  icon: LucideIcon
  to?: string
  summary: Summary
}) {
  const label = slug ? groupOf('/access', slug).group.label : title
  const state = <span className={cn('block max-w-sm whitespace-normal', summary.attention && 'text-warning-foreground')}>{summary.text}</span>

  return (
    <ListRow
      title={label}
      subtitle={description}
      leading={<Icon className="size-4 shrink-0 text-muted-foreground" aria-hidden />}
      trailing={state}
      to={to ?? `/access/${slug}`}
      wrap
    />
  )
}
