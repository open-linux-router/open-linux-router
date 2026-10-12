import { SubPage } from '@/components/layout/sub-page'
import { EditableField, SwitchField, splitList } from '@/components/ui/editable-field'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { ApplyOutcome, useDnsEditor } from '@/features/dns/editor'
import type { UpstreamMode } from '@/lib/config-types'

const MODE_OPTIONS: { value: Exclude<UpstreamMode, ''>; label: string; hint: string }[] = [
  {
    value: 'recurse',
    label: 'Find answers itself',
    hint: 'Resolves through the DNS hierarchy.',
  },
  {
    value: 'forward',
    label: 'Ask another server',
    hint: 'Forwards requests to the servers below.',
  },
]

const MODE_LABEL = new Map(MODE_OPTIONS.map((o) => [o.value as string, o.label]))

export function DnsResolvingPage() {
  const { config, busy, change, applier, gate } = useDnsEditor()
  if (!config) return gate

  const upstream = config.upstream
  const forwarding = (upstream.mode || 'recurse') === 'forward'
  const set = (next: Partial<typeof upstream>) =>
    change({ ...config, upstream: { ...upstream, ...next } })

  return (
    <SubPage section="/gateway/dns" slug="resolving">
      <ApplyOutcome applier={applier} />

      <div className="grid gap-2">
        <Label htmlFor="dns-mode">Method</Label>
        <Select
          value={upstream.mode || 'recurse'}
          onValueChange={(v) => set({ mode: v as UpstreamMode })}
        >
          <SelectTrigger id="dns-mode" className="w-full sm:max-w-sm" disabled={busy}>
            <SelectValue>
              {(value: string) => MODE_LABEL.get(value) ?? MODE_OPTIONS[0].label}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            {MODE_OPTIONS.map(({ value, label, hint }) => (
              <SelectItem key={value} value={value}>
                <span className="flex flex-col gap-0.5">
                  <span>{label}</span>
                  <span className="text-xs text-muted-foreground">{hint}</span>
                </span>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {forwarding && (
        <div className="space-y-4">
          <EditableField
            id="dns-servers"
            label="Servers"
            busy={busy}
            placeholder="1.1.1.1, 9.9.9.9"
            stored={(upstream.servers ?? []).join(', ')}
            onSave={(value) => set({ servers: splitList(value) })}
            hint="Comma separated. A server without a port means 53, or 853 when encrypted."
          />

          <SwitchField
            id="dns-tls"
            label="Encrypt the connection"
            hint="Use DNS over TLS to encrypt requests to the upstream servers."
            checked={upstream.tls ?? false}
            busy={busy}
            onChange={(tls) => set({ tls })}
          />

          {upstream.tls && (
            <EditableField
              id="dns-tls-name"
              label="Certificate name"
              busy={busy}
              placeholder="cloudflare-dns.com"
              stored={upstream.tls_name ?? ''}
              onSave={(value) => set({ tls_name: value.trim() || undefined })}
              hint="Required to verify the upstream server. Blank encrypts the connection without verifying its identity."
            />
          )}
        </div>
      )}
    </SubPage>
  )
}
