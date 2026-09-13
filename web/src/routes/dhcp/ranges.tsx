import { Plus } from 'lucide-react'
import { useState } from 'react'

import { SubPage } from '@/components/layout/sub-page'
import { Button } from '@/components/ui/button'
import { List, ListEmpty, ListRow } from '@/components/ui/list'
import { ApplyOutcome, useDhcpEditor } from '@/features/dhcp/editor'
import { PoolDialog } from '@/features/dhcp/pool-dialog'
import { useDhcpLeases } from '@/features/dhcp/queries'
import type { Pool } from '@/lib/config-types'

export function DhcpRangesPage() {
  const { config, busy, change, applier, gate } = useDhcpEditor()
  const leases = useDhcpLeases()
  const [editing, setEditing] = useState<Pool | undefined>(undefined)
  const [open, setOpen] = useState(false)

  if (!config) return gate
  const pools = config.pools ?? []

  function upsert(pool: Pool) {
    if (!config) return
    const rest = pools.filter((p) => p.interface !== pool.interface)
    change({ ...config, pools: [...rest, pool] })
  }

  function remove(iface: string) {
    if (!config) return
    change({ ...config, pools: pools.filter((p) => p.interface !== iface) })
  }

  return (
    <SubPage section="/dhcp" slug="ranges">
      <ApplyOutcome applier={applier} />

      <div className="flex justify-end">
        <Button
          size="sm"
          disabled={busy}
          onClick={() => {
            setEditing(undefined)
            setOpen(true)
          }}
        >
          <Plus className="size-4" aria-hidden /> Add
        </Button>
      </div>

      {pools.length === 0 ? (
        <ListEmpty>No ranges yet. Add one so devices can get an address.</ListEmpty>
      ) : (
        <List>
          {pools.map((pool) => {
            const u = leases.data?.usage?.find((x) => x.interface === pool.interface)
            return (
              <ListRow
                key={pool.interface}
                title={pool.interface}
                subtitle={`${pool.start} – ${pool.end}`}
                trailing={u ? `${u.active} of ${u.size} in use` : undefined}
                onSelect={
                  busy
                    ? undefined
                    : () => {
                        setEditing(pool)
                        setOpen(true)
                      }
                }
              />
            )
          })}
        </List>
      )}

      <PoolDialog
        key={editing?.interface ?? 'new'}
        open={open}
        onOpenChange={setOpen}
        initial={editing}
        onSubmit={upsert}
        onRemove={editing ? () => remove(editing.interface) : undefined}
      />
    </SubPage>
  )
}
