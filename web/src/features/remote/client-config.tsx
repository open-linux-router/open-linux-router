import encodeQR from '@paulmillr/qr'
import { Check, Copy, Download, QrCode } from 'lucide-react'
import { useMemo, useState } from 'react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
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
import type { RemotePeerResult } from '@/lib/api-types'

/**
 * The one screen in olr that shows something it can never show again.
 *
 * A device's private key is generated on the router, written into this file,
 * returned once, and stored nowhere (docs/remote-access.md §4.1). So this
 * dialog is not a confirmation — it is a handover, and every decision in it
 * follows from that:
 *
 *   - it is a dialog rather than a toast, because a toast disappears;
 *   - it cannot be re-opened, and does not pretend it can. Closing it is the
 *     end, which is why the button says so;
 *   - the QR code comes first. The device this is for is almost always a phone
 *     the operator is holding, and the alternative to a camera is retyping four
 *     hundred characters of base64 or emailing a private key to themselves.
 */
export function ClientConfigDialog({
  result,
  onClose,
}: {
  result: RemotePeerResult
  onClose: () => void
}) {
  const config = result.client_config ?? ''

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{result.name} is ready</DialogTitle>
          <DialogDescription>
            {result.address ? `It will be ${result.address} on the tunnel. ` : ''}
            Set it up on the device now.
          </DialogDescription>
        </DialogHeader>

        {config ? (
          <div className="space-y-4">
            {/* Loud, and above the thing it is about. An operator who closes
                this without acting has lost the device, and the only honest
                place to say so is before they have scrolled past it. */}
            <Alert>
              <AlertTitle>This is shown once</AlertTitle>
              <AlertDescription>
                The key in it is not stored on the router, so this cannot be shown again. If you
                lose it, remove {result.name} and add it back.
              </AlertDescription>
            </Alert>

            <QrPanel config={config} name={result.name} />

            <Disclosure summary="Or copy the file">
              <pre className="max-h-64 overflow-auto rounded-md bg-muted p-3 font-mono text-xs">
                {config}
              </pre>
            </Disclosure>

            {result.next_steps?.length ? (
              <div className="space-y-1 text-sm text-muted-foreground">
                <p>To reach it by name from home, run:</p>
                {result.next_steps.map((step) => (
                  <pre key={step} className="rounded-md bg-muted p-2 font-mono text-xs">
                    {step}
                  </pre>
                ))}
              </div>
            ) : null}
          </div>
        ) : (
          // The operator supplied the public key, so there is no private half
          // to write and no file to hand over. The note says why.
          <Alert>
            <AlertTitle>No file to hand over</AlertTitle>
            <AlertDescription>{result.note}</AlertDescription>
          </Alert>
        )}

        <DialogFooter>
          <Button onClick={onClose}>{config ? 'I have set it up' : 'Close'}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/**
 * The QR code, and the two escape hatches from it.
 *
 * Rendered as an inline SVG rather than a canvas so it stays sharp on a phone
 * held up to a laptop screen, which is the whole interaction this exists for.
 * `@paulmillr/qr` is a zero-dependency encoder; the alternative was asking the
 * operator to move a private key between two devices by hand, which people do
 * by emailing it to themselves.
 */
function QrPanel({ config, name }: { config: string; name: string }) {
  const [copied, setCopied] = useState(false)

  const svg = useMemo(() => {
    try {
      return encodeQR(config, 'svg')
    } catch {
      // A configuration too long to encode. Possible with a long endpoint and
      // several pushed routes, and not worth failing the whole handover over —
      // the file is still right there below.
      return null
    }
  }, [config])

  async function copy() {
    try {
      await navigator.clipboard.writeText(config)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // Clipboard access is refused on an insecure origin, which is exactly
      // what a router on a LAN address is. Silent: the file is on screen and
      // the download still works.
    }
  }

  function download() {
    const blob = new Blob([config], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${name}.conf`
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="space-y-3">
      {svg ? (
        <div className="flex flex-col items-center gap-2">
          <div
            className="rounded-md bg-white p-3 [&>svg]:size-56"
            // The encoder's own output. It is a QR code we just generated from
            // a string we hold, not anything that came off the network.
            dangerouslySetInnerHTML={{ __html: svg }}
            role="img"
            aria-label={`Configuration for ${name} as a QR code`}
          />
          <p className="text-xs text-muted-foreground">
            Scan it with the WireGuard app on the device.
          </p>
        </div>
      ) : (
        <Alert>
          <QrCode />
          <AlertDescription>
            This configuration is too long for a QR code. Copy or download it instead.
          </AlertDescription>
        </Alert>
      )}

      <div className="flex justify-center gap-2">
        <Button size="sm" variant="outline" onClick={copy}>
          {copied ? <Check /> : <Copy />}
          {copied ? 'Copied' : 'Copy'}
        </Button>
        <Button size="sm" variant="outline" onClick={download}>
          <Download />
          Download
        </Button>
      </div>
    </div>
  )
}
