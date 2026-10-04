import { SubPage } from '@/components/layout/sub-page'
import { EditableField, splitList } from '@/components/ui/editable-field'
import { ApplyOutcome, useDnsEditor } from '@/features/dns/editor'

export function DnsListeningPage() {
  const { config, busy, change, applier, gate } = useDnsEditor()
  if (!config) return gate

  return (
    <SubPage section="/dns" slug="listening">
      <ApplyOutcome applier={applier} />

      <EditableField
        id="dns-listen"
        label="Listening on"
        busy={busy}
        placeholder="every address this router holds"
        stored={(config.listen ?? []).join(', ')}
        onSave={(value) => change({ ...config, listen: splitList(value) })}
        hint="Leave this blank unless you have a reason not to: DNS then answers on every address this router holds, and keeps working when your network changes. Naming addresses here pins it to them, and they become yours to keep correct — if one stops existing, DNS stops. Use the firewall to control which networks can access DNS."
      />
    </SubPage>
  )
}
