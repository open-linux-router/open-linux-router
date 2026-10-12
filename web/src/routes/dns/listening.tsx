import { SubPage } from '@/components/layout/sub-page'
import { EditableField, splitList } from '@/components/ui/editable-field'
import { ApplyOutcome, useDnsEditor } from '@/features/dns/editor'

export function DnsListeningPage() {
  const { config, busy, change, applier, gate } = useDnsEditor()
  if (!config) return gate

  return (
    <SubPage section="/gateway/dns" slug="listening">
      <ApplyOutcome applier={applier} />

      <EditableField
        id="dns-listen"
        label="Listening on"
        busy={busy}
        placeholder="All router addresses (default)"
        stored={(config.listen ?? []).join(', ')}
        onSave={(value) => change({ ...config, listen: splitList(value) })}
        hint="Blank listens on all router addresses. A specified address must remain available or DNS may fail to start. Use the firewall to restrict access."
      />
    </SubPage>
  )
}
