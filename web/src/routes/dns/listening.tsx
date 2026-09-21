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
        hint="Leave this blank unless you have a reason not to: DNS then answers on every address this router holds, and keeps working when your network changes. Naming addresses here pins it to them, and they become yours to keep correct — if one stops existing, DNS stops. What keeps this router from answering the internet is 'Who may ask' below, not this field."
      />

      <EditableField
        id="dns-allow"
        label="Who may ask"
        busy={busy}
        placeholder="your own networks"
        stored={(config.allow_from ?? []).join(', ')}
        onSave={(value) => change({ ...config, allow_from: splitList(value) })}
        hint="Ranges, comma separated. Blank means your own networks — not everybody — worked out fresh each time, so it follows your network if that changes. Anything else is dropped without an answer, which is what keeps this router from being used to attack someone else. This is the setting that matters; leave it blank unless you are adding a network olr does not manage."
      />
    </SubPage>
  )
}
