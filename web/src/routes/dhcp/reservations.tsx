import { Plus } from 'lucide-react'
import { useState } from 'react'

import { SubPage } from '@/components/layout/sub-page'
import { Button } from '@/components/ui/button'
import { List, ListEmpty, ListRow } from '@/components/ui/list'
import { ApplyOutcome, useDhcpEditor } from '@/features/dhcp/editor'
import { ReservationDialog } from '@/features/dhcp/reservation-dialog'
import type { Reservation } from '@/lib/config-types'

export function DhcpReservationsPage() {
  const { config, busy, change, applier, gate } = useDhcpEditor()
  const [editing, setEditing] = useState<Reservation | undefined>(undefined)
  const [open, setOpen] = useState(false)

  if (!config) return gate
  const reservations = config.reservations ?? []

  function upsert(reservation: Reservation) {
    if (!config) return
    const key = reservation.mac.toLowerCase()
    const rest = reservations.filter((r) => r.mac.toLowerCase() !== key)
    change({ ...config, reservations: [...rest, reservation] })
  }

  function remove(mac: string) {
    if (!config) return
    change({ ...config, reservations: reservations.filter((x) => x.mac !== mac) })
  }

  return (
    <SubPage section="/dhcp" slug="reservations">
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

      {reservations.length === 0 ? (
        <ListEmpty>No reserved addresses.</ListEmpty>
      ) : (
        <List>
          {reservations.map((r) => (
            <ListRow
              key={r.mac}
              title={r.hostname ?? r.ip}
              subtitle={r.hostname ? `${r.ip} · ${r.mac}` : r.mac}
              onSelect={
                busy
                  ? undefined
                  : () => {
                      setEditing(r)
                      setOpen(true)
                    }
              }
            />
          ))}
        </List>
      )}

      <ReservationDialog
        key={editing?.mac ?? 'new'}
        open={open}
        onOpenChange={setOpen}
        initial={editing}
        onSubmit={upsert}
        onRemove={editing ? () => remove(editing.mac) : undefined}
      />
    </SubPage>
  )
}
