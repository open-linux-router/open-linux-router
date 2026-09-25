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
import type { NetworkRow, InterfaceRow, Uplink, UplinkStatus } from '@/lib/api-types'

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
 *
 * An interface that carries a network is offered too, as a takeover. It used to
 * be listed as blocked, which was accurate and a dead end: the usual reason a
 * network sits on the modem-facing NIC is that it was the only place olr would
 * take an address before the uplink existed, and the way out — remove the
 * network, then set the uplink — took the address away in between, from an
 * operator who was very likely connected through it. Choosing it here removes
 * the network with its address kept (link's keep_addresses), and the uplink
 * claims that same address.
 */
export function UplinkDialog({
  open,
  onOpenChange,
  initial,
  interfaces,
  networks,
  onSubmit,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Undefined when there is no uplink yet. */
  initial?: UplinkStatus
  interfaces: InterfaceRow[]
  networks: NetworkRow[]
  /** `replacing` names the network the chosen interface carries, which goes. */
  onSubmit: (uplink: Uplink, replacing?: string) => void
}) {
  const [iface, setIface] = useState(initial?.interface ?? '')
  const [address, setAddress] = useState(initial?.address ?? '')
  const [gateway, setGateway] = useState(initial?.gateway ?? '')
  const [dns, setDns] = useState((initial?.dns ?? []).join(', '))

  // Only adopted interfaces (design.md §3.4).
  const available = interfaces.filter((i) => i.adopted && !i.loopback)
  const replacing = networks.find((n) => n.name === available.find((i) => i.name === iface)?.network)
  const kept = replacing ? routerPrefix(replacing) : undefined

  function choose(name: string) {
    setIface(name)
    // The network's own address is what a takeover keeps, so it is the one to
    // offer — over a blank field, or over the previous uplink's address when
    // the uplink is moving, since that one is about to come off. Anything the
    // operator typed stays.
    const network = networks.find((n) => n.name === interfaces.find((i) => i.name === name)?.network)
    const prefix = network ? routerPrefix(network) : undefined
    if (prefix && (address.trim() === '' || address === initial?.address)) setAddress(prefix)
  }

  const valid = iface !== '' && (address.trim() === '') === (gateway.trim() === '')

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{initial ? 'Change the uplink' : 'Set up the uplink'}</DialogTitle>
          <DialogDescription>
            Which interface faces your modem, and how to reach the internet through it. From
            here on olr owns that interface&rsquo;s IPv4 and this router&rsquo;s default route
            — the distribution stops configuring them — and puts both back after a reboot.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="uplink-interface">Interface</Label>
            <Select value={iface} onValueChange={(v) => choose(v ?? '')}>
              <SelectTrigger id="uplink-interface" className="w-full">
                <SelectValue placeholder="Choose the interface facing your modem" />
              </SelectTrigger>
              <SelectContent>
                {available.map((i) => (
                  <SelectItem key={i.name} value={i.name}>
                    {i.name}
                    {i.network
                      ? ` — carries the network ${i.network}`
                      : i.address
                        ? ` — currently ${i.address}`
                        : ' — no address'}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {available.length === 0 && (
              <p className="text-xs text-warning">
                No interface has been handed to this router yet. Switch on the one facing your
                modem under Interfaces above.
              </p>
            )}
            {replacing && (
              <p className="text-xs text-warning">
                {iface} carries the network {replacing.name}. A network is something this router
                serves, and the way out is not one, so setting the uplink here removes{' '}
                {replacing.name}: this router stops handing out addresses and answering DNS on
                it. {kept ? `${kept} stays on ${iface}, so a connection through it stays up.` : ''}
              </p>
            )}
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="uplink-address">This router&rsquo;s address</Label>
              <Input
                id="uplink-address"
                value={address}
                placeholder="e.g. 192.168.2.9/24"
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
                placeholder="e.g. 192.168.2.1"
                onChange={(e) => setGateway(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                Where everything else goes — your modem&rsquo;s address on that link.
              </p>
            </div>
          </div>

          <div className="space-y-2">
            <Label htmlFor="uplink-dns">Resolvers this router uses</Label>
            <Input
              id="uplink-dns"
              value={dns}
              placeholder={`e.g. ${gateway.trim() || '192.168.2.1'}`}
              onChange={(e) => setDns(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              Where this router itself looks names up — usually your modem, the gateway above.
              olr writes them in place of whatever the distribution set. Leave blank and the
              router&rsquo;s own resolver setup is left as it is.
            </p>
            {/* The one-click answer for the common case, offered rather than
                filled in: a gateway that does not answer DNS is not rare (an
                ISP's own equipment often does not), and a resolver nobody
                chose would be the harder failure to explain. */}
            {dns.trim() === '' && gateway.trim() !== '' && address.trim() !== '' && (
              <button
                type="button"
                className="text-xs underline underline-offset-4"
                onClick={() => setDns(gateway.trim())}
              >
                Use {gateway.trim()}
              </button>
            )}
          </div>

          {!valid && iface !== '' && (
            <p className="text-xs text-warning">
              An address needs a gateway and a gateway needs an address. Leave both blank for an
              interface olr should own and not configure.
            </p>
          )}
          {valid && address.trim() === '' && (
            // Said out loud because the blank form is valid and easy to send by
            // accident: it brings the interface up and nothing else, which from
            // the page looks like a click that did nothing.
            <p className="text-xs text-muted-foreground">
              No address or gateway: olr brings {iface} up and writes no address or route on it.
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
              onSubmit(
                {
                  interface: iface,
                  ipv4: address.trim()
                    ? { address: address.trim(), gateway: gateway.trim() }
                    : undefined,
                  dns: splitAddresses(dns),
                },
                replacing?.name,
              )
            }
          >
            {initial ? 'Save' : 'Set up'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** A network's router address with the subnet's mask — 192.168.1.2/24 — or
 * undefined for a network that serves no IPv4. */
function routerPrefix(n: NetworkRow): string | undefined {
  if (!n.router || !n.subnet) return undefined
  return `${n.router}/${n.subnet.split('/')[1]}`
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
