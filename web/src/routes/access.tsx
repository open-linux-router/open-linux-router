import { t, useLocale } from '@/lib/i18n'
import { Network } from 'lucide-react'

import { Launcher, LauncherSection, LauncherTile } from '@/components/layout/launcher'
import type { LauncherMark } from '@/components/layout/launcher'
import { ACCESS_DEFINITIONS } from '@/features/access/catalog'
import type { AccessDirection, AccessKind } from '@/features/access/catalog'
import { ACCESS_TECHNOLOGIES } from '@/features/access/technologies'
import { ACCESS_ARTWORK, ACCESS_LOGOS } from '@/features/access/logos'
import type { AccessTechnology } from '@/features/access/technologies'
import { gatewaySummary, remoteSummary, socksOutSummary, tunnelBrokerSummary } from '@/features/access/summary'
import type { Summary } from '@/features/access/summary'
import { useDialConfig, useTunnelBrokerStatus } from '@/features/dial/queries'
import { useGatewayConfig, useGatewayStatus } from '@/features/gateway/queries'
import { useRemoteConfig, useRemoteStatus, useSocksStatus } from '@/features/remote/queries'
import { useSocksOutConfig, useSocksOutStatus } from '@/features/socksout/queries'

// Entries without project artwork or an approved app icon retain an initial.
const MONOGRAMS: Record<string, { letter: string; hue: number }> = {
  ssh: { letter: 'S', hue: 220 },
  gateway: { letter: 'G', hue: 330 },
}

export function AccessPage() {
  useLocale()
  const gateway = useGatewayConfig()
  const gatewayStatus = useGatewayStatus()
  const socksOut = useSocksOutConfig()
  const socksOutStatus = useSocksOutStatus()
  const dial = useDialConfig()
  const broker = useTunnelBrokerStatus()
  const remote = useRemoteConfig()
  const wireguard = useRemoteStatus()
  const socks = useSocksStatus()
  const summaries: Record<AccessKind, Summary> = {
    gateway: gatewaySummary(gateway, gatewayStatus),
    'socks-out': socksOutSummary(socksOut, socksOutStatus),
    tunnelbroker: tunnelBrokerSummary(dial, broker),
    wireguard: remoteSummary(remote, wireguard, socks, 'wireguard'),
    'socks-in': remoteSummary(remote, wireguard, socks, 'socks'),
  }
  return (
    <Launcher>
      {(['network', 'out', 'in'] as const).map(direction => (
        <LauncherSection key={direction} id={direction} title={direction === 'network' ? t('Networking') : direction === 'out' ? t('Add a way out') : t('Add a way in')}>
          {ACCESS_TECHNOLOGIES.filter(entry => entry.directions.includes(direction)).map(entry => (
            <TechnologyTile key={entry.id} entry={entry} kinds={connectionsFor(entry, direction)} summaries={summaries} />
          ))}
        </LauncherSection>
      ))}
    </Launcher>
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
  const artwork = ACCESS_ARTWORK[entry.id]
  const logo = ACCESS_LOGOS[entry.id]
  const monogram = MONOGRAMS[entry.id]
  const definition = kinds.length ? ACCESS_DEFINITIONS.find(item => item.kind === kinds[0]) : undefined
  const mark: LauncherMark = artwork
    ? { kind: 'app-icon', src: artwork }
    : logo
      ? { kind: 'logo', src: logo }
      : monogram
        ? { kind: 'letter', letter: monogram.letter, hue: monogram.hue }
        : { kind: 'icon', children: <Network className="size-9" strokeWidth={1.75} aria-hidden /> }
  return (
    <LauncherTile
      name={entry.name}
      description={entry.description}
      mark={mark}
      notes={kinds.map(kind => summaries[kind])}
      to={definition?.to}
      href={definition ? undefined : entry.website}
    />
  )
}
