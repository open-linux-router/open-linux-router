import { SubPage } from '@/components/layout/sub-page'
import { EditableField, SwitchField, splitList } from '@/components/ui/editable-field'
import { ApplyOutcome, useDnsEditor } from '@/features/dns/editor'

export function DnsEnforcementPage() {
  const { config, busy, change, applier, gate } = useDnsEditor()
  if (!config) return gate

  const hijack = config.hijack
  const set = (next: Partial<typeof hijack>) => change({ ...config, hijack: { ...hijack, ...next } })

  return (
    <SubPage section="/dns" slug="enforcement">
      <ApplyOutcome applier={applier} />

      <SwitchField
        id="dns-hijack"
        label="Redirect their lookups back here"
        hint="Anything sent to another DNS server is answered by this router instead. Devices need no configuration and notice nothing."
        checked={hijack.enabled}
        busy={busy}
        onChange={(enabled) => set({ enabled })}
      />

      {hijack.enabled && (
        <div className="space-y-4">
          <EditableField
            id="dns-hijack-interfaces"
            label="On these networks"
            busy={busy}
            placeholder="lan0, lan1"
            stored={(hijack.interfaces ?? []).join(', ')}
            onSave={(value) => set({ interfaces: splitList(value) })}
            hint="Comma separated, and required — leaving it blank does not mean 'everywhere', because that reading would capture your internet connection too."
          />

          <SwitchField
            id="dns-blockdot"
            label="Also block encrypted DNS"
            hint="A device that cannot be redirected can still slip out over an encrypted connection. Blocking that makes it wait, give up, and fall back to this router."
            checked={hijack.block_dot ?? false}
            busy={busy}
            onChange={(block_dot) => set({ block_dot })}
          />
        </div>
      )}
    </SubPage>
  )
}
