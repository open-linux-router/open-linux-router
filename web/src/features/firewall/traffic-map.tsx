import { ArrowRight, CornerDownRight, ShieldCheck, ShieldX } from 'lucide-react'
import { Link } from 'react-router'

import type { FirewallOpening, FirewallStatus } from '@/lib/api-types'

function openingLabel(opening: FirewallOpening) {
  return opening.from
    ? `${opening.protocol} from ${opening.from}`
    : `${opening.protocol} ${opening.port}`
}

function groupOpenings(openings: FirewallOpening[]) {
  const groups = new Map<string, FirewallOpening[]>()
  for (const opening of openings) {
    groups.set(opening.for, [...(groups.get(opening.for) ?? []), opening])
  }
  return [...groups]
}

function FlowRow({
  source,
  rule,
  detail,
  outcome,
  tone = 'allowed',
}: {
  source: string
  rule: React.ReactNode
  detail?: React.ReactNode
  outcome: string
  tone?: 'allowed' | 'blocked' | 'neutral'
}) {
  return (
    <div className="grid gap-2 border-t border-border/70 px-4 py-3.5 sm:grid-cols-[minmax(7rem,1fr)_minmax(12rem,2.5fr)_minmax(9rem,1fr)] sm:items-center sm:gap-5 sm:px-6">
      <div className="flex items-center gap-2 text-sm font-medium">
        <span className="hidden h-px w-4 bg-border sm:block" aria-hidden="true" />
        {source}
        <CornerDownRight className="size-4 text-muted-foreground sm:hidden" aria-hidden="true" />
      </div>
      <div className="min-w-0 text-sm">
        <div className="font-medium">{rule}</div>
        {detail && <div className="mt-1 text-xs leading-relaxed text-muted-foreground">{detail}</div>}
      </div>
      <div className="flex items-center gap-2 text-sm sm:justify-end">
        <ArrowRight className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
        <span className={`rounded-md px-2 py-1 text-xs font-semibold ${tone === 'blocked' ? 'bg-destructive/10 text-destructive' : tone === 'allowed' ? 'bg-success/10 text-success-foreground' : 'bg-muted text-muted-foreground'}`}>
          {outcome}
        </span>
      </div>
    </div>
  )
}

/** A classification of the configured policy, not a live packet trace. */
export function TrafficMap({ status }: { status: FirewallStatus }) {
  const active = status.enabled
  const verified = active && status.known && !status.drifted
  const blocked = status.blocked_input + status.blocked_forward
  const groups = groupOpenings(status.openings)

  return (
    <section aria-labelledby="traffic-map-title" className="overflow-hidden rounded-2xl border bg-card">
      <div className="flex flex-wrap items-start justify-between gap-3 px-4 py-5 sm:px-6">
        <div>
          <h2 id="traffic-map-title" className="text-lg font-semibold">Traffic map</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            Which traffic reaches this router or your networks, and why.
          </p>
        </div>
        <span className={`rounded-full px-3 py-1 text-xs font-medium ${verified ? 'bg-success/15 text-success-foreground' : 'bg-warning/15 text-warning-foreground'}`}>
          {verified ? 'Rules in force' : active ? 'Intended policy · not verified' : 'Firewall off · not enforced'}
        </span>
      </div>

      <div className="grid grid-cols-[minmax(7rem,1fr)_minmax(12rem,2.5fr)_minmax(9rem,1fr)] gap-5 bg-muted/60 px-6 py-2 text-[0.7rem] font-semibold uppercase tracking-wider text-muted-foreground max-sm:hidden">
        <span>From</span><span>Matched by</span><span className="text-right">Result</span>
      </div>

      <FlowRow
        source="Outside"
        rule="Unsolicited connection"
        detail="To this router or an inside device, with no matching exception."
        outcome={active ? 'Blocked' : 'Not filtered'}
        tone={active ? 'blocked' : 'neutral'}
      />
      <FlowRow
        source="Outside"
        rule="Router services"
        detail={groups.length ? (
          <div className="space-y-1.5">
            {groups.map(([name, openings]) => (
              <div key={name} className="flex flex-wrap items-baseline gap-x-2">
                <span className="text-foreground">{name}</span>
                <span className="font-mono text-[0.7rem]">{openings.map(openingLabel).join(' · ')}</span>
              </div>
            ))}
            <div>Essential network traffic (ICMP and DHCP client) is also allowed.</div>
          </div>
        ) : 'No configured service openings. Essential network traffic (ICMP and DHCP client) is still allowed.'}
        outcome={active ? 'This router' : 'Not filtered'}
        tone={active ? 'allowed' : 'neutral'}
      />
      <FlowRow
        source="Outside"
        rule={<Link className="underline decoration-border underline-offset-4 hover:decoration-foreground" to="/advanced/forwards">Port forwards</Link>}
        detail="Only traffic matching a configured forward goes to its named device. Review each destination and port."
        outcome={active ? 'Named device' : 'Not filtered'}
        tone={active ? 'allowed' : 'neutral'}
      />
      <FlowRow
        source="Outside"
        rule="Reply to an existing connection"
        detail="Established or related traffic is not a new inbound request."
        outcome={active ? 'Router or device' : 'Not filtered'}
        tone={active ? 'allowed' : 'neutral'}
      />
      <FlowRow
        source="Inside"
        rule="Connection started on your networks"
        detail={status.inside.length ? <span>Trusted interfaces: <span className="font-mono">{status.inside.join(', ')}</span></span> : 'No trusted interfaces configured.'}
        outcome={active ? 'Outbound allowed' : 'Not filtered'}
        tone={active ? 'allowed' : 'neutral'}
      />

      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 border-t bg-muted/30 px-4 py-3 text-xs text-muted-foreground sm:px-6">
        {active ? <ShieldCheck className="size-4 text-success-foreground" aria-hidden="true" /> : <ShieldX className="size-4" aria-hidden="true" />}
        <span>
          {active
            ? `${status.known ? `${blocked.toLocaleString()} packets blocked since the rules were last written. ` : 'Block count unavailable. '}Counts are not traffic proportions. Other interfaces count as outside, including ones added later.`
            : 'This diagram shows configured exceptions, not protection. Turn on the firewall to enforce them.'}
        </span>
      </div>
    </section>
  )
}
