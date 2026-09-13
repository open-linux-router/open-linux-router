import { SubPage } from '@/components/layout/sub-page'
import { EditableField } from '@/components/ui/editable-field'
import { ApplyOutcome, useDhcpEditor } from '@/features/dhcp/editor'

export function DhcpAdvancedPage() {
  const { config, busy, change, applier, gate } = useDhcpEditor()
  if (!config) return gate

  return (
    <SubPage section="/dhcp" slug="advanced">
      <ApplyOutcome applier={applier} />

      <EditableField
        id="dhcp-extra"
        label="Extra dnsmasq configuration"
        busy={busy}
        multiline
        stored={config.extra_dnsmasq_conf ?? ''}
        onSave={(value) => change({ ...config, extra_dnsmasq_conf: value || undefined })}
        hint="Directives the router writes itself are refused, so this cannot quietly override a setting above."
      />
    </SubPage>
  )
}
