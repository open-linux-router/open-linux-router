import { AlertTriangle, Plus } from 'lucide-react'
import { useState } from 'react'

import { StatusStrip } from '@/components/layout/status-strip'
import { SubPage } from '@/components/layout/sub-page'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { List, ListEmpty, ListRow } from '@/components/ui/list'
import { Skeleton } from '@/components/ui/skeleton'
import { RecordDialog, providerLabel } from '@/features/dial/record-dialog'
import { useDialConfig, useDialStatus, useRecordEditor } from '@/features/dial/queries'
import type { RecordStatus } from '@/lib/api-types'
import type { Record as DdnsRecord } from '@/lib/config-types'

/**
 * Dynamic DNS, which had an API, a CLI and a publisher for two weeks and no
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
          <AlertTitle>Could not load dynamic DNS</AlertTitle>
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
          records.length === 0
            ? 'No names yet'
            : failing.length > 0
              ? `${failing.length} of ${records.length} not up to date`
              : `Keeping ${records.length} name${records.length === 1 ? '' : 's'} current`
        }
        detail={
          records.length === 0
            ? 'Add a name you own and this router will keep it pointing at your address.'
            : 'Each name is checked on its own timer and updated at the provider only when your address changes.'
        }
        dot={
          records.length === 0
            ? 'bg-muted-foreground/40'
            : failing.length > 0
              ? 'bg-warning'
              : 'bg-success'
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
          <h2 className="px-1 text-sm font-medium text-muted-foreground">Names</h2>
          <Button size="sm" variant="outline" onClick={() => setAdding(true)}>
            <Plus />
            Add
          </Button>
        </div>

        {records.length === 0 ? (
          <ListEmpty>
            No names yet. Remote access and port forwards both need one unless your address never
            changes.
          </ListEmpty>
        ) : (
          <List>
            {records.map((r) => {
              const s = states.get(r.name)
              return (
                <ListRow
                  key={r.name}
                  title={r.name}
                  subtitle={describe(r, s)}
                  trailing={<RecordBadge status={s} />}
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

function RecordBadge({ status: s }: { status?: RecordStatus }) {
  if (!s) return <Badge variant="secondary">Unknown</Badge>
  if (s.problems?.length || s.check_error || s.publish_error)
    return <Badge variant="warning">Not up to date</Badge>
  if (s.cgnat) return <Badge variant="warning">Behind shared NAT</Badge>
  if (!s.watched || !s.checked) return <Badge variant="secondary">Waiting</Badge>
  if (s.address && s.published_address === s.address)
    return <Badge variant="success">Up to date</Badge>
  return <Badge variant="secondary">Updating</Badge>
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
