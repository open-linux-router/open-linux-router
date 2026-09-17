import encodeQR from '@paulmillr/qr'
import { Check, Copy, QrCode } from 'lucide-react'
import { useMemo, useState } from 'react'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

/**
 * Handing the proxy's credential to a device.
 *
 * The shape is borrowed from the tunnel's client-config dialog — QR code first,
 * because the device this is for is almost always a phone — and it differs in
 * one way that had to be got right rather than copied:
 *
 * **This one can be shown again, and the tunnel's cannot.** A WireGuard peer has
 * a private key that exists for one instant and is then forgotten, so that
 * dialog warns that closing it loses the configuration forever. Here there is
 * one password for every client and olr stores it, so re-opening this is
 * ordinary. Carrying over the tunnel's warning would have been a lie that
 * teaches operators to distrust the next warning they see.
 *
 * What *is* true and worth saying is the other half: everybody shares this
 * secret, so revoking one device means changing it for all of them.
 */
export function ProxyLinkDialog({
  url,
  label,
  open,
  onOpenChange,
}: {
  url: string
  label?: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const [copied, setCopied] = useState(false)

  const svg = useMemo(() => {
    try {
      return encodeQR(url, 'svg')
    } catch {
      // An ss:// link is short — endpoint, cipher and a 16- or 32-byte key — so
      // this is close to unreachable. Handled anyway, because the fallback is
      // one line and the alternative is a blank dialog.
      return null
    }
  }, [url])

  async function copy() {
    try {
      await navigator.clipboard.writeText(url)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // Clipboard access is refused on an insecure origin, which is exactly
      // what a router on a LAN address is. Silent: the link is on screen.
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Connect a device to the proxy</DialogTitle>
          <DialogDescription>
            Every device uses this same link. Changing it disconnects all of them at once.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          {svg ? (
            <div className="flex flex-col items-center gap-2">
              <div
                className="rounded-md bg-white p-3 [&>svg]:size-56"
                // The encoder's own output, generated here from a string we
                // hold. Nothing in it came off the network.
                dangerouslySetInnerHTML={{ __html: svg }}
                role="img"
                aria-label="Proxy link as a QR code"
              />
              <p className="text-xs text-muted-foreground">
                Scan it with a Shadowsocks client on the device.
              </p>
            </div>
          ) : (
            <Alert>
              <QrCode />
              <AlertDescription>
                This link is too long for a QR code. Copy it instead.
              </AlertDescription>
            </Alert>
          )}

          {/* Shown as well as encoded: a desktop client is configured by pasting,
              and somebody debugging needs to see what the phone was given. `break-all`
              because the base64 credential has no spaces to wrap at. */}
          <p className="rounded-md bg-muted p-2 font-mono text-xs break-all">{url}</p>
          {label && <p className="text-xs text-muted-foreground">Shows up as “{label}”.</p>}
        </div>

        <DialogFooter className="sm:justify-between">
          <Button size="sm" variant="outline" onClick={copy}>
            {copied ? <Check /> : <Copy />}
            {copied ? 'Copied' : 'Copy link'}
          </Button>
          <Button size="sm" onClick={() => onOpenChange(false)}>
            Done
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
