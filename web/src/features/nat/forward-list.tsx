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
        No port forwards yet. Add a rule to publish a device port.
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
            subtitle={describeForward(f)}
            wrap
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

function describeForward(f: Forward): string {
  const proto = PROTOCOL_LABEL[f.protocol ?? ''] ?? 'TCP'
  const hairpin = f.hairpin === false ? ' · not from inside' : ''

  return `${f.in} ${proto} ${f.port} → ${f.to}${hairpin}`
}
