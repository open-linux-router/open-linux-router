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
import { useDeviceList } from '@/features/devices/queries'
import type { Service, UpstreamScheme } from '@/lib/config-types'

const EMPTY: Service = { name: '', upstream: { port: 0 } }

/** The sentinel for "somewhere that is not a device on this network". */
const MANUAL = ' manual'

/**
 * Publishes a service, or edits one.
 *
 * Two fields, which is the module's whole claim: a name and where requests go.
 * The certificate and the name's answer are not asked about because they were
 * arranged once — one wildcard certificate covers every name that will ever
 * exist, and the dns module already serves the suffix.
 *
 * The target is a **device reference**, not an address, which is the opposite of
 * the firewall's forward dialog and worth the contrast. A forward is a kernel
 * rule that has to exist at boot, before any lease exists; a published service is
 * resolved per request, so it can name the device and let another module say
 * where that device is (design.md §4.1). Nothing here copies an address.
 *
 * The consequence shapes the picker: a device with no *fixed* address is not
 * offered at all. The daemon refuses it — a published name pointing at a pool
 * address works until the lease turns over and then proxies to a stranger's
 * laptop — and offering a choice that is about to be rejected is worse than not
 * offering it.
 */
export function ServiceDialog({
  open,
  onOpenChange,
  initial,
  domain,
  onSubmit,
  onRemove,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  initial?: Service
  /** The suffix names live under, for the preview. Owned by the dns module. */
  domain?: string
  onSubmit: (service: Service) => void
  /** Only supplied when editing. Removing is an edit to the thing you opened. */
  onRemove?: () => void
}) {
  const [draft, setDraft] = useState<Service>(initial ?? EMPTY)
  const [target, setTarget] = useState<string>(initial?.upstream.device ?? MANUAL)
  const editing = initial !== undefined

  const devices = useDeviceList()
  const all = devices.data?.devices ?? []

  // Only devices with a reserved address, per the header. Sorted so the list is
  // stable between renders rather than in whatever order presence came back in.
  const offerable = all
    .filter((d) => d.fixed_ip && d.name)
    .sort((a, b) => a.name.localeCompare(b.name))

  // How many were left out, and why — said once under the picker. Without this
  // the list looks arbitrary to somebody who can see the device on the Devices
  // page and cannot find it here.
  const withoutFixed = all.filter((d) => !d.fixed_ip && d.name).length

  function upstream<K extends keyof Service['upstream']>(key: K, value: Service['upstream'][K]) {
    setDraft((d) => ({ ...d, upstream: { ...d.upstream, [key]: value } }))
  }

  function pickTarget(value: string | null) {
    const next = value ?? MANUAL
    setTarget(next)
    setDraft((d) => ({
      ...d,
      // Device and host are alternatives and the daemon refuses both at once, so
      // choosing one clears the other here rather than letting a stale value ride
      // along into a validation error about a field the operator cannot see.
      upstream: { ...d.upstream, device: next === MANUAL ? '' : next, host: '' },
    }))
  }

  const name = draft.name.trim()
  const complete =
    name !== '' && draft.upstream.port > 0 && (target !== MANUAL || (draft.upstream.host ?? '') !== '')

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          setDraft(initial ?? EMPTY)
          setTarget(initial?.upstream.device ?? MANUAL)
        }
        onOpenChange(next)
      }}
    >
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{editing ? `Edit ${initial.name}` : 'Publish a service'}</DialogTitle>
          <DialogDescription>
            Reach something on your network at a proper https:// address, with no port number and no
            certificate warning.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="svc-name">Address</Label>
            <div className="flex items-center gap-1.5">
              <Input
                id="svc-name"
                value={draft.name}
                placeholder="grafana"
                autoComplete="off"
                onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))}
              />
              {/* The suffix shown rather than typed, because it is not this
                  module's to set — and seeing the whole address assembled is
                  what makes the single word above obviously enough. */}
              <span className="shrink-0 font-mono text-sm text-muted-foreground">
                .{domain ?? '…'}
              </span>
            </div>
            <p className="text-xs text-muted-foreground">
              {name ? (
                <>
                  It will answer at{' '}
                  <span className="font-mono">
                    https://{name}.{domain ?? '…'}
                  </span>
                </>
              ) : (
                'One word. The rest of the address is the name your network already answers for.'
              )}
            </p>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="svc-target">Where it runs</Label>
            <Select value={target} onValueChange={pickTarget}>
              <SelectTrigger id="svc-target">
                {/* A render function, not the bare value: SelectValue shows what
                    it is given, and the sentinel is a string nobody should ever
                    read. Without this the trigger says "manual". */}
                <SelectValue placeholder="Choose a device">
                  {(v: string) => (v === MANUAL ? 'Somewhere else…' : v)}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {offerable.map((d) => (
                  <SelectItem key={d.name} value={d.name}>
                    {d.name} — {d.fixed_ip}
                  </SelectItem>
                ))}
                <SelectItem value={MANUAL}>Somewhere else…</SelectItem>
              </SelectContent>
            </Select>
            {target === MANUAL ? (
              <p className="text-xs text-muted-foreground">
                For something that is not a device on this network — a container on the router
                itself, or a machine behind another router.
              </p>
            ) : (
              <p className="text-xs text-muted-foreground">
                olr looks up where this device is each time, so the address below can change without
                breaking the link.
              </p>
            )}
          </div>

          {target === MANUAL && (
            <div className="space-y-1.5">
              <Label htmlFor="svc-host">Address or hostname</Label>
              <Input
                id="svc-host"
                value={draft.upstream.host ?? ''}
                placeholder="127.0.0.1"
                autoComplete="off"
                onChange={(e) => upstream('host', e.target.value)}
              />
            </div>
          )}

          <div className="space-y-1.5">
            <Label htmlFor="svc-port">Port it listens on</Label>
            <Input
              id="svc-port"
              inputMode="numeric"
              value={draft.upstream.port || ''}
              placeholder="3000"
              onChange={(e) => upstream('port', Number(e.target.value.replace(/\D/g, '')) || 0)}
            />
            <p className="text-xs text-muted-foreground">
              The port you use today, the one in the address with the number in it.
            </p>
          </div>

          {withoutFixed > 0 && target !== MANUAL && (
            // Said here rather than as a validation error later: the device
            // somebody is looking for is missing from the list above, and the
            // reason is a setting on a different page.
            <p className="text-xs text-muted-foreground">
              {withoutFixed === 1 ? 'One device is' : `${withoutFixed} devices are`} not listed
              because {withoutFixed === 1 ? 'it has' : 'they have'} no reserved address. A published
              address has to keep pointing at the same machine, so reserve one on the Devices page
              first.
            </p>
          )}

          <Disclosure summary="It only speaks https">
            <div className="space-y-1.5">
              <Label htmlFor="svc-scheme">How olr reaches it</Label>
              <Select
                value={draft.upstream.scheme || 'http'}
                onValueChange={(v) => upstream('scheme', (v ?? 'http') as UpstreamScheme)}
              >
                <SelectTrigger id="svc-scheme">
                  <SelectValue>{(v: string) => (v === 'https' ? 'https' : 'http')}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="http">http</SelectItem>
                  <SelectItem value="https">https</SelectItem>
                </SelectContent>
              </Select>
              {/* The https case is the NAS and hypervisor UIs that refuse plain
                  HTTP. Their certificates are self-signed and are deliberately
                  not checked — over your own network, to a device you named,
                  demanding a valid one would make this option useless. */}
              <p className="text-xs text-muted-foreground">
                Leave this on http unless the service refuses it. The https a browser sees is
                created here either way.
              </p>
            </div>
          </Disclosure>
        </div>

        <DialogFooter className="sm:justify-between">
          {onRemove ? (
            <Button variant="ghost" className="text-destructive" onClick={onRemove}>
              <Trash2 />
              Stop publishing
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
                onSubmit({
                  name,
                  upstream: {
                    ...draft.upstream,
                    device: target === MANUAL ? undefined : target,
                    host: target === MANUAL ? (draft.upstream.host ?? '').trim() : undefined,
                  },
                })
                onOpenChange(false)
              }}
            >
              {editing ? 'Save' : 'Publish'}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
