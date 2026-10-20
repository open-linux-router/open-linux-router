import { Network } from 'lucide-react'
import { Link } from 'react-router'

import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { ACCESS_DEFINITIONS } from '@/features/access/catalog'
import type { AccessDirection, AccessKind } from '@/features/access/catalog'
import { ACCESS_TECHNOLOGIES } from '@/features/access/technologies'
import { ACCESS_LOGOS } from '@/features/access/logos'
import type { AccessTechnology } from '@/features/access/technologies'
import { forwardsSummary, gatewaySummary, remoteSummary, socksOutSummary, tunnelBrokerSummary } from '@/features/access/summary'
import type { Summary } from '@/features/access/summary'
import { useDialConfig, useTunnelBrokerStatus } from '@/features/dial/queries'
import { useGatewayConfig, useGatewayStatus } from '@/features/gateway/queries'
import { useForwardsStatus } from '@/features/nat/queries'
import { useRemoteConfig, useRemoteStatus, useSocksStatus } from '@/features/remote/queries'
import { useSocksOutConfig, useSocksOutStatus } from '@/features/socksout/queries'
import { cn } from '@/lib/utils'

// Protocols are named, not branded: theSVG carries no mark for them, so each
// wears its initial on a tinted tile. SSH and SOCKS5 share an initial; the hue
// keeps them apart at a glance.
const MONOGRAMS: Record<string, { letter: string; tile: string }> = {
  ssh: { letter: 'S', tile: 'bg-[oklch(0.52_0.12_220)]' },
  socks: { letter: 'S', tile: 'bg-[oklch(0.52_0.12_290)]' },
  gateway: { letter: 'G', tile: 'bg-[oklch(0.52_0.12_330)]' },
  nat: { letter: 'N', tile: 'bg-[oklch(0.55_0.14_25)]' },
  frp: { letter: 'F', tile: 'bg-[oklch(0.58_0.1_190)]' },
}

export function AccessPage() {
  const gateway = useGatewayConfig()
  const gatewayStatus = useGatewayStatus()
  const socksOut = useSocksOutConfig()
  const socksOutStatus = useSocksOutStatus()
  const dial = useDialConfig()
  const broker = useTunnelBrokerStatus()
  const remote = useRemoteConfig()
  const wireguard = useRemoteStatus()
  const socks = useSocksStatus()
  const forwards = useForwardsStatus()
  const summaries: Record<AccessKind, Summary> = {
    gateway: gatewaySummary(gateway, gatewayStatus),
    'socks-out': socksOutSummary(socksOut, socksOutStatus),
    tunnelbroker: tunnelBrokerSummary(dial, broker),
    wireguard: remoteSummary(remote, wireguard, socks, 'wireguard'),
    'socks-in': remoteSummary(remote, wireguard, socks, 'socks'),
    'port-forward': forwardsSummary(gateway, forwards),
  }
  return (
    <div className="space-y-8">
      {(['network', 'out', 'in'] as const).map(direction => (
      <section key={direction} className="space-y-4" aria-labelledby={`access-${direction}`}>
        <h2 id={`access-${direction}`} className="text-2xl leading-8 font-semibold">{direction === 'network' ? 'Networking' : `Add a way ${direction}`}</h2>
        <div className="grid grid-cols-3 gap-x-3 gap-y-6 sm:grid-cols-5 lg:grid-cols-7 xl:grid-cols-8">
          {ACCESS_TECHNOLOGIES.filter(entry => entry.directions.includes(direction)).map(entry => (
            <TechnologyTile key={entry.id} entry={entry} kinds={connectionsFor(entry, direction)} summaries={summaries} />
          ))}
        </div>
      </section>
      ))}
    </div>
  )
}

// A tile belongs to one direction, so it links to at most one configuration
// page: a technology's client and server are separate ways with separate owners.
function connectionsFor(entry: AccessTechnology, direction: 'network' | AccessDirection): AccessKind[] {
  if (direction === 'network') return entry.connections
  return entry.connections.filter(kind => ACCESS_DEFINITIONS.find(definition => definition.kind === kind)?.direction === direction)
}

// Clicking a tile leaves the directory: a technology OLR configures opens its
// configuration page, and one it does not opens the project's own documentation.
function TechnologyTile({ entry, kinds, summaries }: { entry: AccessTechnology; kinds: AccessKind[]; summaries: Record<AccessKind, Summary> }) {
  const logo = ACCESS_LOGOS[entry.id]
  const monogram = MONOGRAMS[entry.id]
  const definition = kinds.length ? ACCESS_DEFINITIONS.find(item => item.kind === kinds[0]) : undefined
  const tile = logo
    ? 'bg-white group-hover:bg-accent group-active:bg-accent'
    : monogram
      ? `${monogram.tile} text-white group-hover:brightness-110 group-active:brightness-95`
      : 'bg-accent/60 text-primary group-hover:bg-accent group-active:bg-accent'
  return (
    <Tooltip>
      <TooltipTrigger
        render={definition ? <Link to={definition.to} /> : entry.website ? <a href={entry.website} target="_blank" rel="noopener noreferrer" /> : <span />}
        aria-label={definition ? `${entry.name} · ${summaries[kinds[0]].text}` : `${entry.name} · Not integrated`}
        className="group flex min-w-0 flex-col items-center gap-2 rounded-xl px-1 py-2 text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-4 focus-visible:ring-offset-background"
      >
        <span className={cn('flex size-16 shrink-0 items-center justify-center rounded-2xl p-3 transition duration-150', tile)}>
          {logo
            ? <img src={logo} alt="" className="max-h-full max-w-full object-contain" />
            : monogram
              ? <span aria-hidden className="text-[28px] leading-none font-semibold tracking-tight">{monogram.letter}</span>
              : <Network className="size-9" strokeWidth={1.75} aria-hidden />}
        </span>
        <span className="max-w-full break-words text-center text-[13px] font-medium sm:text-sm">{entry.name}</span>
      </TooltipTrigger>
      <TooltipContent className="block space-y-2 text-sm leading-relaxed">
        <p>{entry.description}</p>
        {kinds.length ? kinds.map(kind => <p key={kind}>{summaries[kind].text}</p>) : <p>Not integrated · External setup</p>}
      </TooltipContent>
    </Tooltip>
  )
}
