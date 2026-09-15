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
import type { InterfaceRow } from '@/lib/api-types'
import type { Group } from '@/lib/config-types'

const EMPTY: Group = { name: '', members: [], ipv4: { subnet: '' } }

/**
 * Create or edit a network.
 *
 * The form asks for a subnet rather than checking one, which is the whole point
 * of the change it belongs to. The old address-range dialog took an interface,
 * read the address that interface happened to be holding, and refused any range
 * outside it — so an operator who wanted a different subnet met an error with
 * no page behind it. This is that page.
 *
 * The router address is a placeholder rather than a value: leaving it blank
 * stores nothing and derives the first host address, so changing the subnet
 * later moves the router with it instead of leaving a stale pin behind.
 */
export function NetworkDialog({
  open,
  onOpenChange,
  initial,
  interfaces,
  taken,
  onSubmit,
  onRemove,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Undefined when adding. */
  initial?: Group
  interfaces: InterfaceRow[]
  /** Interfaces already carrying another network. */
  taken: Set<string>
  onSubmit: (group: Group) => void
  onRemove?: () => void
}) {
  const [draft, setDraft] = useState<Group>(initial ?? EMPTY)
  const editing = initial !== undefined
  const member = draft.members[0] ?? ''
  const chosen = interfaces.find((i) => i.name === member)

  // Only adopted interfaces can carry a network (design.md §3.4), and one
  // interface carries one network — so the list offers what is actually
  // available rather than everything and an error afterwards.
  const available = interfaces.filter(
    (i) => i.adopted && !i.loopback && (!taken.has(i.name) || i.name === member),
  )

  const derivedRouter = deriveRouter(draft.ipv4?.subnet ?? '')
  const valid = draft.name.trim() !== '' && member !== ''

  function set(patch: Partial<Group>) {
    setDraft((d) => ({ ...d, ...patch }))
  }
  function setIPv4(patch: Partial<NonNullable<Group['ipv4']>>) {
    setDraft((d) => ({ ...d, ipv4: { subnet: '', ...d.ipv4, ...patch } }))
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{initial ? `Edit ${initial.name}` : 'Add network'}</DialogTitle>
          <DialogDescription>
            A subnet and the interface it lives on. This router takes an address on it, and
            everything else — address ranges, how it reaches the internet — is configured
            against this network rather than against the interface.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="net-name">Name</Label>
              <Input
                id="net-name"
                value={draft.name}
                placeholder="lan"
                // The name is the key every other module stores, so renaming is
                // not an edit — it is a delete and a create, and would silently
                // orphan any range pointing at the old one.
                disabled={editing}
                onChange={(e) => set({ name: e.target.value })}
              />
              <p className="text-xs text-muted-foreground">
                {editing
                  ? 'The name is how ranges and routing refer to this network, so it cannot be changed here.'
                  : 'Lowercase letters, digits and hyphens. This is what you will pick in other pages.'}
              </p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="net-member">Interface</Label>
              <Select value={member} onValueChange={(v) => set({ members: v ? [v] : [] })}>
                <SelectTrigger id="net-member" className="w-full">
                  <SelectValue placeholder="Choose an interface" />
                </SelectTrigger>
                <SelectContent>
                  {available.map((i) => (
                    <SelectItem key={i.name} value={i.name}>
                      {i.name}
                      {i.address ? ` — currently ${i.address}` : ' — no address'}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {available.length === 0 && (
                <p className="text-xs text-warning">
                  No interface is free. Adopt one under DHCP → Interfaces, or remove the
                  network already on it.
                </p>
              )}
            </div>
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="net-subnet">Subnet</Label>
              <Input
                id="net-subnet"
                value={draft.ipv4?.subnet ?? ''}
                placeholder="172.16.1.0/24"
                onChange={(e) => setIPv4({ subnet: e.target.value })}
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="net-router">This router's address</Label>
              <Input
                id="net-router"
                value={draft.ipv4?.router ?? ''}
                placeholder={derivedRouter || 'the first address'}
                onChange={(e) => setIPv4({ router: e.target.value || undefined })}
              />
            </div>
          </div>

          {chosen?.address && chosen.address !== (draft.ipv4?.router || derivedRouter) && (
            // The one thing an operator must not discover after the fact. §5.5's
            // guard is not built, so this warning and the confirmation dialog
            // are the whole safety net.
            <p className="text-xs text-warning">
              {chosen.name} currently has {chosen.address}. Applying this replaces it — if you
              are reaching this page over {chosen.name}, the connection will drop and you will
              need to come back on the new address.
            </p>
          )}
        </div>

        <DialogFooter className="sm:justify-between">
          {onRemove ? (
            <Button variant="ghost" className="text-destructive" onClick={onRemove}>
              <Trash2 className="size-4" aria-hidden /> Remove
            </Button>
          ) : (
            <span />
          )}
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button
              disabled={!valid}
              onClick={() => {
                const ipv4 = draft.ipv4?.subnet?.trim() ? draft.ipv4 : undefined
                onSubmit({ ...draft, name: draft.name.trim(), ipv4 })
              }}
            >
              {editing ? 'Save' : 'Add'}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/**
 * The first host address of a subnet, for the placeholder.
 *
 * Deliberately naive and IPv4-only: it exists to show `.1` under an ordinary
 * prefix while somebody types, and the server derives the real value. A
 * placeholder that is wrong on a /25 shows nothing rather than something
 * misleading, because it is never submitted.
 */
function deriveRouter(subnet: string): string {
  const [addr, bits] = subnet.split('/')
  const octets = addr?.split('.') ?? []
  if (octets.length !== 4 || Number(bits) !== 24) return ''
  if (octets.some((o) => o === '' || Number.isNaN(Number(o)))) return ''
  return `${octets[0]}.${octets[1]}.${octets[2]}.1`
}
