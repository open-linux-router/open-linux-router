import { Link } from 'react-router'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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
          One module is mounted so far. More arrive with link and dial.
        </p>
      </header>

      <div className="grid gap-4 sm:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">DHCP</CardTitle>
            <CardDescription>Address assignment</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {status.isPending ? (
              <Skeleton className="h-6 w-24" />
            ) : status.isError ? (
              <p className="text-sm text-muted-foreground">Unavailable</p>
            ) : (
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant={status.data.enabled ? 'default' : 'secondary'}>
                  {status.data.enabled ? 'Enabled' : 'Disabled'}
                </Badge>
                {status.data.drifted && !status.data.drift_error && (
                  <Badge
                    variant="secondary"
                    className="bg-amber-500/15 text-amber-700 dark:text-amber-400"
                  >
                    Not yet applied
                  </Badge>
                )}
              </div>
            )}
            <p className="text-sm text-muted-foreground">
              {active === undefined ? '—' : `${active} active lease${active === 1 ? '' : 's'}`}
            </p>
            <Button variant="outline" size="sm" render={<Link to="/dhcp">Open DHCP</Link>} />
          </CardContent>
        </Card>

        <Card className="border-dashed">
          <CardHeader>
            <CardTitle className="text-base text-muted-foreground">Networks and WAN</CardTitle>
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
