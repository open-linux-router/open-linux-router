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
import { Disclosure } from '@/components/ui/disclosure'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { RemotePeer } from '@/lib/api-types'

/**
 * Lets a device dial in, or changes one.
 *
 * One field is asked for on the way in — a name — which is the module's whole
 * claim. The key pair, the address on the tunnel and every line of the file the
 * device imports are derived from things settled once when remote access was
 * switched on.
 *
 * The second field is a real choice and is the only one on this screen an
 * operator can get wrong in a way that matters, so it is a pair of described
 * options rather than a toggle. See {@link RouteChoice}.
 */
export function PeerDialog({
  open,
  onOpenChange,
  initial,
  subnet,
  onSubmit,
  onRemove,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  initial?: RemotePeer
  /** The dial-in network, for the preview. */
  subnet?: string
  onSubmit: (peer: { name: string; routes: string; public_key?: string }) => void
  /** Only supplied when editing. Removing is an edit to the thing you opened. */
  onRemove?: () => void
}) {
  const editing = initial !== undefined
  const [name, setName] = useState(initial?.name ?? '')
  const [routes, setRoutes] = useState<string>(initial?.routes ?? 'home')
  const [publicKey, setPublicKey] = useState('')

  const valid = name.trim() !== ''

  function submit() {
    if (!valid) return
    onSubmit({
      name: name.trim(),
      routes,
      public_key: publicKey.trim() || undefined,
    })
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{editing ? name : 'Let a device in'}</DialogTitle>
          <DialogDescription>
            {editing
              ? 'What this device sends through the tunnel. Its address and its key do not change.'
              : 'One device, one entry. A phone and a laptop are two.'}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          {!editing && (
            <div className="space-y-2">
              <Label htmlFor="peer-name">Name</Label>
              <Input
                id="peer-name"
                value={name}
                autoFocus
                placeholder="phone"
                onChange={(e) => setName(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                Letters, digits and hyphens. {subnet ? `It gets an address on ${subnet}.` : ''}
              </p>
            </div>
          )}

          <RouteChoice value={routes} onChange={setRoutes} editing={editing} />

          {!editing && (
            <Disclosure summary="I generated the key on the device myself">
              <div className="space-y-2">
                <Label htmlFor="peer-key">Public key</Label>
                <Input
                  id="peer-key"
                  value={publicKey}
                  placeholder="base64, 44 characters"
                  onChange={(e) => setPublicKey(e.target.value)}
                />
                <p className="text-xs text-muted-foreground">
                  Leave this empty and the router generates the pair, which is the ordinary way and
                  the only one that can hand you a finished file. Fill it in and the router never
                  holds the private half — but you finish the configuration on the device.
                </p>
              </div>
            </Disclosure>
          )}
        </div>

        <DialogFooter className="sm:justify-between">
          {onRemove ? (
            <Button
              variant="ghost"
              className="text-destructive"
              onClick={() => {
                onOpenChange(false)
                onRemove()
              }}
            >
              <Trash2 />
              Remove
            </Button>
          ) : (
            <span />
          )}
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button onClick={submit} disabled={!valid}>
              {editing ? 'Save' : 'Add'}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/**
 * The one choice on this screen, as two described options rather than a switch.
 *
 * A switch labelled "send everything through the tunnel" would be smaller and
 * would hide the consequence, which is not symmetrical: the safe option costs
 * nothing, and the other one routes every byte the device sends through the
 * operator's home upload — and, today, through address translation olr does not
 * yet write, so it may reach no internet at all (docs/remote-access.md §8).
 *
 * The note when editing is the part that cannot be left out. What a device
 * sends is decided by the file *on the device*; the router cannot change it,
 * and does not pretend to. Saying so here, before the operator saves, is better
 * than saying it afterwards in a toast they may have looked away from.
 */
function RouteChoice({
  value,
  onChange,
  editing,
}: {
  value: string
  onChange: (v: string) => void
  editing: boolean
}) {
  return (
    <div className="space-y-2">
      <Label htmlFor="peer-routes">What it sends home</Label>
      {/* The component's value can be null when a selection is cleared, which
          this control never does — it has no empty option. Coalescing keeps the
          default rather than writing an empty string the daemon would read as
          "home" anyway. */}
      <Select value={value} onValueChange={(v) => onChange(v ?? 'home')}>
        <SelectTrigger id="peer-routes" className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="home">Only what is on my network</SelectItem>
          <SelectItem value="everything">Everything the device does</SelectItem>
        </SelectContent>
      </Select>
      <p className="text-xs text-muted-foreground">
        {value === 'everything'
          ? 'The device looks like it is at home for every purpose, including its public address — at the cost of every byte crossing your home upload twice. It also needs address translation this router does not write yet, so the device may connect and reach no internet.'
          : 'The device reaches your NAS, your printer and this router, while everything else keeps going out of whatever network the device is actually on.'}
      </p>
      {editing && (
        <p className="text-xs text-muted-foreground">
          This is decided by the file on the device, so the router cannot change it for you. Saving
          gives you the line to edit there.
        </p>
      )}
    </div>
  )
}
