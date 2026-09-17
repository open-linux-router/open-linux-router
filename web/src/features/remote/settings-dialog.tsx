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
import type { RemoteConfig } from '@/lib/config-types'

/**
 * The tunnel's settings, which are set once and then not thought about.
 *
 * The endpoint is first and is the only required one, because it is the single
 * thing the router cannot work out for itself: the uplink may be behind a
 * carrier NAT, and the name that tracks its address lives at a DNS provider.
 * Everything else here has a default that is right.
 *
 * The dial-in network is below a disclosure and carries a warning, for the
 * reason docs/remote-access.md §3.2 gives: changing it renumbers every device,
 * and a device's address is written into a file that has already left this box.
 * It is the one field on the screen whose damage is to something the router
 * cannot reach.
 */
export function SettingsDialog({
  open,
  onOpenChange,
  initial,
  onSubmit,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  initial?: RemoteConfig['wireguard']
  onSubmit: (fields: Record<string, unknown>) => void
}) {
  const [endpoint, setEndpoint] = useState(initial?.endpoint ?? '')
  const [subnet, setSubnet] = useState(initial?.subnet ?? '')
  const [port, setPort] = useState(String(initial?.listen_port ?? ''))

  function submit() {
    const fields: Record<string, unknown> = { endpoint: endpoint.trim() }
    // Only what changed. A patch that does not mention a key leaves it alone,
    // which is what lets this form round-trip without a way to clear a field by
    // opening the dialog and pressing Save.
    if (subnet.trim() !== (initial?.subnet ?? '')) fields.subnet = subnet.trim()
    const parsed = Number(port)
    if (port.trim() !== '' && Number.isFinite(parsed) && parsed !== initial?.listen_port) {
      fields.listen_port = parsed
    }
    onSubmit(fields)
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>How devices reach this router</DialogTitle>
          <DialogDescription>
            Set once. Everything a device needs is derived from this.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="remote-endpoint">Public address</Label>
            <Input
              id="remote-endpoint"
              value={endpoint}
              autoFocus
              placeholder="home.example.net"
              onChange={(e) => setEndpoint(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              The name or address your devices dial from outside. If a name already follows this
              router&rsquo;s address, use it — that is what the DDNS records keep current. Add
              <span className="font-mono"> :port</span> if the port forwarded to this router differs
              from the one below.
            </p>
          </div>

          <Disclosure summary="Advanced">
            <div className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="remote-port">Port</Label>
                <Input
                  id="remote-port"
                  value={port}
                  inputMode="numeric"
                  placeholder="51820"
                  onChange={(e) => setPort(e.target.value)}
                />
                <p className="text-xs text-muted-foreground">
                  UDP. This is the port to forward to this router if it sits behind another one.
                </p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="remote-subnet">Dial-in network</Label>
                <Input
                  id="remote-subnet"
                  value={subnet}
                  placeholder="10.6.0.0/24"
                  onChange={(e) => setSubnet(e.target.value)}
                />
                <p className="text-xs text-muted-foreground">
                  The addresses dial-in devices get. Changing it renumbers every device — the
                  configuration already on each of them stops working and has to be replaced, so
                  this is the one setting here that reaches out of the router.
                </p>
              </div>
            </div>
          </Disclosure>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={endpoint.trim() === ''}>
            Save
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
