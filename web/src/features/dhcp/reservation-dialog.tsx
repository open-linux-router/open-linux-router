import { Trash2 } from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { Reservation } from '@/lib/config-types'

const EMPTY: Reservation = { mac: '', ip: '' }

/**
 * Adds or edits a fixed address.
 *
 * design.md §11.1 says a fixed address is a property *of a device*, set from the
 * device list rather than by typing a MAC into a form. That is now how it is
 * normally reached: the Devices screen opens this dialog with the MAC already
 * filled in, so the operator never goes and finds a hardware address.
 *
 * The flat MAC-to-IP form survives underneath because the fact itself still
 * belongs to `dhcp` — §4.1 forbids `devices` keeping its own copy — and because
 * the Addresses screen still needs a way to add a reservation for a device that
 * has never appeared on the network. What changed is which door an operator
 * usually comes through, not who owns the room.
 */
export function ReservationDialog({
  open,
  onOpenChange,
  initial,
  onSubmit,
  onRemove,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  initial?: Reservation
  onSubmit: (reservation: Reservation) => void
  /** Only supplied when editing. Removing is an edit to the thing you opened,
      which is why it lives here rather than as a second button on the row. */
  onRemove?: () => void
}) {
  const [draft, setDraft] = useState<Reservation>(initial ?? EMPTY)
  const editing = initial !== undefined

  function field<K extends keyof Reservation>(key: K, value: Reservation[K]) {
    setDraft((d) => ({ ...d, [key]: value }))
  }

  const complete = draft.mac.trim() && draft.ip.trim()

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next)
        if (next) setDraft(initial ?? EMPTY)
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{editing ? 'Edit reserved address' : 'Reserve an address'}</DialogTitle>
          <DialogDescription>
            Always give this device the same address. Devices stay connected
            while this is added.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4">
          <div className="grid gap-2">
            <Label htmlFor="res-mac">MAC address</Label>
            <Input
              id="res-mac"
              placeholder="aa:bb:cc:dd:ee:ff"
              value={draft.mac}
              disabled={editing}
              onChange={(e) => field('mac', e.target.value)}
            />
          </div>

          <div className="grid gap-2">
            <Label htmlFor="res-ip">Address</Label>
            <Input
              id="res-ip"
              placeholder="192.168.1.50"
              value={draft.ip}
              onChange={(e) => field('ip', e.target.value)}
            />
          </div>

          <div className="grid gap-2">
            <Label htmlFor="res-hostname">Hostname</Label>
            <Input
              id="res-hostname"
              placeholder="optional"
              value={draft.hostname ?? ''}
              onChange={(e) => field('hostname', e.target.value || undefined)}
            />
          </div>
        </div>

        <DialogFooter>
          {onRemove && (
            <Button
              variant="destructive"
              className="mr-auto"
              onClick={() => {
                onRemove()
                onOpenChange(false)
              }}
            >
              <Trash2 className="size-4" aria-hidden /> Remove
            </Button>
          )}
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            disabled={!complete}
            onClick={() => {
              onSubmit({
                ...draft,
                mac: draft.mac.trim(),
                ip: draft.ip.trim(),
                hostname: draft.hostname?.trim() || undefined,
              })
              onOpenChange(false)
            }}
          >
            {editing ? 'Save' : 'Add'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
