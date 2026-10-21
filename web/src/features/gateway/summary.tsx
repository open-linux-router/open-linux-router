import { t, useLocale } from '@/lib/i18n'
import { AlertTriangle, ChevronRight } from 'lucide-react'
import { useEffect } from 'react'
import { Link, useLocation } from 'react-router'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { ListEmpty } from '@/components/ui/list'
import { MetricValue } from '@/components/ui/metric-value'
import { Skeleton } from '@/components/ui/skeleton'
import { useDialStatus, useUplink } from '@/features/dial/queries'
import { useInterfaceTraffic, type RateSample } from '@/features/link/interface-traffic'
import { NetworkIcon } from '@/features/link/network-icon'
import { useInterfaces } from '@/features/link/queries'
import type { InterfaceList, InterfaceRow } from '@/lib/api-types'
import { linkHealth, state, type Health } from '@/lib/state'
import { StatusInline } from '@/components/ui/status'
import { RowChevron } from '@/components/ui/row-action'
import { cn, formatRate } from '@/lib/utils'

import { GatewayStacks } from './stacks'

/**
 * The gateway, on the page you land on.
 *
 * Two layers. The interfaces are the objects: what this box is plugged into,
 * each card opening that interface's own page. Below them the page is two
 * stacks, IPv4 and IPv6, each cut across the modules in the order a packet
 * meets them — upstream, LAN, DHCP, forwarding, exit, DNS — so "does IPv6
 * work, and where do I fix it" is answered without knowing which module owns
 * which half. Every card links to the page that changes it and holds no
 * configuration of its own (features/gateway/stacks.tsx).
 *
 * There is no status strip and there are no switches. Routing's own state is a
 * detail of the routing settings, and the stacks report its writing rather than
 * offering to change it; Overview is where a gateway fault is reported.
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
    <div className="space-y-8">
      <InterfacesCard />
      <GatewayStacks />
    </div>
  )
}

/* -------------------------------------------------------------------------- */

function InterfacesCard() {
  useLocale()
  const interfaces = useInterfaces()
  const uplink = useUplink()
  const dial = useDialStatus()
  const traffic = useInterfaceTraffic(interfaces.data, interfaces.isError)
  const rows = (interfaces.data?.interfaces ?? []).filter(row => row.physical)
  // The configured uplink when olr owns it, otherwise the interface the observed
  // default route leaves by — the WAN is a fact, not only a setting.
  const wan = uplink.isError ? undefined : (uplink.data?.uplink?.interface ?? dial.data?.route?.dev)

  return (
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
              />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

/**
 * One interface, three lines: what it is, what address it holds, what it is
 * moving.
 *
 * The tile is a link to the interface's own page rather than a button opening a
 * dialog. A dialog could not hold the readings this object has — the negotiated
 * link speed, both counters, its addresses, the network it carries and the
 * internet route it is given — and the page behind it already keeps the
 * switches that change them, so the card's job is to get out of the way.
 *
 * The role moved out of the picture. Drawn inside the 48px glyph it was 9px
 * text and two 3px squares that had no name, which read as decoration because
 * it was. Link speed left too: on a box whose drivers do not report it, the
 * card said "Unknown" three times and taught nobody anything, while the
 * interface page still has the value and the sentence that explains its
 * absence.
 */
function InterfaceTile({ row, role, current, failed }: {
  row: InterfaceRow
  role?: string
  current?: RateSample
  failed: boolean
}) {
  useLocale()
  // The tone decides whether the link is carrying traffic; the translated label
  // only ever reaches a reader. Comparing labels would break on a locale change
  // and read as "no traffic" on a busy interface.
  const health: Health = failed ? state('Unavailable') : linkHealth(row)
  const connected = health.tone === 'ok'
  const address = row.address || row.prefixes?.[0]?.split('/')[0]
  return (
    <Link
      to={`/gateway/interfaces/${encodeURIComponent(row.name)}`}
      aria-label={`${row.name}${role ? `, ${role}` : ''}: ${health.label}${address ? `, ${address}` : ''}. ${t('Details')}`}
      className="flex h-full min-w-0 flex-col rounded-xl bg-muted p-4 transition-colors duration-150 hover:bg-accent active:bg-accent/80 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
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
          <StatusInline tone={health.tone} className="mt-0.5">{health.label}</StatusInline>
        </div>
        <RowChevron className="mt-1" />
      </div>
      <div className="mt-3 min-w-0 text-[0.8125rem] leading-5">
        {address
          ? <span className="block font-mono [overflow-wrap:anywhere]">{address}</span>
          : <span className="block text-muted-foreground">{t('No address')}</span>}
      </div>
      <div className="mt-auto flex flex-wrap items-center gap-x-4 gap-y-1 pt-3 text-sm font-medium tabular-nums">
        {failed ? <span className="text-muted-foreground">{t('No readings')}</span>
          : !connected ? <span className="text-muted-foreground">{t('No traffic')}</span>
            : current ? (
              <>
                <span className="text-receive" aria-label={t('Receive {rate}', { rate: formatRate(current.rx) })}>↓ <MetricValue value={formatRate(current.rx)} /></span>
                <span className="text-transmit" aria-label={t('Transmit {rate}', { rate: formatRate(current.tx) })}>↑ <MetricValue value={formatRate(current.tx)} /></span>
              </>
            ) : <span className="text-muted-foreground">{t('Measuring…')}</span>}
      </div>
    </Link>
  )
}

/* -------------------------------------------------------------------------- */

const sectionLink = 'inline-flex min-h-11 items-center gap-1 text-sm font-medium text-primary underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring sm:min-h-9'

function roleOf(row: InterfaceRow, wan: string | undefined, data?: InterfaceList) {
  if (row.name === wan) return 'WAN'
  if (row.network || data?.networks.some(network => network.members.includes(row.name))) return 'LAN'
  return undefined
}
