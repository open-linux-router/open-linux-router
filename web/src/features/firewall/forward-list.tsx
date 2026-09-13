import { Badge } from '@/components/ui/badge'
import { List, ListEmpty, ListRow } from '@/components/ui/list'
import type { ForwardStatus } from '@/lib/api-types'
import type { Forward } from '@/lib/config-types'
import { formatBytes } from '@/lib/utils'

export function ForwardList({
  forwards,
  status,
  onEdit,
}: {
  forwards: Forward[]
  status?: ForwardStatus[]
  onEdit: (forward: Forward) => void
}) {
  if (forwards.length === 0) {
    return (
      <ListEmpty>
        Nothing is being forwarded in. Add one to let something on the internet reach a device on
        your network — a web server, a game, a camera.
      </ListEmpty>
    )
  }

  return (
    <List>
      {forwards.map((f) => {
        const row = status?.find((s) => s.name === f.name)
        return (
          <ListRow
            key={f.name}
            title={f.name}
            subtitle={describeForward(f, row)}
            trailing={row ? <ArrivedBadge row={row} /> : undefined}
            onSelect={() => onEdit(f)}
          />
        )
      })}
    </List>
  )
}

/**
 * Whether anything has arrived, which is the first question when a port does not
 * work — and it has three answers, not two.
 *
 * "Nothing yet" says the rule is in the kernel and no packet has matched it,
 * which points outward: the ISP, the modem, or somebody else's filter. "Not
 * counted" says we could not read the counter, which points at this box.
 * Collapsing them into a zero would send an operator to debug the wrong half.
 */
function ArrivedBadge({ row }: { row: ForwardStatus }) {
  if (!row.counted) return <Badge variant="secondary">Not counted</Badge>
  if (row.packets === 0) return <Badge variant="secondary">Nothing yet</Badge>
  return <Badge variant="success">{formatBytes(row.bytes)} in</Badge>
}

/** The schema word for `both` is not a label; the other two already are. */
const PROTOCOL_LABEL: Record<string, string> = {
  '': 'TCP',
  tcp: 'TCP',
  udp: 'UDP',
  both: 'TCP+UDP',
}

function describeForward(f: Forward, row?: ForwardStatus): string {
  const proto = PROTOCOL_LABEL[f.protocol ?? ''] ?? 'TCP'
  const hairpin = f.hairpin === false ? ' · not from inside' : ''

  // The badge beside this row is `hidden sm:block`, so on a phone it is the only
  // thing that would say nothing has ever arrived — which is the most
  // operationally important fact on the screen and the worst one to drop at the
  // width most people will read it at. Said in front of the subtitle, where it
  // survives both the breakpoint and the truncation.
  const quiet = row?.counted && row.packets === 0 ? 'Nothing has arrived · ' : ''
  return `${quiet}${f.in} ${proto} ${f.port} → ${f.to}${hairpin}`
}
