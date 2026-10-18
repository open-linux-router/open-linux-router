import { Heading } from '@/components/ui/heading'
import { useMemo } from 'react'
import { Link } from 'react-router'
import type { FirewallStatus } from '@/lib/api-types'
import { trafficFlows } from './traffic-model'
import { VerticalSankey } from './vertical-sankey'

export function TrafficPermissions({ status }: { status: FirewallStatus }) {
  const verified = status.known && !status.drifted && !status.problem
  const flows = useMemo(() => trafficFlows(status.traffic ?? []), [status.traffic])

  return (
    <section className="panel-surface p-4 sm:p-6" aria-label="Traffic permissions">
      <Heading>Traffic permissions</Heading>
      {!status.enabled ? (
        <p className="mt-4 text-sm text-muted-foreground">The firewall is off. OLR is not enforcing traffic permissions.</p>
      ) : status.native ? (
        <p className="mt-4 text-sm text-muted-foreground">Native rules define the traffic permissions. <Link className="text-primary underline" to="/firewall/rules">Inspect rules</Link></p>
      ) : !verified ? (
        <p className="mt-4 text-sm text-muted-foreground">Traffic permissions could not be verified. <Link className="text-primary underline" to="/firewall/rules">Inspect current rules</Link></p>
      ) : !status.traffic_known ? (
        <p className="mt-4 text-sm text-muted-foreground">Traffic counters are unavailable.</p>
      ) : flows.length === 0 ? (
        <p className="mt-4 text-sm text-muted-foreground">No traffic recorded since the rules were last written.</p>
      ) : (
        <VerticalSankey flows={flows} />
      )}
    </section>
  )
}
