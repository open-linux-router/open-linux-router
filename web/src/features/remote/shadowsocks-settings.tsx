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
import { Textarea } from '@/components/ui/textarea'
import type { HowProxyTrafficIsEncrypted, Shadowsocks } from '@/lib/config-types'

const DEFAULT_CIPHER = '2022-blake3-aes-128-gcm'
const CIPHERS: { value: HowProxyTrafficIsEncrypted; label: string }[] = [
  { value: DEFAULT_CIPHER, label: '2022 AES-128-GCM (recommended)' },
  { value: '2022-blake3-aes-256-gcm', label: '2022 AES-256-GCM' },
  { value: '2022-blake3-chacha20-poly1305', label: '2022 ChaCha20-Poly1305' },
  { value: 'aes-256-gcm', label: 'Legacy AES-256-GCM' },
  { value: 'chacha20-ietf-poly1305', label: 'Legacy ChaCha20-Poly1305' },
]

function validPort(value: string) {
  return /^[1-9]\d*$/.test(value) && Number(value) <= 65535
}

export function ShadowsocksSettings({
  initial,
  onOpenChange,
  onSubmit,
}: {
  initial: Shadowsocks
  onOpenChange: (open: boolean) => void
  onSubmit: (fields: Record<string, unknown>) => void
}) {
  const [port, setPort] = useState(String(initial.listen_port || 8388))
  const [publicPort, setPublicPort] = useState(initial.public_port ? String(initial.public_port) : '')
  const [cipher, setCipher] = useState<HowProxyTrafficIsEncrypted>(initial.cipher || DEFAULT_CIPHER)
  const [udp, setUdp] = useState(initial.udp !== false)
  const [extra, setExtra] = useState(initial.raw_shadowsocks_conf ?? '')

  let extraError = ''
  if (extra.trim()) {
    try {
      const parsed: unknown = JSON.parse(extra)
      if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
        extraError = 'Enter a JSON object.'
      }
    } catch {
      extraError = 'Enter a valid JSON object.'
    }
  }
  const portError = validPort(port) ? '' : 'Enter a port from 1 to 65535.'
  const publicPortError =
    publicPort === '' || validPort(publicPort) ? '' : 'Enter a port from 1 to 65535.'
  const valid = !portError && !publicPortError && !extraError

  function submit() {
    if (!valid) return
    const fields: Record<string, unknown> = {}
    if (Number(port) !== (initial.listen_port || 8388)) fields.listen_port = Number(port)
    if (Number(publicPort) !== (initial.public_port || 0)) fields.public_port = Number(publicPort)
    if (cipher !== (initial.cipher || DEFAULT_CIPHER)) fields.cipher = cipher
    if (udp !== (initial.udp !== false)) fields.udp = udp
    if (extra.trim() !== (initial.raw_shadowsocks_conf ?? '')) {
      fields.raw_shadowsocks_conf = extra.trim()
    }
    if (Object.keys(fields).length) onSubmit(fields)
    onOpenChange(false)
  }

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Shadowsocks settings</DialogTitle>
          <DialogDescription>
            Every client uses the same link. Changing the cipher generates a new password and
            requires sharing a new link with every client.
          </DialogDescription>
        </DialogHeader>

        <div className="max-h-[65vh] space-y-5 overflow-y-auto pr-1">
          <div className="space-y-2">
            <Label htmlFor="ss-port">Listen port</Label>
            <Input
              id="ss-port"
              value={port}
              inputMode="numeric"
              aria-invalid={!!portError}
              onChange={(e) => setPort(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              TCP and UDP port on this router. Forward this port if it sits behind another router.
            </p>
            {portError && <p className="text-xs text-destructive">{portError}</p>}
          </div>
          <div className="space-y-2">
            <Label htmlFor="ss-cipher">Encryption</Label>
            <Select
              value={cipher}
              onValueChange={(value) => setCipher(value as HowProxyTrafficIsEncrypted)}
            >
              <SelectTrigger id="ss-cipher" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {CIPHERS.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              Use a legacy cipher only for clients that do not support the 2022 methods.
            </p>
          </div>
          <div className="flex items-center justify-between gap-4">
            <div className="space-y-1">
              <Label htmlFor="ss-udp">Carry UDP</Label>
              <p className="text-xs text-muted-foreground">Needed for DNS and QUIC in many apps.</p>
            </div>
            <Switch id="ss-udp" checked={udp} onCheckedChange={setUdp} />
          </div>
          <Disclosure summary="Advanced">
            <div className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="ss-public-port">Public port</Label>
                <Input
                  id="ss-public-port"
                  value={publicPort}
                  inputMode="numeric"
                  placeholder="Same as listen port"
                  aria-invalid={!!publicPortError}
                  onChange={(e) => setPublicPort(e.target.value)}
                />
                <p className="text-xs text-muted-foreground">
                  Only set this if the upstream router forwards a different external port. Clearing
                  it uses the listen port.
                </p>
                {publicPortError && <p className="text-xs text-destructive">{publicPortError}</p>}
              </div>
              <div className="space-y-2">
                <Label htmlFor="ss-extra">Additional server configuration (JSON)</Label>
                <Textarea
                  id="ss-extra"
                  value={extra}
                  className="font-mono text-xs"
                  placeholder="{}"
                  aria-invalid={!!extraError}
                  onChange={(e) => setExtra(e.target.value)}
                />
                <p className="text-xs text-muted-foreground">
                  A JSON object of extra shadowsocks-rust options. Port, password, method and mode
                  are managed above.
                </p>
                {extraError && <p className="text-xs text-destructive">{extraError}</p>}
              </div>
            </div>
          </Disclosure>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={!valid}>
            Save
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
