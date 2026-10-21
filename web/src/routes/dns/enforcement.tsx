import { t, useLocale } from '@/lib/i18n'
import { SubPage } from '@/components/layout/sub-page'
import { EditableField, SwitchField, splitList } from '@/components/ui/editable-field'
import { ApplyOutcome, useDnsEditor } from '@/features/dns/editor'

export function DnsEnforcementPage() {
  useLocale()
  const { config, busy, change, applier, gate } = useDnsEditor()
  if (!config) return gate

  const hijack = config.hijack
  const set = (next: Partial<typeof hijack>) => change({ ...config, hijack: { ...hijack, ...next } })

  return (
    <SubPage section="/gateway/dns" slug="enforcement">
      <ApplyOutcome applier={applier} />

      <SwitchField
        id="dns-hijack"
        label={t('Redirect external DNS')}
        hint={t('Redirect standard DNS requests on the selected networks to OLR.')}
        checked={hijack.enabled}
        busy={busy}
        onChange={(enabled) => set({ enabled })}
      />

      {hijack.enabled && (
        <div className="space-y-4">
          <EditableField
            id="dns-hijack-interfaces"
            label={t('On these networks')}
            busy={busy}
            placeholder="lan0, lan1"
            stored={(hijack.interfaces ?? []).join(', ')}
            onSave={(value) => set({ interfaces: splitList(value) })}
            hint="Required interface names, comma separated. Blank does not mean all networks."
          />

          <SwitchField
            id="dns-blockdot"
            label={t('Also block encrypted DNS')}
            hint={t('Block DNS over TLS on port 853. DNS over HTTPS is not blocked; clients may fail instead of falling back.')}
            checked={hijack.block_dot ?? false}
            busy={busy}
            onChange={(block_dot) => set({ block_dot })}
          />
        </div>
      )}
    </SubPage>
  )
}
