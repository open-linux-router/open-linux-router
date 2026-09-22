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
import type { InterfaceRow, Uplink, UplinkStatus } from '@/lib/api-types'

/**
 * Set how this router reaches the internet.
 *
 * Three fields, and the order is the order somebody thinks: which cable goes to
 * the modem, what address this box has on that link, and where to send
 * everything else.
 *
 * The address keeps its host bits — 192.168.2.9/24, not 192.168.2.0/24 — which
 * is the opposite of the network form directly above this one on the page, and
 * the mistake worth catching in the field rather than in a refusal. The server
 * refuses the network address too, but by then the operator has had to read an
 * error to learn something the label could have told them.
 */
export function UplinkDialog({
  open,
  onOpenChange,
  initial,
  interfaces,
  onSubmit,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Undefined when there is no uplink yet. */
  initial?: UplinkStatus
  interfaces: InterfaceRow[]
  onSubmit: (uplink: Uplink) => void
}) {
  const [iface, setIface] = useState(initial?.interface ?? '')
  const [address, setAddress] = useState(initial?.address ?? '')
  const [gateway, setGateway] = useState(initial?.gateway ?? '')
  const [dns, setDns] = useState((initial?.dns ?? []).join(', '))

  // Only adopted interfaces (design.md §3.4), and never one already carrying a
  // network: an uplink and a network cannot both own one interface's
  // addressing. The server refuses it; offering it here and then refusing would
  // be a list that lies.
  const available = interfaces.filter(
    (i) => i.adopted && !i.loopback && (!i.group || i.name === initial?.interface),
  )
  const blocked = interfaces.filter((i) => i.adopted && !i.loopback && i.group)

  const valid = iface !== '' && (address.trim() === '') === (gateway.trim() === '')

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{initial ? 'Change the uplink' : 'Set up the uplink'}</DialogTitle>
          <DialogDescription>
            Which interface faces your modem, and how to reach the internet through it. From
            here on olr owns that interface&rsquo;s address and this router&rsquo;s default
            route, and puts both back after a reboot.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="uplink-interface">Interface</Label>
            <Select value={iface} onValueChange={(v) => setIface(v ?? '')}>
              <SelectTrigger id="uplink-interface" className="w-full">
                <SelectValue placeholder="Choose the interface facing your modem" />
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
                No interface is free. Switch one on under Interfaces above, or remove the
                network from the one facing your modem.
              </p>
            )}
            {blocked.length > 0 && (
              <p className="text-xs text-muted-foreground">
                {blocked.map((i) => i.name).join(', ')}{' '}
                {blocked.length === 1 ? 'carries a network' : 'carry networks'} and cannot be the
                uplink. A network is something this router serves — it hands out addresses there
                — and the way out is not one.
              </p>
            )}
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="uplink-address">This router&rsquo;s address</Label>
              <Input
                id="uplink-address"
                value={address}
                placeholder="192.168.2.9/24"
                onChange={(e) => setAddress(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                The address on the link to your modem, with its mask. This is an address, not a
                subnet: 192.168.2.9/24, not 192.168.2.0/24.
              </p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="uplink-gateway">Gateway</Label>
              <Input
                id="uplink-gateway"
                value={gateway}
                placeholder="192.168.2.1"
                onChange={(e) => setGateway(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                Where everything else goes — your modem&rsquo;s address on that link.
              </p>
            </div>
          </div>

          <div className="space-y-2">
            <Label htmlFor="uplink-dns">Your ISP&rsquo;s resolvers (optional)</Label>
            <Input
              id="uplink-dns"
              value={dns}
              placeholder="e.g. 9.9.9.9, 1.1.1.1"
              onChange={(e) => setDns(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              Recorded only — nothing reads them yet. This router resolves names from the root
              by default, which needs no upstream at all.
            </p>
          </div>

          {!valid && iface !== '' && (
            <p className="text-xs text-warning">
              An address needs a gateway and a gateway needs an address. Leave both blank for an
              interface olr should own and not configure.
            </p>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            disabled={!valid}
            onClick={() =>
              onSubmit({
                interface: iface,
                ipv4: address.trim()
                  ? { address: address.trim(), gateway: gateway.trim() }
                  : undefined,
                dns: splitAddresses(dns),
              })
            }
          >
            {initial ? 'Save' : 'Set up'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** Splits a typed list on commas or spaces. Undefined when nothing was typed,
 * so an untouched field stores no key at all. */
function splitAddresses(value: string): string[] | undefined {
  const parts = value
    .split(/[,\s]+/)
    .map((s) => s.trim())
    .filter(Boolean)
  return parts.length ? parts : undefined
}
