import { ChevronRight } from 'lucide-react'
import { Link } from 'react-router'

import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { useDhcpLeases, useDhcpStatus } from '@/features/dhcp/queries'

/**
 * A placeholder until there is something real to show.
 *
 * The overview this project actually wants — throughput, WAN state, a device
 * inventory — needs the link and dial modules, which are milestone 1 and not
 * yet written (design.md §9). Rather than invent numbers, this shows only what
 * the one mounted module can honestly answer.
 */
export function OverviewPage() {
  const status = useDhcpStatus()
  const leases = useDhcpLeases()

  const active = leases.data?.leases.filter((l) => l.active).length

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl font-semibold tracking-tight">Overview</h1>
        <p className="text-sm text-muted-foreground">
          One part of the router is set up so far. More arrive with link and dial.
        </p>
      </header>

      <div className="grid gap-4 sm:grid-cols-2">
        {/* The whole card is the link. One target beats a card containing a
            button, and it gives the row the same affordance as everywhere
            else in the app. */}
        <Card className="transition-colors focus-within:border-ring hover:border-foreground/20">
          <Link to="/dhcp" className="block outline-none">
            <CardHeader>
              <CardTitle className="flex items-center gap-1.5 text-base">
                Addresses
                <ChevronRight className="size-4 text-muted-foreground/60" aria-hidden />
              </CardTitle>
              <CardDescription>Handing out addresses to your devices</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              {status.isPending ? (
                <Skeleton className="h-5 w-24" />
              ) : status.isError ? (
                <p className="text-sm text-muted-foreground">Cannot reach the router</p>
              ) : (
                <div className="flex flex-wrap items-center gap-2">
                  <Badge variant={status.data.enabled ? 'success' : 'secondary'}>
                    {status.data.enabled ? 'On' : 'Off'}
                  </Badge>
                  {status.data.drifted && !status.data.drift_error && (
                    <Badge variant="warning">Not yet applied</Badge>
                  )}
                </div>
              )}
              {active === undefined ? (
                <Skeleton className="h-4 w-32" />
              ) : (
                <p className="text-sm text-muted-foreground">
                  {active} device{active === 1 ? '' : 's'} connected
                </p>
              )}
            </CardContent>
          </Link>
        </Card>

        {/* Card draws its edge with a ring, so `border-dashed` alone was doing
            nothing and this read as an equal peer to the live card. Drop the
            ring and use a real dashed border to mark it as a placeholder. */}
        <Card className="border border-dashed bg-transparent ring-0">
          <CardHeader>
            <CardTitle className="text-base text-muted-foreground">
              Networks and internet
            </CardTitle>
            <CardDescription>
              Arrives with the link and dial modules. Until then this box has no
              opinion about its own interfaces.
            </CardDescription>
          </CardHeader>
        </Card>
      </div>
    </div>
  )
}
