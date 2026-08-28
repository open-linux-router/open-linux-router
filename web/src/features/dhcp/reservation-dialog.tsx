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
 * Worth knowing while reading this: design.md §11.1 says a fixed address is a
 * property *of a device*, and §10 open decision 6 — who owns the device
 * inventory — is still open and explicitly blocks that surface. So this form
 * edits a flat MAC-to-IP entry, which the design names as the shape to grow out
 * of. It will be replaced by a device-centric one, not extended.
 */
export function ReservationDialog({
  open,
  onOpenChange,
  initial,
  onSubmit,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  initial?: Reservation
  onSubmit: (reservation: Reservation) => void
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
          <DialogTitle>{editing ? 'Edit fixed address' : 'Add fixed address'}</DialogTitle>
          <DialogDescription>
            Always give this device the same address. Adding one is a reload, so
            connected clients are not interrupted.
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
