import { Search, ShieldOff } from 'lucide-react'
import { useState } from 'react'

import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ListEmpty } from '@/components/ui/list'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { useDnsNames, useDnsQueries } from '@/features/dns/queries'
import { ApiError } from '@/lib/api'
import type { DnsStats, NameRow, QueryRow } from '@/lib/api-types'
import type { DnsConfig } from '@/lib/config-types'
import { cn } from '@/lib/utils'

/**
 * What the router actually saw.
 *
 * This is first on the page and not in a third tab, because it is the entire
 * reason olr owns :53 (docs/dns.md §4). The resolver leg on its own buys
 * nothing an operator can look at; these two lists are what it buys. A DNS page
 * that opens on a configuration form buries the most valuable thing in the
 * module.
 *
 * Both views answer the same question — "what did my network look up?" — so
 * they are one card with two faces rather than two cards competing for the top
 * of the page.
 */
type View = 'queries' | 'names'

export function ActivityCard({
  config,
  busy,
  onChange,
}: {
  config: DnsConfig
  busy: boolean
  onChange: (next: DnsConfig) => void
}) {
  const [view, setView] = useState<View>('queries')
  const [filter, setFilter] = useState('')
  const [blockedOnly, setBlockedOnly] = useState(false)

  const queries = useDnsQueries(view === 'queries')
  const names = useDnsNames(view === 'names')

  const active = view === 'queries' ? queries : names
  const stats = (active.data as { stats?: DnsStats } | undefined)?.stats
  const error = active.error

  return (
    <Card>
      <CardHeader>
        <CardTitle>What your network looked up</CardTitle>
        <CardDescription>
          Every device on this network resolves through here, so this is what
          they asked for — not a sample.
        </CardDescription>
        <CardAction className="flex items-center gap-2">
          <Label htmlFor="dns-querylog" className="text-xs text-muted-foreground">
            Keep a log
          </Label>
          <Switch
            id="dns-querylog"
            checked={config.query_log.enabled}
            disabled={busy}
            onCheckedChange={(enabled) =>
              onChange({ ...config, query_log: { ...config.query_log, enabled } })
            }
          />
        </CardAction>
      </CardHeader>

      <CardContent className="space-y-4">
        <Counters stats={stats} pending={active.isPending} />

        <div className="flex flex-wrap items-center gap-2">
          <Segmented
            value={view}
            onChange={(next) => {
              setView(next)
              setBlockedOnly(false)
            }}
          />
          <div className="relative ml-auto min-w-0 flex-1 sm:max-w-56">
            <Search
              className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground"
              aria-hidden
            />
            <Input
              aria-label={view === 'queries' ? 'Filter queries' : 'Filter addresses'}
              placeholder="Filter by name or device"
              className="h-9 pl-8"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
            />
          </div>
          {view === 'queries' && (
            <button
              type="button"
              aria-pressed={blockedOnly}
              onClick={() => setBlockedOnly((v) => !v)}
              className={cn(
                'flex h-9 shrink-0 items-center gap-1.5 rounded-lg border px-3 text-sm transition-colors',
                blockedOnly
                  ? 'border-destructive/40 bg-destructive/10 text-destructive'
                  : 'text-muted-foreground hover:bg-accent/50',
              )}
            >
              <ShieldOff className="size-3.5" aria-hidden />
              Blocked only
            </button>
          )}
        </div>

        {error ? (
          <RelayError error={error} />
        ) : active.isPending ? (
          <RowsSkeleton />
        ) : view === 'queries' ? (
          <QueryList
            rows={queries.data?.queries ?? []}
            filter={filter}
            blockedOnly={blockedOnly}
            logging={config.query_log.enabled}
          />
        ) : (
          <NameList rows={names.data?.names ?? []} filter={filter} />
        )}

        <Accounting stats={stats} shown={shownCount(view, queries.data?.queries, names.data?.names)} />
      </CardContent>
    </Card>
  )
}

function shownCount(view: View, queries?: QueryRow[], names?: NameRow[]) {
  return view === 'queries' ? (queries?.length ?? 0) : (names?.length ?? 0)
}

/* -------------------------------------------------------------------------- */

function Segmented({ value, onChange }: { value: View; onChange: (next: View) => void }) {
  const options: { value: View; label: string }[] = [
    { value: 'queries', label: 'Queries' },
    { value: 'names', label: 'Addresses seen' },
  ]

  // Buttons with aria-pressed rather than a tablist: there is no tab panel to
  // associate, and claiming the tab role without the wiring reads worse to a
  // screen reader than an honest pair of toggles.
  return (
    <div className="inline-flex shrink-0 rounded-lg bg-muted p-0.5">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          aria-pressed={value === o.value}
          onClick={() => onChange(o.value)}
          className={cn(
            'rounded-[calc(var(--radius)-2px)] px-3 py-1.5 text-sm transition-colors',
            value === o.value
              ? 'bg-background font-medium shadow-sm'
              : 'text-muted-foreground hover:text-foreground',
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

/* -------------------------------------------------------------------------- */

function Counters({ stats, pending }: { stats?: DnsStats; pending: boolean }) {
  if (pending && !stats) {
    return (
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <Skeleton key={i} className="h-16 rounded-lg" />
        ))}
      </div>
    )
  }
  if (!stats) return null

  const cells = [
    { label: 'Looked up', value: stats.queries, tone: '' },
    { label: 'Blocked', value: stats.blocked, tone: stats.blocked > 0 ? 'text-destructive' : '' },
    // Named for what it means rather than for the DNS rcode: a refusal is a
    // device that was not allowed to ask, which is an access-control fact.
    { label: 'Not allowed to ask', value: stats.refused, tone: '' },
    { label: 'No answer', value: stats.failed, tone: stats.failed > 0 ? 'text-warning' : '' },
  ]

  return (
    <div>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        {cells.map((c) => (
          <div key={c.label} className="rounded-lg border px-3 py-2">
            <div className={cn('text-xl font-semibold tabular-nums', c.tone)}>
              {c.value.toLocaleString()}
            </div>
            <div className="text-xs text-muted-foreground">{c.label}</div>
          </div>
        ))}
      </div>
      {/* The counters reset when the relay does, and saying so beats implying a
          history the log does not have. */}
      <p className="pt-2 text-xs text-muted-foreground">
        Since {new Date(stats.since).toLocaleString()}, when the DNS server last
        started.
      </p>
    </div>
  )
}

/**
 * The gaps.
 *
 * Rendered even at zero, deliberately. internal/dns/print.go makes the same
 * choice for the same reason: a counter that only appears when it is non-zero
 * teaches an operator that it does not exist, and then a log that silently shed
 * entries under load looks complete.
 */
function Accounting({ stats, shown }: { stats?: DnsStats; shown: number }) {
  if (!stats) return null
  const truncated = stats.held > shown

  return (
    <div className="space-y-1 text-xs text-muted-foreground">
      <p>
        The log holds {stats.held.toLocaleString()} of {stats.capacity.toLocaleString()} entries
        {truncated && `, and this page is showing the most recent ${shown.toLocaleString()}`}.
      </p>
      {(stats.dropped > 0 || stats.unparsed > 0) && (
        <p className="text-warning">
          Not accounted for: {stats.dropped.toLocaleString()} observation
          {stats.dropped === 1 ? '' : 's'} dropped under load,{' '}
          {stats.unparsed.toLocaleString()} response{stats.unparsed === 1 ? '' : 's'} that could not
          be read. Neither cost anybody an answer — both were passed through untouched — but both
          are gaps in the list above.
        </p>
      )}
    </div>
  )
}

/* -------------------------------------------------------------------------- */

/**
 * A relay that is not answering.
 *
 * This must never render as "no queries". An empty list and a dead DNS server
 * look identical to a reader and mean opposite things — one is a quiet evening,
 * the other is the whole network unable to resolve.
 */
function RelayError({ error }: { error: unknown }) {
  const unreachable = error instanceof ApiError && error.status === 503

  return (
    <div className="rounded-xl border border-dashed px-4 py-8 text-center">
      <p className="text-sm font-medium">
        {unreachable ? 'The DNS server is not answering' : 'Could not read the log'}
      </p>
      <p className="mx-auto max-w-md pt-1 text-sm text-muted-foreground">
        {unreachable
          ? 'Nothing is being recorded, and devices on this network may not be able to look anything up. The status above says which part is down.'
          : ((error as Error)?.message ?? 'Unknown error.')}
      </p>
    </div>
  )
}

function RowsSkeleton() {
  return (
    <div className="divide-y overflow-hidden rounded-xl border">
      {[0, 1, 2, 3, 4].map((i) => (
        <div key={i} className="flex items-center gap-3 px-3 py-2.5">
          <Skeleton className="h-3 w-12 shrink-0" />
          <div className="flex-1 space-y-1.5">
            <Skeleton className="h-3.5 w-40 max-w-full" />
            <Skeleton className="h-3 w-24" />
          </div>
        </div>
      ))}
    </div>
  )
}

/* -------------------------------------------------------------------------- */

function QueryList({
  rows,
  filter,
  blockedOnly,
  logging,
}: {
  rows: QueryRow[]
  filter: string
  blockedOnly: boolean
  logging: boolean
}) {
  const needle = filter.trim().toLowerCase()
  const shown = rows.filter(
    (q) =>
      (!blockedOnly || q.blocked) &&
      (!needle ||
        q.name.toLowerCase().includes(needle) ||
        q.client.toLowerCase().includes(needle) ||
        (q.policy ?? '').toLowerCase().includes(needle)),
  )

  if (shown.length === 0) {
    // The three empty states mean different things and must not share wording:
    // the log is off, nothing has been asked yet, or the filter excluded
    // everything.
    if (!logging) {
      return (
        <ListEmpty>
          The log is off, so nothing is being kept. Queries are still answered
          and still counted above.
        </ListEmpty>
      )
    }
    if (rows.length > 0) {
      return <ListEmpty>Nothing matches that filter.</ListEmpty>
    }
    return <ListEmpty>Nothing has been looked up yet.</ListEmpty>
  }

  return (
    <ul className="divide-y overflow-hidden rounded-xl border">
      {shown.map((q, i) => (
        <li key={`${q.at}-${q.client}-${q.name}-${i}`} className="flex items-center gap-3 px-3 py-2">
          {/* 24-hour regardless of locale, matching `olr dns queries`. A log
              column has to be one fixed width or the names beside it stop
              lining up, and a 12-hour clock wraps its AM onto a second line at
              this size. */}
          <span className="w-14 shrink-0 font-mono text-xs text-muted-foreground tabular-nums">
            {new Date(q.at).toLocaleTimeString(undefined, {
              hour: '2-digit',
              minute: '2-digit',
              second: '2-digit',
              hour12: false,
            })}
          </span>
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm">{q.name}</div>
            <div className="truncate text-xs text-muted-foreground">
              {q.client} · {q.type}
              {q.answers?.length ? ` → ${q.answers.join(', ')}` : ''}
            </div>
          </div>
          <QueryResult query={q} />
        </li>
      ))}
    </ul>
  )
}

/**
 * The outcome, and — when it was blocked — the rule that blocked it.
 *
 * The policy name is not decoration. "Why can this device not reach that site"
 * is the question the query log exists to answer, and a blocked row without the
 * rule behind it sends the operator hunting through every policy by hand.
 */
function QueryResult({ query }: { query: QueryRow }) {
  if (query.blocked) {
    return (
      <Badge variant="destructive" className="shrink-0">
        Blocked{query.policy ? ` · ${query.policy}` : ''}
      </Badge>
    )
  }
  if (query.rcode && query.rcode !== 'NOERROR') {
    return (
      <Badge variant="warning" className="shrink-0">
        {query.rcode}
      </Badge>
    )
  }
  return null
}

/* -------------------------------------------------------------------------- */

function NameList({ rows, filter }: { rows: NameRow[]; filter: string }) {
  const needle = filter.trim().toLowerCase()
  const shown = rows.filter(
    (n) =>
      !needle ||
      n.name.toLowerCase().includes(needle) ||
      n.address.toLowerCase().includes(needle) ||
      n.client.toLowerCase().includes(needle),
  )

  if (shown.length === 0) {
    return (
      <ListEmpty>
        {rows.length > 0 ? 'Nothing matches that filter.' : 'No addresses observed yet.'}
      </ListEmpty>
    )
  }

  return (
    <ul className="divide-y overflow-hidden rounded-xl border">
      {shown.map((n, i) => {
        // The tail of the CNAME chain is the organisation signal: it is why a
        // device that asked for one name shows up talking to a CDN.
        const via = n.chain?.length ? n.chain[n.chain.length - 1] : undefined
        return (
          <li key={`${n.client}-${n.name}-${n.address}-${i}`} className="px-3 py-2">
            <div className="flex items-baseline gap-2">
              <span className="min-w-0 flex-1 truncate text-sm">{n.name}</span>
              <span className="shrink-0 font-mono text-xs text-muted-foreground">{n.address}</span>
            </div>
            <div className="truncate text-xs text-muted-foreground">
              asked by {n.client}
              {via && via !== n.name && ` · via ${via}`}
            </div>
          </li>
        )
      })}
    </ul>
  )
}
