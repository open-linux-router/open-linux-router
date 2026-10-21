import { t, useLocale } from '@/lib/i18n'
import { StatusInline } from '@/components/ui/status'
import { state } from '@/lib/state'
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
            trailing={e.via.kind === 'blocked' ? <StatusInline tone="off">{t('Blocking')}</StatusInline> : row ? <ExitState row={row} /> : <StatusInline tone="unknown">{t('Status unknown')}</StatusInline>}
            editLabel={`${t('Edit')} ${e.name}`}
            onSelect={() => onEdit(e)}
          />
        )
      })}
    </List>
  )
}

function ExitState({ row }: { row: ExitStatus }) {
  useLocale()
  // "Not checked" rather than "up": claiming health nobody measured is exactly
  // where design.md §5.6 says faults go to hide.
  const health = !row.probed ? state('Not checked') : state(row.up ? 'Working' : 'Down')
  return <StatusInline tone={health.tone}>{health.label}</StatusInline>
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
