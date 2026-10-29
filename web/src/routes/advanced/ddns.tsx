import { t, useLocale } from '@/lib/i18n'
import { AlertTriangle, Plus } from 'lucide-react'
import { useState } from 'react'
import { Heading } from '@/components/ui/heading'

import { StatusStrip } from '@/components/layout/status-strip'
import { SubPage } from '@/components/layout/sub-page'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { StatusInline } from '@/components/ui/status'
import { state } from '@/lib/state'
import { Button } from '@/components/ui/button'
import { List, ListEmpty, ListRow } from '@/components/ui/list'
import { Skeleton } from '@/components/ui/skeleton'
import { RecordDialog, providerLabel } from '@/features/dial/record-dialog'
import { useDialConfig, useDialStatus, useRecordEditor } from '@/features/dial/queries'
import type { RecordStatus } from '@/lib/api-types'
import type { Record as DdnsRecord } from '@/lib/config-types'

/**
 * DDNS, which had an API, a CLI and a publisher for two weeks and no
 * page at all.
 *
 * One list, because a record is the only object here (docs/ddns.md §1): a name
 * this router keeps pointing at itself. Each row answers the three questions
 * the status keeps apart — was the address read, what was it, did the provider
 * take it — and a record failing any of them is also written out in full above
 * the list. A subtitle truncates, and the provider's own error message is the
 * part of this page most worth reading whole.
 */
export function DdnsPage() {
  useLocale()
  const config = useDialConfig()
  const status = useDialStatus()
  const editor = useRecordEditor()

  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<DdnsRecord | null>(null)

  if (config.isPending) {
    return (
      <SubPage section="/advanced" slug="ddns">
        <Skeleton className="h-24 w-full" />
      </SubPage>
    )
  }
  if (config.isError) {
    return (
      <SubPage section="/advanced" slug="ddns">
        <Alert variant="destructive">
          <AlertTriangle />
          <AlertTitle>{t('Could not load DDNS')}</AlertTitle>
          <AlertDescription>{(config.error as Error).message}</AlertDescription>
        </Alert>
      </SubPage>
    )
  }

  const records = config.data.records ?? []
  const states = new Map((status.data?.records ?? []).map((r) => [r.name, r]))
  const failing = records
    .map((r) => states.get(r.name))
    .filter((s): s is RecordStatus => !!s && trouble(s) !== undefined)
  const uplink = status.data?.uplink?.interface

  return (
    <SubPage section="/advanced" slug="ddns">
      <StatusStrip
        headline={
          status.isError ? 'Cannot read update status' : status.isPending ? 'Checking update status…' : records.some((r) => !states.has(r.name)) ? 'Update status unknown' : records.length === 0
            ? 'No names yet'
            : failing.length > 0
              ? `${failing.length} of ${records.length} not up to date`
              : `Keeping ${records.length} name${records.length === 1 ? '' : 's'} current`
        }
        detail={
          status.isError ? status.error.message : records.length === 0
            ? 'Add a name you own and this router will keep it pointing at your address.'
            : 'Updates when the observed public address changes.'
        }
        tone={
          status.isError ? 'fault' : status.isPending || records.some((r) => !states.has(r.name))
            ? 'unknown'
            : records.length === 0
              ? 'off'
              : failing.length > 0
                ? 'warn'
                : 'ok'
        }
      />

      {failing.map((s) => (
        <Alert key={s.name}>
          <AlertTriangle />
          <AlertTitle>{s.name}</AlertTitle>
          <AlertDescription className="min-w-0 break-words">{trouble(s)}</AlertDescription>
        </Alert>
      ))}

      <div className="space-y-3">
        <div className="flex items-center justify-between gap-3">
          <Heading size="sm" className="px-1">{t('Names')}</Heading>
          <Button size="sm" onClick={() => setAdding(true)}>
            <Plus />{t('Add')}</Button>
        </div>

        {records.length === 0 ? (
          <ListEmpty> {t('No names configured.')} </ListEmpty>
        ) : (
          <List>
            {records.map((r) => {
              const s = states.get(r.name)
              return (
                <ListRow
                  key={r.name}
                  title={r.name}
                  wrap
                  subtitle={describe(r, s)}
                  trailing={<RecordState status={status.isError ? undefined : s} />}
                  editLabel={`${t('Edit')} ${r.name}`}
                  onSelect={() => setEditing(r)}
                />
              )
            })}
          </List>
        )}
      </div>

      <RecordDialog
        open={adding}
        onOpenChange={setAdding}
        uplink={uplink}
        onSubmit={(record) => editor.save(record)}
      />
      {editing && (
        <RecordDialog
          open
          onOpenChange={(open) => !open && setEditing(null)}
          initial={editing}
          onSubmit={(record) => editor.save(record)}
          onRemove={() => {
            setEditing(null)
            editor.remove(editing.name)
          }}
        />
      )}
    </SubPage>
  )
}

/** What is wrong with a record, in full, or nothing. */
function trouble(s: RecordStatus): string | undefined {
  const problem = s.problems?.[0]?.message
  if (problem) return problem
  if (s.check_error) return `Could not read the address: ${s.check_error}`
  if (s.publish_error) {
    const retry = s.retry ? ` Trying again ${when(s.retry)}.` : ''
    return `${providerLabel(s.provider)} refused the update: ${s.publish_error}.${retry}`
  }
  if (s.cgnat) {
    return `The address the internet sees, ${s.address}, belongs to your provider's shared NAT. Publishing it will not reach this router — ask your provider for a public address.`
  }
  return undefined
}

/** Provider, source and the one fact that matters most right now. */
function describe(r: DdnsRecord, s?: RecordStatus): string {
  const from =
    r.source === 'reflector'
      ? `asks ${hostOf(r.reflector_url)}`
      : `reads ${r.interface ?? 'the uplink'}`
  const where = `${providerLabel(r.provider)} · ${from}`
  if (!s?.published_address) return where
  return `${where} · published ${s.published_address} ${when(s.published!)}`
}

function RecordState({ status: s }: { status?: RecordStatus }) {
  useLocale()
  const health = !s ? state('Unknown')
    : s.problems?.length || s.check_error || s.publish_error ? state('Not up to date')
      : s.cgnat ? state('Behind shared NAT')
        : !s.watched || !s.checked ? state('Waiting')
          : s.address && s.published_address === s.address ? state('Up to date')
            : state('Updating')
  return <StatusInline tone={health.tone}>{health.label}</StatusInline>
}

function hostOf(url?: string): string {
  try {
    return new URL(url ?? '').host
  } catch {
    return 'a website'
  }
}

/** A past or future moment, roughly: "3 min ago", "in 2 min". */
function when(iso: string): string {
  const diff = new Date(iso).getTime() - Date.now()
  const minutes = Math.round(Math.abs(diff) / 60_000)
  const amount =
    minutes < 1
      ? 'moments'
      : minutes < 60
        ? `${minutes} min`
        : minutes < 48 * 60
          ? `${Math.round(minutes / 60)}h`
          : `${Math.round(minutes / 1440)}d`
  if (amount === 'moments') return diff < 0 ? 'just now' : 'in a moment'
  return diff < 0 ? `${amount} ago` : `in ${amount}`
}
