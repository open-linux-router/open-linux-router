import { Cable, Cloud, ExternalLink, Globe, KeyRound, LockKeyhole, Network, Radio, Route, Server, Shield, Terminal, Waypoints, Wind, Zap } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { ACCESS_DEFINITIONS } from '@/features/access/catalog'
import type { AccessKind } from '@/features/access/catalog'
import { ACCESS_TECHNOLOGIES, ACCESS_USES } from '@/features/access/technologies'
import type { AccessTechnology } from '@/features/access/technologies'
import { forwardsSummary, gatewaySummary, ingressSummary, remoteSummary, socksOutSummary, systemSummary, tunnelBrokerSummary } from '@/features/access/summary'
import type { Summary } from '@/features/access/summary'
import { useDialConfig, useDialStatus, useTunnelBrokerStatus } from '@/features/dial/queries'
import { useGatewayConfig, useGatewayStatus } from '@/features/gateway/queries'
import { useIngressConfig, useIngressStatus } from '@/features/ingress/queries'
import { useForwardsStatus } from '@/features/nat/queries'
import { useRemoteConfig, useRemoteStatus, useShadowsocksStatus, useSocksStatus } from '@/features/remote/queries'
import { useSocksOutConfig, useSocksOutStatus } from '@/features/socksout/queries'
import { cn } from '@/lib/utils'

const ICONS = { wireguard: Shield, tailscale: Network, netbird: Radio, zerotier: Globe, headscale: Network, nebula: Waypoints, openvpn: LockKeyhole, ipsec: KeyRound, frp: Cable, cloudflare: Cloud, rathole: Cable, ssh: Terminal, shadowsocks: Wind, socks: Waypoints, http: Globe, trojan: Shield, vless: Zap, vmess: Zap, hysteria: Wind, tuic: Zap, gateway: Route, tunnelbroker: Globe, https: Server, nat: Route }

export function AccessPage() {
  const [use, setUse] = useState<typeof ACCESS_USES[number]>('All')
  const [selected, setSelected] = useState<AccessTechnology | null>(null)
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
      <section className="space-y-4" aria-label="Connection technologies">
        <div className="flex flex-wrap gap-2" role="group" aria-label="Filter by purpose">
          {ACCESS_USES.map(item => (
            <Button key={item} variant={use === item ? 'secondary' : 'ghost'} aria-pressed={use === item} onClick={() => setUse(item)}>{item}</Button>
          ))}
        </div>
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4 sm:gap-4 lg:grid-cols-5 xl:grid-cols-6">
          {ACCESS_TECHNOLOGIES.filter(entry => use === 'All' || entry.uses.includes(use)).map(entry => (
            <TechnologyTile key={entry.id} entry={entry} onSelect={() => setSelected(entry)} summaries={summaries} />
          ))}
        </div>
      </section>
      <Dialog open={selected !== null} onOpenChange={open => { if (!open) setSelected(null) }}>
        <DialogContent>
          {selected && <>
            <DialogHeader>
              <DialogTitle>{selected.name}</DialogTitle>
              <DialogDescription>{selected.description}</DialogDescription>
            </DialogHeader>
            <div className="flex flex-wrap gap-2 text-sm text-muted-foreground">
              {selected.uses.map(item => <span key={item} className="rounded-md bg-muted px-2 py-1">{item}</span>)}
            </div>
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
  const Icon = ICONS[entry.id as keyof typeof ICONS]
  const integrated = entry.connections.length > 0
  return (
    <Tooltip>
      <TooltipTrigger render={<button type="button" />} onClick={onSelect} aria-label={`${entry.name}${integrated ? '' : ' · Not integrated'}`} className="relative flex aspect-square min-w-0 flex-col items-center justify-center gap-4 rounded-xl border bg-card p-3 text-card-foreground shadow-xs transition-colors duration-150 hover:border-primary/50 hover:bg-accent/30 active:bg-accent/50 focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring">
        <Icon className={cn('size-9 sm:size-10', integrated ? 'text-primary' : 'text-muted-foreground')} strokeWidth={1.75} aria-hidden />
        <span className="max-w-full break-words text-center text-sm font-medium">{entry.name}</span>
        {!integrated && <span className="absolute right-2 top-2 text-xs text-muted-foreground" aria-hidden>↗</span>}
      </TooltipTrigger>
      <TooltipContent className="block space-y-2 text-sm leading-relaxed">
        <p>{entry.description}</p>
        {integrated ? entry.connections.map(kind => <p key={kind}>{entry.connections.length > 1 ? `${kind === 'socks-in' ? 'Server' : 'Client'} · ` : ''}{summaries[kind].text}</p>) : <p>Not integrated · External setup</p>}
      </TooltipContent>
    </Tooltip>
  )
}
