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
    hint: 'Asks the root servers directly. Nobody else sees everything your network looks up.',
  },
  {
    value: 'forward',
    label: 'Ask another server',
    hint: 'Sends every lookup to the servers you name. Faster from cold, and the only way to use their filtering.',
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
    <SubPage section="/dns" slug="resolving">
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
            hint="Without this, every name your network looks up is visible to whoever carries the traffic — which is most of what choosing a particular server was meant to avoid."
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
              hint="Without this the connection is encrypted but not verified — you cannot tell you are talking to the server you meant."
            />
          )}
        </div>
      )}
    </SubPage>
  )
}
