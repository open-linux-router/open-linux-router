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
import { useInterfaces } from '@/features/link/queries'
import type {
  DNSProvider,
  Record as DdnsRecord,
  WhereTheAddressComesFrom,
} from '@/lib/config-types'

/** What the API hands back in place of a credential — internal/dial RedactedToken. */
const MASK = '********'

/**
 * The providers, in the words their own consoles use.
 *
 * The key pair is labelled per vendor because the server's messages are: an
 * operator copying from the Alibaba console is looking for "AccessKey Secret",
 * and a form that says "secret" makes them guess which of two strings it means.
 */
const PROVIDERS: {
  value: DNSProvider
  label: string
  keyId?: string
  token?: string
}[] = [
  { value: 'cloudflare', label: 'Cloudflare', token: 'API token' },
  { value: 'alidns', label: 'Alibaba Cloud DNS', keyId: 'AccessKey ID', token: 'AccessKey Secret' },
  { value: 'tencentcloud', label: 'Tencent Cloud DNSPod', keyId: 'SecretId', token: 'SecretKey' },
  { value: 'callback', label: 'Another service, by URL' },
]

export function providerLabel(name: string) {
  return PROVIDERS.find((p) => p.value === name)?.label ?? name
}

// No source, on purpose — see RecordDialog. The type insists on one, and the
// Add button stays disabled until the operator has picked it.
const EMPTY: DdnsRecord = {
  name: '',
  provider: 'cloudflare',
  source: '' as WhereTheAddressComesFrom,
}

/**
 * Adds a public name to keep current, or edits one.
 *
 * The name cannot be changed once saved. It is the record's only identity, and
 * the server's item route takes the name from the path: an edit that renamed
 * would store a second record and leave the first one publishing. Removing and
 * adding again is honest about what that is.
 *
 * Credentials come back masked and are never shown. A field that still holds the
 * mask is sent back as the mask, which the server reads as "unchanged" — so
 * editing the interval does not mean typing the token again, and saving a form
 * nobody touched cannot overwrite the credential with asterisks.
 *
 * Where the address comes from has no default, matching the server. One answer
 * makes this box talk to a third party every few minutes and the other does
 * not, and that is the operator's call, not a pre-selected radio button's.
 */
export function RecordDialog({
  open,
  onOpenChange,
  initial,
  uplink,
  onSubmit,
  onRemove,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  initial?: DdnsRecord
  /** The interface olr owns as the way out, if any — the likely answer. */
  uplink?: string
  onSubmit: (record: DdnsRecord) => void
  onRemove?: () => void
}) {
  const start = initial ?? { ...EMPTY, interface: uplink }
  const [draft, setDraft] = useState<DdnsRecord>(start)
  const editing = initial !== undefined

  const interfaces = useInterfaces()
  const available = (interfaces.data?.interfaces ?? []).filter((i) => i.adopted && !i.loopback)

  const provider = PROVIDERS.find((p) => p.value === draft.provider) ?? PROVIDERS[0]
  const callback = draft.provider === 'callback'
  const reflector = draft.source === 'reflector'
  const chosen = (draft.source as string) !== ''

  function set<K extends keyof DdnsRecord>(key: K, value: DdnsRecord[K]) {
    setDraft((d) => ({ ...d, [key]: value }))
  }

  function pickProvider(value: DNSProvider) {
    // The credential fields mean different things per provider, and the server
    // refuses the ones that do not apply — so a switch clears them rather than
    // carrying a Cloudflare token into a callback's request body.
    setDraft((d) => ({
      ...d,
      provider: value,
      provider_key_id: undefined,
      provider_token: undefined,
      callback_url: undefined,
    }))
  }

  const name = draft.name.trim()
  const hasSecret = (v?: string) => (v ?? '') !== ''
  const complete =
    name !== '' &&
    (callback
      ? hasSecret(draft.callback_url)
      : hasSecret(draft.provider_token) && (!provider.keyId || hasSecret(draft.provider_key_id))) &&
    chosen &&
    (reflector ? hasSecret(draft.reflector_url) : hasSecret(draft.interface))

  function reset() {
    setDraft(start)
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) reset()
        onOpenChange(next)
      }}
    >
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{editing ? `Edit ${initial.name}` : 'Keep a name current'}</DialogTitle>
          <DialogDescription>
            A public name that follows this router&rsquo;s address, so it keeps working when your
            internet provider changes it.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="ddns-name">Name</Label>
            <Input
              id="ddns-name"
              value={draft.name}
              placeholder="home.example.net"
              autoComplete="off"
              disabled={editing}
              onChange={(e) => set('name', e.target.value)}
            />
            {editing && (
              <p className="text-xs text-muted-foreground">
                To use a different name, remove this one and add it again.
              </p>
            )}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="ddns-provider">Where the name is hosted</Label>
            <Select
              value={draft.provider}
              onValueChange={(v) => v && pickProvider(v as DNSProvider)}
            >
              <SelectTrigger id="ddns-provider">
                <SelectValue>{(v: string) => providerLabel(v)}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {PROVIDERS.map((p) => (
                  <SelectItem key={p.value} value={p.value}>
                    {p.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {callback && (
              <p className="text-xs text-muted-foreground">
                For DuckDNS, No-IP, Dynu and the other services updated by visiting a link.
              </p>
            )}
          </div>

          {callback ? (
            <SecretField
              key="callback"
              id="ddns-callback"
              label="Update URL"
              value={draft.callback_url}
              placeholder="https://www.duckdns.org/update?domains=home&token=…&ip=#{ip}"
              hint={
                <>
                  Put <span className="font-mono">#{'{ip}'}</span> where the address goes. It is
                  treated as a password, because most of these services put the token in it.
                </>
              }
              onChange={(v) => set('callback_url', v)}
            />
          ) : (
            <>
              {provider.keyId && (
                <div className="space-y-1.5">
                  <Label htmlFor="ddns-key-id">{provider.keyId}</Label>
                  <Input
                    id="ddns-key-id"
                    value={draft.provider_key_id ?? ''}
                    autoComplete="off"
                    onChange={(e) => set('provider_key_id', e.target.value.trim())}
                  />
                </div>
              )}
              <SecretField
                // Remounted per provider, so a switch forgets that the old
                // provider's credential was saved.
                key={draft.provider}
                id="ddns-token"
                label={provider.token ?? 'Secret'}
                value={draft.provider_token}
                hint="Limit it to this one domain if the provider lets you. olr never shows it again."
                onChange={(v) => set('provider_token', v)}
              />
            </>
          )}

          <div className="space-y-1.5">
            <Label htmlFor="ddns-source">Where the address comes from</Label>
            <Select
              value={draft.source || null}
              onValueChange={(v) =>
                v &&
                setDraft((d) => ({
                  ...d,
                  source: v as WhereTheAddressComesFrom,
                  // Only the reflector takes a URL; the server refuses it on the
                  // other source rather than ignoring it.
                  reflector_url: v === 'reflector' ? d.reflector_url : undefined,
                }))
              }
            >
              <SelectTrigger id="ddns-source">
                <SelectValue placeholder="Choose">
                  {(v: string) =>
                    v === 'reflector' ? 'Ask a website what it sees' : 'This router’s interface'
                  }
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="interface">This router&rsquo;s interface</SelectItem>
                <SelectItem value="reflector">Ask a website what it sees</SelectItem>
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              {!chosen
                ? 'Read it off the router itself if the router holds your public address; ask a website if it sits behind a modem.'
                : reflector
                  ? 'For a router behind a modem, whose own address is a private one. Also the only way to notice your provider has put you behind shared NAT.'
                  : 'For a router plugged straight into the internet, holding the public address itself.'}
            </p>
          </div>

          {reflector && (
            <div className="space-y-1.5">
              <Label htmlFor="ddns-reflector">Website to ask</Label>
              <Input
                id="ddns-reflector"
                value={draft.reflector_url ?? ''}
                placeholder="https://api.ipify.org"
                autoComplete="off"
                onChange={(e) => set('reflector_url', e.target.value.trim())}
              />
              <p className="text-xs text-muted-foreground">
                Any https address that answers with the address it was reached from. There is no
                default: this router asks it every few minutes, so choose who that is.
              </p>
            </div>
          )}

          {chosen && !reflector && (
            <InterfacePicker
              value={draft.interface}
              interfaces={available.map((i) => i.name)}
              onChange={(v) => set('interface', v)}
            />
          )}

          <Disclosure summary="More options">
            <div className="space-y-4">
              {reflector && (
                <InterfacePicker
                  value={draft.interface}
                  interfaces={available.map((i) => i.name)}
                  optional
                  onChange={(v) => set('interface', v)}
                />
              )}
              <div className="space-y-1.5">
                <Label htmlFor="ddns-zone">Zone</Label>
                <Input
                  id="ddns-zone"
                  value={draft.zone ?? ''}
                  placeholder="Worked out from the name"
                  autoComplete="off"
                  onChange={(e) => set('zone', e.target.value.trim() || undefined)}
                />
                <p className="text-xs text-muted-foreground">
                  The domain as your provider lists it. Only needed when it is not the usual
                  registered domain — a zone delegated to you below someone else&rsquo;s.
                </p>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-1.5">
                  <Label htmlFor="ddns-interval">Check every</Label>
                  <Input
                    id="ddns-interval"
                    value={draft.interval ?? ''}
                    placeholder="5m"
                    autoComplete="off"
                    onChange={(e) => set('interval', e.target.value.trim() || undefined)}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="ddns-ttl">TTL (seconds)</Label>
                  <Input
                    id="ddns-ttl"
                    inputMode="numeric"
                    value={draft.ttl || ''}
                    placeholder="Provider default"
                    onChange={(e) =>
                      set('ttl', Number(e.target.value.replace(/\D/g, '')) || undefined)
                    }
                  />
                </div>
              </div>
            </div>
          </Disclosure>
        </div>

        <DialogFooter className="sm:justify-between">
          {onRemove ? (
            <Button variant="ghost" className="text-destructive" onClick={onRemove}>
              <Trash2 />
              Stop keeping it current
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
                onSubmit({ ...draft, name, interface: draft.interface || undefined })
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

/**
 * A credential input that never shows the stored value.
 *
 * The mask is kept in the draft rather than in the input, so the field reads as
 * empty with "saved" in its placeholder, and leaving it alone sends the mask back
 * — which is "unchanged" to the server. Clearing a field somebody typed into
 * puts the mask back for the same reason: an empty credential is never what
 * editing a saved record meant.
 */
function SecretField({
  id,
  label,
  value,
  placeholder,
  hint,
  onChange,
}: {
  id: string
  label: string
  value?: string
  placeholder?: string
  hint: React.ReactNode
  onChange: (value: string | undefined) => void
}) {
  const [saved] = useState(value === MASK)
  const shown = value === MASK ? '' : (value ?? '')

  return (
    <div className="space-y-1.5">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        type="password"
        value={shown}
        placeholder={saved ? 'Saved — type to replace' : placeholder}
        autoComplete="new-password"
        onChange={(e) => onChange(e.target.value || (saved ? MASK : undefined))}
      />
      <p className="text-xs text-muted-foreground">{hint}</p>
    </div>
  )
}

function InterfacePicker({
  value,
  interfaces,
  optional,
  onChange,
}: {
  value?: string
  interfaces: string[]
  optional?: boolean
  onChange: (value: string | undefined) => void
}) {
  // A saved interface that is no longer adopted still has to show, or the
  // select would render blank and the operator could not see what is wrong.
  const options = value && !interfaces.includes(value) ? [value, ...interfaces] : interfaces
  const ANY = ' any'

  return (
    <div className="space-y-1.5">
      <Label htmlFor="ddns-interface">
        {optional ? 'Ask through' : 'Read the address off'}
      </Label>
      <Select
        value={value || (optional ? ANY : null)}
        onValueChange={(v) => onChange(!v || v === ANY ? undefined : v)}
      >
        <SelectTrigger id="ddns-interface">
          <SelectValue placeholder="Choose an interface">
            {(v: string) => (v === ANY ? 'Whichever way out is in use' : v)}
          </SelectValue>
        </SelectTrigger>
        <SelectContent>
          {optional && <SelectItem value={ANY}>Whichever way out is in use</SelectItem>}
          {options.map((name) => (
            <SelectItem key={name} value={name}>
              {name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {optional && (
        <p className="text-xs text-muted-foreground">
          Only matters with more than one internet connection, so the name follows the one you
          mean.
        </p>
      )}
    </div>
  )
}
