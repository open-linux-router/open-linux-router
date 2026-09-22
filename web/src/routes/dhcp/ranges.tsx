import { Plus } from 'lucide-react'
import { useState } from 'react'

import { SubPage } from '@/components/layout/sub-page'
import { Button } from '@/components/ui/button'
import { List, ListEmpty, ListRow } from '@/components/ui/list'
import { ApplyOutcome, useDhcpEditor } from '@/features/dhcp/editor'
import { PoolDialog } from '@/features/dhcp/pool-dialog'
import { useDhcpLeases } from '@/features/dhcp/queries'
import { useInterfaces } from '@/features/link/queries'
import type { Pool } from '@/lib/config-types'

export function DhcpRangesPage() {
  const { config, busy, change, applier, gate } = useDhcpEditor()
  const leases = useDhcpLeases()
  // Read to notice a range whose network has gone. Removing a network does not
  // remove the ranges on it (routes/networks warns before it happens), and a
  // range with no network looked exactly like a healthy one here — while the
  // server kept handing it out on whatever interface it was last given.
  const interfaces = useInterfaces()
  const networks = interfaces.isSuccess
    ? new Set(interfaces.data.groups.map((g) => g.name))
    : undefined
  const [editing, setEditing] = useState<Pool | undefined>(undefined)
  const [open, setOpen] = useState(false)

  if (!config) return gate
  const pools = config.pools ?? []

  function upsert(pool: Pool) {
    if (!config) return
    const rest = pools.filter((p) => p.group !== pool.group)
    change({ ...config, pools: [...rest, pool] })
  }

  function remove(group: string) {
    if (!config) return
    change({ ...config, pools: pools.filter((p) => p.group !== group) })
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
        <ListEmpty>No addresses yet. Pick a network and it will hand out addresses on it.</ListEmpty>
      ) : (
        <List>
          {pools.map((pool) => {
            const u = leases.data?.usage?.find((x) => x.group === pool.group)
            return (
              <ListRow
                key={pool.group}
                title={pool.group}
                subtitle={
                  networks && !networks.has(pool.group) ? (
                    <span className="text-warning">
                      There is no network called {pool.group} any more, so this range cannot be
                      served. Open it to remove it, or add the network back.
                    </span>
                  ) : (
                    describePool(pool)
                  )
                }
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
        key={editing?.group ?? 'new'}
        open={open}
        onOpenChange={setOpen}
        initial={editing}
        onSubmit={upsert}
        onRemove={editing ? () => remove(editing.group) : undefined}
      />
    </SubPage>
  )
}

/**
 * What a network hands out, in one line.
 *
 * It says "derived" rather than printing the addresses when the range was not
 * typed. Those two are genuinely different states — one is a decision somebody
 * made, the other is one this router made for them — and showing resolved
 * numbers for both would hide which is which. The Networks page prints what a
 * derived range resolves to.
 */
function describePool(pool: Pool): string {
  const parts: string[] = []
  if (!pool.ipv4) parts.push('no IPv4')
  else if (pool.ipv4.start && pool.ipv4.end) parts.push(`${pool.ipv4.start} – ${pool.ipv4.end}`)
  else parts.push('IPv4, range derived from the subnet')

  const v6 = pool.ipv6?.mode
  if (v6 === 'slaac') parts.push('IPv6 automatic')
  else if (v6 === 'stateful') parts.push('IPv6 managed')

  return parts.join(' · ')
}
