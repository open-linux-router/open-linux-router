import { Tag, Trash2 } from 'lucide-react'
import { useState } from 'react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Disclosure } from '@/components/ui/disclosure'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { CategoryPicker } from '@/features/devices/category-picker'
import { DeviceIcon } from '@/features/devices/device-icon'
import { categoryLabel } from '@/features/devices/icons'
import type { DeviceRow } from '@/lib/api-types'
import type { Device, DeviceCategory } from '@/lib/config-types'

/**
 * One device, and everything a human is allowed to say about it.
 *
 * The dialog holds the two halves of §4.4 apart on screen, because conflating
 * them is how an inventory starts lying: the top is *identity*, which is stored
 * and which the operator owns, and the bottom is *presence*, which is observed,
 * stamped, and read-only. Nothing observed is editable here and nothing stored
 * is presented as a fact about the network.
 */
export function DeviceDetail({
  device,
  open,
  onOpenChange,
  onSave,
  onForget,
  onEditFixedAddress,
  busy,
}: {
  device: DeviceRow
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (identity: Device) => void
  /** Only offered for a device that has stored identity to drop. */
  onForget?: () => void
  onEditFixedAddress: () => void
  busy?: boolean
}) {
  const [name, setName] = useState(device.name_origin === 'operator' ? device.name : '')
  const [category, setCategory] = useState<DeviceCategory>(
    device.category_origin === 'operator' ? device.category : '',
  )
  const [notes, setNotes] = useState(device.notes ?? '')

  function reset() {
    setName(device.name_origin === 'operator' ? device.name : '')
    setCategory(device.category_origin === 'operator' ? device.category : '')
    setNotes(device.notes ?? '')
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next)
        if (next) reset()
      }}
    >
      <DialogContent className="max-h-[90svh] overflow-y-auto">
        <DialogHeader>
          <div className="flex items-center gap-3">
            <DeviceIcon category={device.category} online={device.online} size="lg" />
            <div className="min-w-0">
              <DialogTitle className="truncate">{device.name || device.mac}</DialogTitle>
              <DialogDescription className="truncate font-mono text-xs">
                {device.mac}
              </DialogDescription>
              <PresenceBadge device={device} />
            </div>
          </div>
        </DialogHeader>

        <div className="grid gap-5">
          <div className="grid gap-2">
            <Label htmlFor="device-name">Name</Label>
            <Input
              id="device-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={
                // The hostname is shown as the placeholder rather than filled
                // in, so it is visibly the client's own claim and not something
                // the operator has agreed to.
                device.hostname ? `${device.hostname} (what it calls itself)` : device.mac
              }
            />
          </div>

          <div className="grid gap-2">
            <Label>Category</Label>
            <CategoryPicker
              value={category}
              detected={device.detected_category}
              onChange={setCategory}
            />
          </div>

          <div className="grid gap-2">
            <Label htmlFor="device-notes">Notes</Label>
            <Textarea
              id="device-notes"
              rows={2}
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
              placeholder="Where it lives, whose it is — anything you want to remember"
            />
          </div>

          <FixedAddress device={device} onEdit={onEditFixedAddress} />

          <Observed device={device} />
        </div>

        <DialogFooter>
          {onForget && (
            <Button
              variant="destructive"
              className="mr-auto"
              disabled={busy}
              onClick={() => {
                onForget()
                onOpenChange(false)
              }}
            >
              <Trash2 className="size-4" aria-hidden /> Forget
            </Button>
          )}
          <Button variant="ghost" disabled={busy} onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            disabled={busy}
            onClick={() => {
              onSave({
                mac: device.mac,
                name: name.trim() || undefined,
                category: category || undefined,
                notes: notes.trim() || undefined,
                // Preserved rather than edited: there is no tier-2 image set to
                // choose from yet, and silently dropping the field on every
                // save would quietly erase an operator's earlier answer.
                model: device.model || undefined,
              })
              onOpenChange(false)
            }}
          >
            Save
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function PresenceBadge({ device }: { device: DeviceRow }) {
  if (device.online) {
    return (
      <Badge variant="success" className="mt-1">
        Online
      </Badge>
    )
  }
  // Never seen is not the same as away, and the difference is usually a typo in
  // a hand-entered MAC.
  if (!device.seen) {
    return (
      <Badge variant="outline" className="mt-1">
        Never seen
      </Badge>
    )
  }
  return (
    <Badge variant="secondary" className="mt-1">
      Offline
    </Badge>
  )
}

/**
 * The fixed address, owned by dhcp.
 *
 * §11.1: a fixed address is a property *of a device*, set from the device list
 * rather than by typing a MAC into a form. This is that surface. The fact still
 * lives in the dhcp module — the button hands off to dhcp's own editor, so the
 * change goes through dhcp's plan and its impact gate rather than sneaking a
 * network change through a screen that otherwise cannot cause one.
 */
function FixedAddress({ device, onEdit }: { device: DeviceRow; onEdit: () => void }) {
  return (
    <div className="rounded-lg border p-3">
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-1.5 text-sm font-medium">
            <Tag className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
            Fixed address
          </div>
          <div className="mt-0.5 truncate text-[0.8rem] text-muted-foreground">
            {device.fixed_ip ? (
              <span className="font-mono">{device.fixed_ip}</span>
            ) : (
              'This device gets whichever address is free.'
            )}
          </div>
        </div>
        <Button variant="outline" size="sm" onClick={onEdit}>
          {device.fixed_ip ? 'Change' : 'Reserve'}
        </Button>
      </div>
    </div>
  )
}

/** Everything read off the network. Read-only, and stamped by the caller. */
function Observed({ device }: { device: DeviceRow }) {
  const rows: [string, React.ReactNode][] = []

  if (device.ips?.length) {
    rows.push([
      device.ips.length > 1 ? 'Addresses' : 'Address',
      <span key="ips" className="font-mono">
        {device.ips.join(', ')}
      </span>,
    ])
  }
  if (device.hostname) rows.push(['Calls itself', device.hostname])
  if (device.vendor) rows.push(['Vendor', device.vendor])
  if (device.sources?.length) {
    rows.push(['Seen by', device.sources.map(sourceLabel).join(', ')])
  }
  if (device.expires) {
    rows.push(['Lease expires', new Date(device.expires).toLocaleString()])
  }
  if (device.detected_category) {
    rows.push([
      'Detected as',
      <span key="detected">
        {categoryLabel(device.detected_category)}
        {device.detect_reason && (
          <span className="text-muted-foreground"> — {device.detect_reason}</span>
        )}
      </span>,
    ])
  }

  if (rows.length === 0) {
    return (
      <p className="text-[0.8rem] text-muted-foreground">
        Nothing has been observed about this device yet.
      </p>
    )
  }

  return (
    <Disclosure summary="What the network says">
      <dl className="grid gap-1.5 pt-1 text-[0.8rem]">
        {rows.map(([label, value], i) => (
          <div key={i} className="flex gap-3">
            <dt className="w-28 shrink-0 text-muted-foreground">{label}</dt>
            <dd className="min-w-0 break-words">{value}</dd>
          </div>
        ))}
      </dl>
    </Disclosure>
  )
}

function sourceLabel(source: string): string {
  switch (source) {
    case 'dhcp-lease':
      return 'DHCP lease'
    case 'arp':
      return 'network traffic'
    default:
      return source
  }
}
