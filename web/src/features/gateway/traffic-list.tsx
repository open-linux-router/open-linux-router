import { List, ListEmpty, ListRow } from '@/components/ui/list'
import { Skeleton } from '@/components/ui/skeleton'
import type { GatewayTraffic, Usage } from '@/lib/api-types'
import { formatBytes } from '@/lib/utils'

export /**
 * Who used the bandwidth, and through which way out.
 *
 * The limits underneath come from the server rather than being written here, so
 * the CLI and any agent reading the same endpoint carry the same caveats. They
 * are shown rather than tucked behind a disclosure because each one explains a
 * number being *smaller* than expected, and the first question a surprising
 * number produces is "is this broken?".
 */
function TrafficList({ traffic, enabled }: { traffic?: GatewayTraffic; enabled: boolean }) {
  if (!enabled) {
    return <ListEmpty>Counting is off, so nothing is being recorded.</ListEmpty>
  }
  if (!traffic) {
    return <Skeleton className="h-24 w-full" />
  }
  if (!traffic.counting) {
    return (
      <ListEmpty>
        Counting is on, but this router could not read the counters — so nothing here
        is real yet.
      </ListEmpty>
    )
  }
  if (traffic.usage.length === 0) {
    return (
      <ListEmpty>
        Nothing counted yet. Counting starts when the router forwards traffic, and the
        totals reset when it restarts.
      </ListEmpty>
    )
  }

  return (
    <div className="space-y-4">
      <List>
        {traffic.usage.map((u) => (
          <ListRow
            key={`${u.address}/${u.exit}/${u.unknown ?? false}`}
            title={u.address}
            subtitle={describeUsageExit(u)}
            trailing={`${formatBytes(u.up_bytes)} up · ${formatBytes(u.down_bytes)} down`}
          />
        ))}
      </List>
      {traffic.capacity > 0 && traffic.held >= traffic.capacity * 0.9 ? (
        // Above the limits, and worded as a fact about this router rather than
        // as a caveat about counting: the limits explain why a number is
        // smaller than expected, this explains why a whole device is absent.
        <p className="text-[0.8rem] text-muted-foreground">
          The accounting table is nearly full ({traffic.held} of {traffic.capacity} entries).
          Devices seen recently may be missing from this list entirely.
        </p>
      ) : null}
      {traffic.limits?.length ? (
        <div className="text-[0.8rem] text-muted-foreground">
          <p className="font-medium">What these numbers do not include</p>
          <ul className="mt-1 space-y-1">
            {traffic.limits.map((l) => (
              <li key={l} className="flex gap-2">
                <span aria-hidden>·</span>
                {l}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </div>
  )
}

function describeUsageExit(u: Usage): string {
  if (u.unknown) return 'A way out that was removed'
  // Named rather than left blank, so the row reads as an answer and not a gap.
  if (!u.exit) return 'Not routed by this router'
  return `Via ${u.exit}`
}
