import { SubPage } from '@/components/layout/sub-page'
import { EditableField } from '@/components/ui/editable-field'
import { Label } from '@/components/ui/label'
import { ApplyOutcome, useDnsEditor } from '@/features/dns/editor'

export function DnsAdvancedPage() {
  const { config, busy, change, applier, gate } = useDnsEditor()
  if (!config) return gate

  const entries = config.query_log.entries ?? 0

  return (
    <SubPage section="/dns" slug="advanced">
      <ApplyOutcome applier={applier} />

      <EditableField
        id="dns-log-entries"
        label="How many lookups to keep"
        busy={busy}
        placeholder="5000"
        stored={entries ? String(entries) : ''}
        onSave={(value) =>
          change({
            ...config,
            query_log: { ...config.query_log, entries: Number(value.trim()) || undefined },
          })
        }
        hint="Blank for the default of 5000. A count rather than a length of time, because the log lives in memory and the number that matters is the one bounding it."
      />

      <div className="space-y-3">
        <Label htmlFor="dns-extra">Extra unbound configuration</Label>
        <p className="text-sm text-muted-foreground">
          Settings this router does not model, passed straight through to
          unbound. Kept here rather than hand-edited into the daemon's file, so
          they stay part of your configuration and survive an upgrade.
          Directives the router writes itself are refused.
        </p>
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
