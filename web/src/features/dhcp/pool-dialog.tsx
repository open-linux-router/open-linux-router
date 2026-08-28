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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { Pool, RouterAdvertisementMode } from '@/lib/config-types'

// The RA vocabulary comes from the published schema (RAMode.JSONSchema in Go),
// so these labels are the only thing added here. The values themselves are not
// restated — see config-types.ts.
//
// Split into a short label and a hint because the label has to survive in the
// closed trigger, where "SLAAC — advertise the prefix (recommended)" was
// truncated into uselessness. The protocol name is not what an operator is
// choosing between; what the devices end up doing is.
const RA_OPTIONS: { value: Exclude<RouterAdvertisementMode, ''>; label: string; hint: string }[] = [
  { value: 'off', label: 'Off', hint: 'No IPv6 on this network.' },
  {
    value: 'slaac',
    label: 'Automatic',
    hint: 'Devices choose their own IPv6 address. Recommended.',
  },
  { value: 'stateful', label: 'Managed', hint: 'The router hands out IPv6 addresses too.' },
]

const RA_LABEL = new Map(RA_OPTIONS.map((o) => [o.value as string, o.label]))

const EMPTY: Pool = { interface: '', start: '', end: '' }

export function PoolDialog({
  open,
  onOpenChange,
  initial,
  onSubmit,
  onRemove,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Undefined when adding. */
  initial?: Pool
  onSubmit: (pool: Pool) => void
  /** Only supplied when editing. Removing is an edit to the thing you opened,
      which is why it lives here rather than as a second button on the row. */
  onRemove?: () => void
}) {
  const [draft, setDraft] = useState<Pool>(initial ?? EMPTY)
  const editing = initial !== undefined

  function field<K extends keyof Pool>(key: K, value: Pool[K]) {
    setDraft((d) => ({ ...d, [key]: value }))
  }

  // Only the fields the server requires are checked here. Everything else —
  // whether the range sits inside the interface's subnet, whether a reservation
  // collides — is the server's job (design.md §5.3.1) and is deliberately not
  // duplicated: a second validator in the browser would be a second source of
  // truth that disagrees with the first one the day either changes.
  const complete = draft.interface.trim() && draft.start.trim() && draft.end.trim()

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next)
        if (next) setDraft(initial ?? EMPTY)
      }}
    >
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{editing ? 'Edit address range' : 'Add address range'}</DialogTitle>
          <DialogDescription>
            An address range served on one interface. The range must fall inside
            a subnet already configured on that interface.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4">
          <div className="grid gap-2">
            <Label htmlFor="pool-interface">Interface</Label>
            <Input
              id="pool-interface"
              placeholder="lan0"
              value={draft.interface}
              disabled={editing}
              onChange={(e) => field('interface', e.target.value)}
            />
            {editing && (
              <p className="text-xs text-muted-foreground">
                The interface identifies this range and cannot be changed.
                Remove and re-add to move it.
              </p>
            )}
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="grid gap-2">
              <Label htmlFor="pool-start">First address</Label>
              <Input
                id="pool-start"
                placeholder="192.168.1.100"
                value={draft.start}
                onChange={(e) => field('start', e.target.value)}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="pool-end">Last address</Label>
              <Input
                id="pool-end"
                placeholder="192.168.1.200"
                value={draft.end}
                onChange={(e) => field('end', e.target.value)}
              />
            </div>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="grid gap-2">
              <Label htmlFor="pool-lease">Lease time</Label>
              <Input
                id="pool-lease"
                placeholder="12h"
                value={draft.lease_time ?? ''}
                onChange={(e) => field('lease_time', e.target.value || undefined)}
              />
              <p className="text-xs text-muted-foreground">
                Blank for the default. Units: s, m, h, d, w.
              </p>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="pool-ra">IPv6</Label>
              <Select
                value={draft.ra ?? 'off'}
                onValueChange={(v) => field('ra', v as RouterAdvertisementMode)}
              >
                <SelectTrigger id="pool-ra" className="w-full">
                  {/* Without the render function this shows the raw schema
                      value — the trigger read "slaac". */}
                  <SelectValue>{(value: string) => RA_LABEL.get(value) ?? 'Off'}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {RA_OPTIONS.map(({ value, label, hint }) => (
                    <SelectItem key={value} value={value}>
                      <span className="flex flex-col gap-0.5">
                        <span>{label}</span>
                        <span className="text-xs text-muted-foreground">{hint}</span>
                      </span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="grid gap-2">
              <Label htmlFor="pool-gateway">Gateway</Label>
              <Input
                id="pool-gateway"
                placeholder="the router itself"
                value={draft.gateway ?? ''}
                onChange={(e) => field('gateway', e.target.value || undefined)}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="pool-domain">Domain</Label>
              <Input
                id="pool-domain"
                placeholder="lan"
                value={draft.domain ?? ''}
                onChange={(e) => field('domain', e.target.value || undefined)}
              />
            </div>
          </div>

          <div className="grid gap-2">
            <Label htmlFor="pool-dns">DNS servers</Label>
            <Input
              id="pool-dns"
              placeholder="the router itself"
              value={(draft.dns ?? []).join(', ')}
              onChange={(e) => field('dns', splitList(e.target.value))}
            />
            <p className="text-xs text-muted-foreground">
              Comma separated. Blank advertises this router.
            </p>
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
              onSubmit(normalise(draft))
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

function splitList(value: string): string[] | undefined {
  const items = value
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
  return items.length ? items : undefined
}

/** Trims, and drops empty optional fields so they are omitted rather than sent blank. */
function normalise(pool: Pool): Pool {
  return {
    ...pool,
    interface: pool.interface.trim(),
    start: pool.start.trim(),
    end: pool.end.trim(),
    lease_time: pool.lease_time?.trim() || undefined,
    gateway: pool.gateway?.trim() || undefined,
    domain: pool.domain?.trim() || undefined,
    ra: pool.ra || undefined,
  }
}
