import { SubPage } from '@/components/layout/sub-page'
import { EditableField } from '@/components/ui/editable-field'
import { ApplyOutcome, useDhcpEditor } from '@/features/dhcp/editor'

export function DhcpAdvancedPage() {
  const { config, busy, change, applier, gate } = useDhcpEditor()
  if (!config) return gate

  return (
    <SubPage section="/gateway/dhcp" slug="advanced">
      <ApplyOutcome applier={applier} />

      <EditableField
        id="dhcp-extra"
        label="Custom dnsmasq configuration"
        busy={busy}
        multiline
        stored={config.extra_dnsmasq_conf ?? ''}
        onSave={(value) => change({ ...config, extra_dnsmasq_conf: value || undefined })}
        hint="OLR-managed directives cannot be overridden."
      />
    </SubPage>
  )
}
