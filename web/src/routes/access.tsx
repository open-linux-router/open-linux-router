import { ArrowLeftRight, Cable, ExternalLink, Globe, LockKeyhole, Network, Route, Server, Shield, Terminal, Waypoints } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { ACCESS_DEFINITIONS } from '@/features/access/catalog'
import type { AccessKind } from '@/features/access/catalog'
import { ACCESS_TECHNOLOGIES } from '@/features/access/technologies'
import { ACCESS_LOGOS } from '@/features/access/logos'
import type { AccessTechnology } from '@/features/access/technologies'
import { forwardsSummary, gatewaySummary, ingressSummary, remoteSummary, socksOutSummary, tunnelBrokerSummary } from '@/features/access/summary'
import type { Summary } from '@/features/access/summary'
import { useDialConfig, useTunnelBrokerStatus } from '@/features/dial/queries'
import { useGatewayConfig, useGatewayStatus } from '@/features/gateway/queries'
import { useIngressConfig, useIngressStatus } from '@/features/ingress/queries'
import { useForwardsStatus } from '@/features/nat/queries'
import { useRemoteConfig, useRemoteStatus, useShadowsocksStatus, useSocksStatus } from '@/features/remote/queries'
import { useSocksOutConfig, useSocksOutStatus } from '@/features/socksout/queries'
import { cn } from '@/lib/utils'

// Established SVG library icons for protocols without a project mark.
const PROTOCOL_ICONS = {
  socks: Waypoints,
  http: Globe,
  trojan: Shield,
  tuic: Cable,
  gateway: Route,
  tunnelbroker: Network,
  https: LockKeyhole,
  nat: ArrowLeftRight,
  frp: Server,
  rathole: Terminal,
}

export function AccessPage() {
  const [selected, setSelected] = useState<AccessTechnology | null>(null)
  const gateway = useGatewayConfig()
  const gatewayStatus = useGatewayStatus()
  const socksOut = useSocksOutConfig()
  const socksOutStatus = useSocksOutStatus()
  const dial = useDialConfig()
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
  return (
    <div className="space-y-8">
      {(['out', 'in'] as const).map(direction => (
      <section key={direction} className="space-y-4" aria-labelledby={`access-${direction}`}>
        <h2 id={`access-${direction}`} className="text-2xl leading-8 font-semibold">Add a way {direction}</h2>
        <div className="grid grid-cols-3 gap-x-3 gap-y-6 sm:grid-cols-5 lg:grid-cols-7 xl:grid-cols-8">
          {ACCESS_TECHNOLOGIES.filter(entry => entry.directions.includes(direction)).map(entry => {
            const directional = { ...entry, connections: entry.connections.filter(kind => ACCESS_DEFINITIONS.find(definition => definition.kind === kind)?.direction === direction) }
            return <TechnologyTile key={entry.id} entry={directional} onSelect={() => setSelected(directional)} summaries={summaries} />
          })}
        </div>
      </section>
      ))}
      <Dialog open={selected !== null} onOpenChange={open => { if (!open) setSelected(null) }}>
        <DialogContent>
          {selected && <>
            <DialogHeader>
              <DialogTitle>{selected.name}</DialogTitle>
              <DialogDescription>{selected.description}</DialogDescription>
            </DialogHeader>
            {selected.connections.length ? (
              <div className="space-y-3">
                {selected.connections.map(kind => {
                  const definition = ACCESS_DEFINITIONS.find(entry => entry.kind === kind)!
                  const summary = summaries[kind]
                  return <div key={kind} className="space-y-2">
                    <p className={cn('text-sm text-muted-foreground', summary.attention && 'text-warning-foreground')}>{summary.text}</p>
                    <Button render={<Link to={definition.to} />} nativeButton={false} onClick={() => setSelected(null)}>
                      {kind === 'socks-out' ? 'Use an external proxy' : kind === 'socks-in' ? 'Share this router’s proxy' : 'Configure'}
                    </Button>
                  </div>
                })}
              </div>
            ) : (
              <div className="space-y-3">
                <p className="text-sm text-muted-foreground">Not integrated in OLR. Deploy and manage this technology separately.</p>
                <Button render={<a href={selected.website} target="_blank" rel="noopener noreferrer" />} nativeButton={false} variant="outline">Official documentation <ExternalLink aria-hidden /></Button>
              </div>
            )}
          </>}
        </DialogContent>
      </Dialog>
    </div>
  )
}

function TechnologyTile({ entry, onSelect, summaries }: { entry: AccessTechnology; onSelect: () => void; summaries: Record<AccessKind, Summary> }) {
  const logo = ACCESS_LOGOS[entry.id]
  const Icon = PROTOCOL_ICONS[entry.id as keyof typeof PROTOCOL_ICONS] ?? Network
  const integrated = entry.connections.length > 0
  return (
    <Tooltip>
      <TooltipTrigger render={<button type="button" />} onClick={onSelect} aria-label={`${entry.name}${integrated ? '' : ' · Not integrated'}`} className="group flex min-w-0 flex-col items-center gap-2 rounded-xl px-1 py-2 text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-4 focus-visible:ring-offset-background">
        <span className={cn('flex size-16 shrink-0 items-center justify-center rounded-2xl p-3 transition-colors duration-150 group-hover:bg-accent group-active:bg-accent', logo ? 'bg-white' : 'bg-accent/60 text-primary')}>
          {logo ? <img src={logo} alt="" className="max-h-full max-w-full object-contain" /> : <Icon className="size-9" strokeWidth={1.75} aria-hidden />}
        </span>
        <span className="max-w-full break-words text-center text-[13px] font-medium sm:text-sm">{entry.name}</span>
      </TooltipTrigger>
      <TooltipContent className="block space-y-2 text-sm leading-relaxed">
        <p>{entry.description}</p>
        {integrated ? entry.connections.map(kind => <p key={kind}>{entry.connections.length > 1 ? `${kind === 'socks-in' ? 'Server' : 'Client'} · ` : ''}{summaries[kind].text}</p>) : <p>Not integrated · External setup</p>}
      </TooltipContent>
    </Tooltip>
  )
}
