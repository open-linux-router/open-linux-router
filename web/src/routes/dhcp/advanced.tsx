import { t, useLocale } from '@/lib/i18n'
import { SubPage } from '@/components/layout/sub-page'
import { EditableField } from '@/components/ui/editable-field'
import { ApplyOutcome, useDhcpEditor } from '@/features/dhcp/editor'

export function DhcpAdvancedPage() {
  useLocale()
  const { config, busy, change, applier, gate } = useDhcpEditor()
  if (!config) return gate

  return (
    <SubPage section="/gateway/dhcp" slug="advanced">
      <ApplyOutcome applier={applier} />

      <EditableField
        id="dhcp-extra"
        label={t('Custom dnsmasq configuration')}
        busy={busy}
        multiline
        stored={config.extra_dnsmasq_conf ?? ''}
        onSave={(value) => change({ ...config, extra_dnsmasq_conf: value || undefined })}
        hint={t('OLR-managed directives cannot be overridden.')}
      />
    </SubPage>
  )
}
