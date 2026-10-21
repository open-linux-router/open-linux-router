import { t, useLocale } from '@/lib/i18n'
import { Route } from 'lucide-react'
import type { ReactNode } from 'react'
import { Link } from 'react-router'

import { RowChevron } from '@/components/ui/row-action'
import { Skeleton } from '@/components/ui/skeleton'
import { StatusInline } from '@/components/ui/status'
import { useGatewayRoutes } from '@/features/gateway/queries'
import type { RouteTable } from '@/lib/api-types'
import { state, type Health } from '@/lib/state'

/**
 * The routing table, as two cards under the stacks.
 *
 * It is not a stage of the packet's path and it is not per module, so it does
 * not join the six-card flow: every module the stacks report writes *into* this
 * table, and the table is what the kernel actually consults. A card per family
 * because a route table is per family.
 *
 * The card answers the one question the stacks cannot: how much routing is there
 * and who wrote it. Counts, not the routes — a card is not a table, and the table
 * is one link away. Which tables are OLR's and which are the box's is the page's
 * job to say, not this card's.
 */
export function GatewayRoutesCards() {
  useLocale()
  return (
    <section className="space-y-4" aria-labelledby="gateway-routes">
      <h2 id="gateway-routes" className="text-2xl leading-8 font-semibold">{t('Routes')}</h2>
      <div className="grid gap-3 sm:grid-cols-2">
        <RoutesCard family={4} />
        <RoutesCard family={6} />
      </div>
    </section>
  )
}

function RoutesCard({ family }: { family: 4 | 6 }) {
  useLocale()
  const routes = useGatewayRoutes()
  const tables = (routes.data?.tables ?? []).filter((table) => table.family === family)

  let health: Health
  let detail: ReactNode

  if (routes.isError) {
    health = state('Unavailable')
    detail = t('Route table unavailable')
  } else if (routes.isPending || !routes.data) {
    health = state('Reading…')
    detail = <Skeleton className="h-4 w-28" />
  } else if (!routes.data.known) {
    health = state('Unknown')
    detail = routes.data.error ?? t('The routing table could not be read')
  } else if (tables.length === 0) {
    health = state('No routes')
    detail = t('No route table was reported for this family')
  } else if (!defaultRoute(tables)) {
    // A table with connected subnets and no default route is a network that
    // reaches its neighbours and nothing else, which is worth a caution of its
    // own rather than hiding behind a route count.
    health = state('No default route')
    detail = t('{count} routes', { count: routeCount(tables) })
  } else {
    health = state('Default route')
    detail = [
      t('{count} routes', { count: routeCount(tables) }),
      when(exitTableCount(tables), '{count} exit tables'),
      when(otherTableCount(tables), '{count} other tables'),
    ].filter(Boolean).join(' · ')
  }

  return (
    <Link
      to="/gateway/routes"
      className="flex min-w-0 gap-3 rounded-xl border bg-card p-4 transition-colors duration-150 hover:bg-accent/60 active:bg-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
    >
      <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
        <Route className="size-4.5" strokeWidth={1.75} aria-hidden />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-sm font-semibold">{family === 4 ? t('IPv4') : t('IPv6')}</span>
          <RowChevron className="ml-auto" />
        </div>
        <StatusInline tone={health.tone} className="mt-0.5">{health.label}</StatusInline>
        <div className="mt-1 text-[0.8125rem] leading-5 text-muted-foreground [overflow-wrap:anywhere]">{detail}</div>
      </div>
    </Link>
  )
}

/** A count that only earns its segment when it is not zero. */
function when(value: number, message: string): string | undefined {
  return value > 0 ? t(message, { count: value }) : undefined
}

function routeCount(tables: RouteTable[]): number {
  return tables.reduce((sum, table) => sum + table.routes.length, 0)
}

function exitTableCount(tables: RouteTable[]): number {
  return tables.filter((table) => table.owner === 'exit').length
}

function otherTableCount(tables: RouteTable[]): number {
  return tables.filter((table) => table.owner === 'other').length
}

function defaultRoute(tables: RouteTable[]): boolean {
  return tables.some((table) => table.routes.some((route) => route.default))
}
