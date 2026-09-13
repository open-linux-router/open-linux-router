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
        placeholder="192.168.1.1:53"
        stored={(config.listen ?? []).join(', ')}
        onSave={(value) => change({ ...config, listen: splitList(value) })}
        hint="Comma separated, and always specific addresses. Turning DNS on with this empty fills in this router's own address on each network you have given it. A resolver reachable from the internet is something other people can abuse to attack a third party, so there is deliberately no way to say 'everywhere' here."
      />

      <EditableField
        id="dns-allow"
        label="Who may ask"
        busy={busy}
        placeholder="the networks it listens on"
        stored={(config.allow_from ?? []).join(', ')}
        onSave={(value) => change({ ...config, allow_from: splitList(value) })}
        hint="Ranges, comma separated. Blank means the networks of the addresses above — not everybody. Anything else is dropped without an answer."
      />
    </SubPage>
  )
}
