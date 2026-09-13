import { Badge } from '@/components/ui/badge'
import { List, ListEmpty, ListRow } from '@/components/ui/list'
import type { ExitStatus } from '@/lib/api-types'
import type { Exit } from '@/lib/config-types'

export function ExitList({
  exits,
  status,
  onEdit,
}: {
  exits: Exit[]
  status?: ExitStatus[]
  onEdit: (exit: Exit) => void
}) {
  if (exits.length === 0) {
    return (
      <ListEmpty>
        {/* docs/gateway.md §10: the empty state should guide, and it is where
            the SOCKS5 question gets answered before somebody tries it. */}
        Nothing here yet. Add the box or connection you want traffic to go
        through. A SOCKS5 or HTTP proxy on its own cannot be used — it only
        carries traffic that knows to ask it.
      </ListEmpty>
    )
  }

  return (
    <List>
      {exits.map((e) => {
        const row = status?.find((s) => s.name === e.name)
        return (
          <ListRow
            key={e.name}
            title={e.name}
            subtitle={describeExit(e, row)}
            trailing={row ? <HealthBadge row={row} /> : undefined}
            onSelect={() => onEdit(e)}
          />
        )
      })}
    </List>
  )
}

function HealthBadge({ row }: { row: ExitStatus }) {
  // "Not checked" rather than "up": claiming health nobody measured is exactly
  // where design.md §5.6 says faults go to hide.
  if (!row.probed) return <Badge variant="secondary">Not checked</Badge>
  return row.up ? <Badge variant="success">Working</Badge> : <Badge variant="destructive">Down</Badge>
}

function describeExit(e: Exit, row?: ExitStatus): string {
  const where =
    e.via.kind === 'blocked'
      ? 'Blocks traffic'
      : e.via.kind === 'interface'
        ? `Out ${e.via.interface}`
        : `To ${e.via.next_hop}`
  const used = row?.used_by?.length ? ` · used by ${row.used_by.join(', ')}` : ''

  // The badge beside this row is `hidden sm:block`, so on a phone it is the
  // only thing that would say an exit has stopped working — which is the most
  // operationally important fact on the screen and the worst one to drop at the
  // width most people will read it at. Said in the subtitle too, where it
  // survives, rather than only in the badge.
  const down = row && row.probed && !row.up ? 'Not responding · ' : ''
  return down + where + used
}
