import { useMemo } from 'react'
import { Link } from 'react-router'
import type { FirewallStatus } from '@/lib/api-types'
import { sourceLabels, trafficFlows } from './traffic-model'
import { VerticalSankey } from './vertical-sankey'

export function TrafficPermissions({ status }: { status: FirewallStatus }) {
  const verified = status.known && !status.drifted && !status.problem
  const flows = useMemo(() => trafficFlows(status.traffic ?? []), [status.traffic])

  return (
    <section className="rounded-2xl border bg-card p-4 shadow-xs sm:p-6" aria-label="Traffic permissions">
      <h2 className="font-semibold">Traffic permissions</h2>
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
        <>
          <VerticalSankey flows={flows} />
          <p id="firewall-chart-scroll" className="mt-2 text-sm text-muted-foreground">Both sides show traffic arriving at OLR. Path widths use a logarithmic scale normalized separately for each side; compare widths within the same side only. Scroll horizontally to see all categories.</p>
          <details className="mt-4 text-sm">
            <summary className="cursor-pointer focus-visible:outline-2 focus-visible:outline-primary">Matching conditions and counts</summary>
            <ul className="mt-4 divide-y">
              {flows.map(flow => (
                <li key={flow.id} className="flex min-h-16 flex-wrap items-center justify-between gap-x-6 gap-y-2 py-4">
                  <div className="min-w-0 flex-1 basis-64">
                    <h3 className="text-sm font-medium">{flow.category}</h3>
                    <p className="mt-1 text-muted-foreground">{sourceLabels[flow.source]} · {flow.destinations.map(destination => destination === 'router' ? 'To router' : 'Forwarded').join(' / ')}</p>
                    <p className="mt-1 break-words text-muted-foreground">{flow.descriptions.join(' ')}</p>
                  </div>
                  <div className="text-sm">
                    <span className={flow.verdict === 'allowed' ? 'text-success-foreground' : 'text-warning-foreground'}>{flow.verdict === 'allowed' ? 'Allowed' : 'Blocked'}</span>
                    <p className="mt-1 tabular-nums text-muted-foreground">{flow.packets.toLocaleString()} packets · {flow.bytes.toLocaleString()} bytes</p>
                  </div>
                </li>
              ))}
            </ul>
          </details>
          <p className="mt-4 text-sm text-muted-foreground">Packet counts since the last rule update, attributed to the first matching rule. Trusted networks include remote-access WireGuard; other interfaces are external. Other firewalls can further restrict allowed traffic.</p>
        </>
      )}
    </section>
  )
}
