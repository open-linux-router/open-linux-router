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
import { Switch } from '@/components/ui/switch'
import type { Exit, ExitForm } from '@/lib/config-types'

const EMPTY: Exit = { name: '', slot: 0, via: { kind: 'next_hop' } }

/**
 * What each form is, said the way somebody choosing between them needs to hear
 * it (docs/gateway.md §1.2). The schema words are `interface`, `next_hop` and
 * `blocked`; none of them is what an operator would say out loud.
 */
const FORMS: { value: ExitForm; label: string; hint: string }[] = [
  {
    value: 'next_hop',
    label: 'Another box on your network',
    hint: 'Traffic is handed to a machine you already have — the modem, or a box running a proxy.',
  },
  {
    value: 'interface',
    label: 'A connection on this router',
    hint: 'Traffic leaves through a device on this box, such as a VPN tunnel or a proxy.',
  },
  {
    value: 'blocked',
    label: 'Nowhere — block it',
    hint: 'Traffic is refused. Apps fail straight away and say so, instead of hanging.',
  },
]

/** The same words the options carry, so the trigger and the list agree. */
const FAILURE_LABEL: Record<string, string> = {
  '': 'Stop the traffic',
  block: 'Stop the traffic',
  direct: 'Send it out normally instead',
}

const IPV6_LABEL: Record<string, string> = {
  '': 'Block it',
  block: 'Block it',
  via: 'Send it through this way out too',
  direct: 'Let it go out normally',
}

/**
 * Adds or edits an exit.
 *
 * The word "exit" is the schema's, and the operator meets it only as the thing
 * on the other side of *Internet via …* (§1.3). What this form asks for is a
 * name and a destination; everything that can be defaulted safely is, and the
 * two answers that cannot — what happens to IPv6, and what happens when it stops
 * working — are behind a disclosure with their consequences spelled out rather
 * than left as jargon.
 */
export function ExitDialog({
  open,
  onOpenChange,
  initial,
  onSubmit,
  onRemove,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  initial?: Exit
  onSubmit: (exit: Exit) => void
  /** Only supplied when editing. Removing is an edit to the thing you opened. */
  onRemove?: () => void
}) {
  const [draft, setDraft] = useState<Exit>(initial ?? EMPTY)
  const editing = initial !== undefined

  function field<K extends keyof Exit>(key: K, value: Exit[K]) {
    setDraft((d) => ({ ...d, [key]: value }))
  }

  function setForm(kind: ExitForm) {
    // Switching form clears the other form's fields rather than leaving them
    // behind, where they would read as still being in force.
    setDraft((d) => ({ ...d, via: { kind } }))
  }

  const complete =
    draft.name.trim() &&
    (draft.via.kind === 'blocked' ||
      (draft.via.kind === 'interface' && draft.via.interface?.trim()) ||
      (draft.via.kind === 'next_hop' && draft.via.next_hop?.trim()))

  const form = FORMS.find((f) => f.value === draft.via.kind) ?? FORMS[0]

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) setDraft(initial ?? EMPTY)
        onOpenChange(next)
      }}
    >
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{editing ? `Edit ${initial.name}` : 'Add a way out'}</DialogTitle>
          <DialogDescription>
            A way out is somewhere this router can hand your traffic to. Once it exists you can
            send any network through it.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="exit-name">Name</Label>
            <Input
              id="exit-name"
              value={draft.name}
              placeholder="Clash"
              onChange={(e) => field('name', e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              What you will call it on the network list. Anything you like.
            </p>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="exit-form">Traffic goes to</Label>
            <Select value={draft.via.kind} onValueChange={(v) => setForm(v as ExitForm)}>
              <SelectTrigger id="exit-form">
                {/* Labelled, because the trigger otherwise shows the schema
                    word — "next_hop" — which is the one thing this form exists
                    to keep out of an operator's way. */}
                <SelectValue>
                  {(v: string) => FORMS.find((f) => f.value === v)?.label ?? v}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {FORMS.map((f) => (
                  <SelectItem key={f.value} value={f.value}>
                    {f.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">{form.hint}</p>
          </div>

          {draft.via.kind === 'next_hop' && (
            <div className="space-y-1.5">
              <Label htmlFor="exit-hop">Its address</Label>
              <Input
                id="exit-hop"
                value={draft.via.next_hop ?? ''}
                placeholder="192.168.1.50"
                onChange={(e) => field('via', { ...draft.via, next_hop: e.target.value })}
              />
              <p className="text-xs text-muted-foreground">
                Its address on <em>your</em> network, not a public one — this router has to be able
                to reach it directly.
              </p>
            </div>
          )}

          {draft.via.kind === 'interface' && (
            <div className="space-y-1.5">
              <Label htmlFor="exit-iface">Which connection</Label>
              <Input
                id="exit-iface"
                value={draft.via.interface ?? ''}
                placeholder="wg0"
                onChange={(e) => field('via', { ...draft.via, interface: e.target.value })}
              />
              <p className="text-xs text-muted-foreground">
                The name of the device, as the system knows it. It does not have to exist yet.
              </p>
            </div>
          )}

          {draft.via.kind !== 'blocked' && (
            <Disclosure summary="Health check, IPv6 and other details">
              <div className="space-y-4 pt-1">
                <div className="space-y-1.5">
                  <Label htmlFor="exit-probe">Check it is working by connecting to</Label>
                  <Input
                    id="exit-probe"
                    value={draft.probe?.target ?? ''}
                    placeholder="1.1.1.1:443"
                    onChange={(e) =>
                      field(
                        'probe',
                        e.target.value.trim() ? { ...draft.probe, target: e.target.value } : undefined,
                      )
                    }
                  />
                  <p className="text-xs text-muted-foreground">
                    Something on the far side, so the check fails when traffic stops passing
                    through. Without it, a box that stops forwarding but stays switched on looks
                    healthy and the devices behind it are simply offline.
                  </p>
                </div>

                <div className="space-y-1.5">
                  <Label htmlFor="exit-failure">If it stops working</Label>
                  <Select
                    value={draft.on_failure || 'block'}
                    onValueChange={(v) => field('on_failure', v as Exit['on_failure'])}
                  >
                    <SelectTrigger id="exit-failure">
                      <SelectValue>
                        {(v: string) => FAILURE_LABEL[v] ?? FAILURE_LABEL.block}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="block">Stop the traffic</SelectItem>
                      <SelectItem value="direct">Send it out normally instead</SelectItem>
                    </SelectContent>
                  </Select>
                  <p className="text-xs text-muted-foreground">
                    Stopping is the safe answer: the network list will say why. Sending it out
                    normally keeps devices online but sends exactly the traffic you wanted routed
                    out unrouted, without telling anyone.
                  </p>
                </div>

                <div className="space-y-1.5">
                  <Label htmlFor="exit-ipv6">IPv6</Label>
                  <Select
                    value={draft.ipv6 || 'block'}
                    onValueChange={(v) => field('ipv6', v as Exit['ipv6'])}
                  >
                    <SelectTrigger id="exit-ipv6">
                      <SelectValue>{(v: string) => IPV6_LABEL[v] ?? IPV6_LABEL.block}</SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="block">Block it</SelectItem>
                      <SelectItem value="via">Send it through this way out too</SelectItem>
                      <SelectItem value="direct">Let it go out normally</SelectItem>
                    </SelectContent>
                  </Select>
                  <p className="text-xs text-muted-foreground">
                    Blocking is the safe answer, and clients fall back to IPv4 immediately. Letting
                    it go out normally means every site with an IPv6 address quietly bypasses this
                    way out at full speed.
                  </p>
                </div>

                {draft.via.kind === 'next_hop' && (
                  <div className="flex items-start justify-between gap-4">
                    <div className="space-y-1">
                      <Label htmlFor="exit-snat">Hide device addresses from it</Label>
                      <p className="text-xs text-muted-foreground">
                        Makes replies come back through this router, so traffic is counted
                        correctly. Turn it off if the other box needs to see which device a
                        connection came from — it will need a network of its own.
                      </p>
                    </div>
                    <Switch
                      id="exit-snat"
                      checked={draft.snat ?? true}
                      onCheckedChange={(v) => field('snat', v)}
                    />
                  </div>
                )}
              </div>
            </Disclosure>
          )}
        </div>

        <DialogFooter className="sm:justify-between">
          {onRemove ? (
            <Button variant="ghost" className="text-destructive" onClick={onRemove}>
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
            <Button
              disabled={!complete}
              onClick={() => {
                onSubmit({ ...draft, name: draft.name.trim() })
                onOpenChange(false)
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
