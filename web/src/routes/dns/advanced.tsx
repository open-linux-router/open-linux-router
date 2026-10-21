import { t, useLocale } from '@/lib/i18n'
import { SubPage } from '@/components/layout/sub-page'
import { EditableField } from '@/components/ui/editable-field'
import { Label } from '@/components/ui/label'
import { ApplyOutcome, useDnsEditor } from '@/features/dns/editor'

export function DnsAdvancedPage() {
  useLocale()
  const { config, busy, change, applier, gate } = useDnsEditor()
  if (!config) return gate

  const entries = config.query_log.entries ?? 0

  return (
    <SubPage section="/gateway/dns" slug="advanced">
      <ApplyOutcome applier={applier} />

      <EditableField
        id="dns-log-entries"
        label={t('Query log capacity')}
        busy={busy}
        placeholder="5000"
        stored={entries ? String(entries) : ''}
        onSave={(value) =>
          change({
            ...config,
            query_log: { ...config.query_log, entries: Number(value.trim()) || undefined },
          })
        }
        hint={t('Entries kept in memory. Default: 5,000.')}
      />

      <div className="space-y-3">
        <Label htmlFor="dns-extra">{t('Custom Unbound configuration')}</Label>
        <p className="text-sm text-muted-foreground"> {t('OLR-managed directives cannot be overridden.')} </p>
        <EditableField
          id="dns-extra"
          busy={busy}
          multiline
          stored={config.extra_unbound_conf ?? ''}
          onSave={(value) => change({ ...config, extra_unbound_conf: value || undefined })}
        />
      </div>
    </SubPage>
  )
}
