import { t, useLocale } from '@/lib/i18n'
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
  useLocale()
  if (exits.length === 0) {
    return (
      <ListEmpty>
        {/* docs/gateway.md §10: the empty state should guide, and it is where
            the SOCKS5 question gets answered before somebody tries it. */}
        No ways out yet. Add an interface or gateway. An external proxy needs a routing interface first.
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
            wrap
            trailing={e.via.kind === 'blocked' ? <Badge variant="secondary">{t('Blocking')}</Badge> : row ? <HealthBadge row={row} /> : <Badge variant="secondary">{t('Status unknown')}</Badge>}
            onSelect={() => onEdit(e)}
          />
        )
      })}
    </List>
  )
}

function HealthBadge({ row }: { row: ExitStatus }) {
  useLocale()
  // "Not checked" rather than "up": claiming health nobody measured is exactly
  // where design.md §5.6 says faults go to hide.
  if (!row.probed) return <Badge variant="secondary">{t('Not checked')}</Badge>
  return row.up ? <Badge variant="success">{t('Working')}</Badge> : <Badge variant="destructive">{t('Down')}</Badge>
}

function describeExit(e: Exit, row?: ExitStatus): string {
  const where =
    e.via.kind === 'blocked'
      ? 'Blocks traffic'
      : e.via.kind === 'interface'
        ? `Out ${e.via.interface}`
        : `To ${e.via.next_hop}`
  const used = row?.used_by?.length ? ` · used by ${row.used_by.join(', ')}` : ''
  const dns =
    e.dns?.mode === 'exit'
      ? ' · DNS via this box'
      : e.dns?.mode === 'custom'
        ? ` · DNS ${e.dns.server}`
        : ' · DNS via OLR'

  return where + dns + used
}
