import { AlertTriangle, Plus } from 'lucide-react'
import { useState } from 'react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Disclosure } from '@/components/ui/disclosure'
import { List, ListEmpty, ListRow } from '@/components/ui/list'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { ExitDialog } from '@/features/routing/exit-dialog'
import { ImpactBadge, PlanDiff, PlanReasons, impactHint } from '@/features/routing/plan-preview'
import { useRoutingConfig, useRoutingStatus, useRoutingTraffic } from '@/features/routing/queries'
import { useRoutingApply } from '@/features/routing/use-apply'
import type { AssignmentStatus, ExitStatus, RoutingTraffic, Usage } from '@/lib/api-types'
import type { Exit, RoutingConfig } from '@/lib/config-types'

// Sentinel values for the two choices that are not an exit name.
//
// The **leading space is load-bearing** and is not a typo to tidy away: an exit
// name is trimmed by Normalize and required non-empty by Validate, so no real
// name can begin with one, which is what makes these impossible to collide with
// an exit somebody actually called "inherit". The empty string is not available
// — the Select treats it as "nothing chosen" — so a distinguishable non-empty
// value is needed for each.
const INHERIT = ' inherit'
const DIRECT = ' direct'

export function RoutingPage() {
  const config = useRoutingConfig()
  const status = useRoutingStatus()
  const traffic = useRoutingTraffic()
  const applier = useRoutingApply()

  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<Exit | null>(null)

  if (config.isPending) return <PageSkeleton />
  if (config.isError) {
    return (
      <Alert variant="destructive">
        <AlertTriangle />
        <AlertTitle>Could not load the internet settings</AlertTitle>
        <AlertDescription>{(config.error as Error).message}</AlertDescription>
      </Alert>
    )
  }

  const current = config.data
  const exits = current.exits ?? []

  /** Every edit is a whole new config sent through the same path. */
  const change = (next: RoutingConfig) => applier.submit(next)

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl font-semibold tracking-tight">Internet</h1>
        <p className="text-sm text-muted-foreground">
          Choose how each network reaches the internet. Everything follows one setting unless you
          change it for a network of its own.
        </p>
      </header>

      {applier.blocked && (
        <Alert variant="destructive" role="alert">
          <AlertTriangle />
          <AlertTitle>Something else is managing routing on this box</AlertTitle>
          <AlertDescription className="space-y-3">
            <p>{applier.blocked.blocked}</p>
            {applier.blocked.foreign?.length ? (
              <Disclosure summary="The rules we found">
                <pre className="overflow-auto font-mono text-xs leading-relaxed">
                  {applier.blocked.foreign
                    .map((f) => `priority ${f.priority}  ${f.family}  table ${f.table}  ${f.selector}`)
                    .join('\n')}
                </pre>
              </Disclosure>
            ) : null}
            <Button size="sm" variant="outline" onClick={applier.dismissBlocked}>
              Dismiss
            </Button>
          </AlertDescription>
        </Alert>
      )}

      {applier.failure && (
        <Alert variant="destructive" role="alert">
          <AlertTriangle />
          <AlertTitle>Only part of the change went through</AlertTitle>
          <AlertDescription className="space-y-3">
            <p>
              The steps that finished have already taken effect and will not be undone. Fixing the
              cause and applying again is safe — it picks up where this left off.
            </p>
            <Disclosure summary="Which steps ran">
              <ul className="space-y-1 font-mono text-xs">
                {applier.failure.steps?.map((s, i) => (
                  <li key={i}>
                    {s.done ? 'done   ' : s.error ? 'failed ' : 'skipped'} {s.description}
                    {s.error ? ` — ${s.error}` : ''}
                  </li>
                ))}
              </ul>
            </Disclosure>
            <Button size="sm" variant="outline" onClick={applier.dismissFailure}>
              Dismiss
            </Button>
          </AlertDescription>
        </Alert>
      )}

      {status.data && !status.data.known && (
        <Alert>
          <AlertTriangle />
          <AlertTitle>These settings are saved but not in force</AlertTitle>
          <AlertDescription>
            This router could not read its own network configuration, so nothing below is actually
            running. On Linux this usually means the daemon is missing permission to change
            routing.
          </AlertDescription>
        </Alert>
      )}

      {status.data?.drifted && (
        <Alert>
          <AlertTriangle />
          <AlertTitle>The router is not doing what these settings say</AlertTitle>
          <AlertDescription>
            Something changed the routing outside olr. Saving any change here puts it back.
          </AlertDescription>
        </Alert>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Internet via</CardTitle>
          <CardDescription>
            The setting every network follows unless it has one of its own.
          </CardDescription>
          <CardAction>
            <Switch
              aria-label="Apply these settings"
              checked={current.enabled}
              disabled={applier.busy}
              onCheckedChange={(enabled) => change({ ...current, enabled })}
            />
          </CardAction>
        </CardHeader>
        <CardContent className="space-y-4">
          <Select
            value={current.default || DIRECT}
            disabled={applier.busy}
            onValueChange={(v) => change({ ...current, default: !v || v === DIRECT ? '' : v })}
          >
            <SelectTrigger className="w-full sm:w-72">
              {/* The trigger shows the raw value unless it is given a label,
                  which is fine for an exit name and wrong for the sentinel —
                  it would read as a literal " direct". */}
              <SelectValue>
                {(v: string) => (v === DIRECT ? 'This router’s own connection' : v)}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={DIRECT}>This router&rsquo;s own connection</SelectItem>
              {exits.map((e) => (
                <SelectItem key={e.name} value={e.name}>
                  {e.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {!current.enabled && (
            <p className="text-sm text-muted-foreground">
              These settings are saved but switched off, so every network is using this
              router&rsquo;s own connection.
            </p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Networks</CardTitle>
          <CardDescription>
            Change one network without affecting the rest. The most specific setting wins.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <NetworkList
            config={current}
            status={status.data?.assignments}
            busy={applier.busy}
            onChange={change}
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Ways out</CardTitle>
          <CardDescription>
            Somewhere this router can hand traffic to — another box on your network, a VPN or proxy
            connection, or nowhere at all.
          </CardDescription>
          <CardAction>
            <Button size="sm" variant="outline" onClick={() => setAdding(true)}>
              <Plus />
              Add
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent>
          <ExitList exits={exits} status={status.data?.exits} onEdit={setEditing} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Usage</CardTitle>
          <CardDescription>
            How much each device has sent and received since this router started.
          </CardDescription>
          <CardAction>
            <Switch
              aria-label="Count how much each device uses"
              checked={current.stats ?? true}
              disabled={applier.busy}
              onCheckedChange={(stats) => change({ ...current, stats })}
            />
          </CardAction>
        </CardHeader>
        <CardContent>
          <TrafficList traffic={traffic.data} enabled={current.stats ?? true} />
        </CardContent>
      </Card>

      {status.data?.foreign?.length ? (
        <Card>
          <CardHeader>
            <CardTitle>Routing this router does not manage</CardTitle>
            {/* design.md §3.4: pretending we are the only actor is a bug.
                Somebody else's rules are shown so a hand-rolled setup is
                legible rather than mysterious. */}
            <CardDescription>
              Rules another program put in place. olr leaves them alone.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <pre className="overflow-auto font-mono text-xs leading-relaxed">
              {status.data.foreign
                .map((f) => `priority ${f.priority}  ${f.family}  table ${f.table}  ${f.selector}`)
                .join('\n')}
            </pre>
          </CardContent>
        </Card>
      ) : null}

      <ExitDialog
        open={adding}
        onOpenChange={setAdding}
        onSubmit={(exit) => change({ ...current, exits: [...exits, exit] })}
      />
      {editing && (
        <ExitDialog
          open
          onOpenChange={(open) => !open && setEditing(null)}
          initial={editing}
          onSubmit={(exit) =>
            change({
              ...current,
              exits: exits.map((e) => (e.name === editing.name ? exit : e)),
            })
          }
          onRemove={() => {
            setEditing(null)
            change({ ...current, exits: exits.filter((e) => e.name !== editing.name) })
          }}
        />
      )}

      <ConfirmDialog applier={applier} />
    </div>
  )
}

/**
 * Who used the bandwidth, and through which way out.
 *
 * The limits underneath come from the server rather than being written here, so
 * the CLI and any agent reading the same endpoint carry the same caveats. They
 * are shown rather than tucked behind a disclosure because each one explains a
 * number being *smaller* than expected, and the first question a surprising
 * number produces is "is this broken?".
 */
function TrafficList({ traffic, enabled }: { traffic?: RoutingTraffic; enabled: boolean }) {
  if (!enabled) {
    return <ListEmpty>Counting is off, so nothing is being recorded.</ListEmpty>
  }
  if (!traffic) {
    return <Skeleton className="h-24 w-full" />
  }
  if (!traffic.counting) {
    return (
      <ListEmpty>
        Counting is on, but this router could not read the counters — so nothing here
        is real yet.
      </ListEmpty>
    )
  }
  if (traffic.usage.length === 0) {
    return (
      <ListEmpty>
        Nothing counted yet. Counting starts when the router forwards traffic, and the
        totals reset when it restarts.
      </ListEmpty>
    )
  }

  return (
    <div className="space-y-4">
      <List>
        {traffic.usage.map((u) => (
          <ListRow
            key={`${u.address}/${u.exit}/${u.unknown ?? false}`}
            title={u.address}
            subtitle={describeUsageExit(u)}
            trailing={`${formatBytes(u.up_bytes)} up · ${formatBytes(u.down_bytes)} down`}
          />
        ))}
      </List>
      {traffic.limits?.length ? (
        <div className="text-[0.8rem] text-muted-foreground">
          <p className="font-medium">What these numbers do not include</p>
          <ul className="mt-1 space-y-1">
            {traffic.limits.map((l) => (
              <li key={l} className="flex gap-2">
                <span aria-hidden>·</span>
                {l}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </div>
  )
}

function describeUsageExit(u: Usage): string {
  if (u.unknown) return 'A way out that was removed'
  // Named rather than left blank, so the row reads as an answer and not a gap.
  if (!u.exit) return 'Not routed by this router'
  return `Via ${u.exit}`
}

/**
 * Powers of 1024 with the short suffixes, matching every other tool on the box.
 * A second convention would make two numbers about the same traffic disagree.
 */
function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  const units = ['KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  let value = n / 1024
  let i = 0
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024
    i++
  }
  return `${value.toFixed(1)} ${units[i]}`
}

/**
 * One row per network, each with the sentence docs/gateway.md §1.3 settles on.
 *
 * The effective value carries its source, which is the whole reason
 * inheritance is usable here at all: *Clash · from the box-wide setting* tells
 * an operator both what is happening and where to go to change it, without
 * anyone having to simulate a rule list.
 */
function NetworkList({
  config,
  status,
  busy,
  onChange,
}: {
  config: RoutingConfig
  status?: AssignmentStatus[]
  busy: boolean
  onChange: (next: RoutingConfig) => void
}) {
  const assignments = config.interfaces ?? []
  const exits = config.exits ?? []

  if (assignments.length === 0) {
    return (
      <ListEmpty>
        No networks yet. Networks appear here once this router knows about them.
      </ListEmpty>
    )
  }

  function set(iface: string, value: string | null) {
    const exit = !value || value === INHERIT ? '' : value
    onChange({
      ...config,
      interfaces: assignments.map((a) => (a.interface === iface ? { ...a, exit } : a)),
    })
  }

  // Not a List: these rows carry an interactive control, and List's trailing
  // slot is `hidden sm:block` — which would leave the setting unreachable on a
  // phone, on the one screen whose whole purpose is changing it.
  return (
    <ul className="divide-y overflow-hidden rounded-xl border">
      {assignments.map((a) => {
        const row = status?.find((s) => s.interface === a.interface)
        return (
          <li
            key={a.interface}
            className="flex flex-col gap-2 bg-card px-4 py-3 sm:flex-row sm:items-center sm:gap-3"
          >
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-medium">{a.interface}</div>
              <div className="truncate text-[0.8rem] text-muted-foreground">
                {describeAssignment(row)}
              </div>
            </div>
            <Select
              value={a.exit ? a.exit : INHERIT}
              disabled={busy}
              onValueChange={(v) => set(a.interface, v)}
            >
              <SelectTrigger
                className="w-full sm:w-56"
                aria-label={`Internet via, for ${a.interface}`}
              >
                <SelectValue>
                  {(v: string) => (v === INHERIT ? 'Follow the setting above' : v)}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={INHERIT}>Follow the setting above</SelectItem>
                {exits.map((e) => (
                  <SelectItem key={e.name} value={e.name}>
                    {e.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </li>
        )
      })}
    </ul>
  )
}

function describeAssignment(row?: AssignmentStatus): string {
  if (!row) return ''
  if (row.reason) return row.reason
  const via = row.exit || 'this router’s own connection'
  return row.source === 'default' ? `${via} — from the setting above` : via
}

function ExitList({
  exits,
  status,
  onEdit,
}: {
  exits: Exit[]
  status?: ExitStatus[]
  onEdit: (exit: Exit) => void
}) {
  if (exits.length === 0) {
    return (
      <ListEmpty>
        {/* docs/gateway.md §10: the empty state should guide, and it is where
            the SOCKS5 question gets answered before somebody tries it. */}
        Nothing here yet. Add the box or connection you want traffic to go
        through. A SOCKS5 or HTTP proxy on its own cannot be used — it only
        carries traffic that knows to ask it.
      </ListEmpty>
    )
  }

  return (
    <List>
      {exits.map((e) => {
        const row = status?.find((s) => s.name === e.name)
        return (
          <ListRow
            key={e.name}
            title={e.name}
            subtitle={describeExit(e, row)}
            trailing={row ? <HealthBadge row={row} /> : undefined}
            onSelect={() => onEdit(e)}
          />
        )
      })}
    </List>
  )
}

function HealthBadge({ row }: { row: ExitStatus }) {
  // "Not checked" rather than "up": claiming health nobody measured is exactly
  // where design.md §5.6 says faults go to hide.
  if (!row.probed) return <Badge variant="secondary">Not checked</Badge>
  return row.up ? <Badge variant="success">Working</Badge> : <Badge variant="destructive">Down</Badge>
}

function describeExit(e: Exit, row?: ExitStatus): string {
  const where =
    e.via.kind === 'blocked'
      ? 'Blocks traffic'
      : e.via.kind === 'interface'
        ? `Out ${e.via.interface}`
        : `To ${e.via.next_hop}`
  const used = row?.used_by?.length ? ` · used by ${row.used_by.join(', ')}` : ''

  // The badge beside this row is `hidden sm:block`, so on a phone it is the
  // only thing that would say an exit has stopped working — which is the most
  // operationally important fact on the screen and the worst one to drop at the
  // width most people will read it at. Said in the subtitle too, where it
  // survives, rather than only in the badge.
  const down = row && row.probed && !row.up ? 'Not responding · ' : ''
  return down + where + used
}

/**
 * The one thing that interrupts an operator (design.md §6.3).
 *
 * Everything else applies on the click. This appears only when the plan came
 * back `disruptive`, which on this screen most often means the change would
 * route the operator's own connection somewhere else — the single outcome they
 * cannot recover from by clicking again.
 */
function ConfirmDialog({ applier }: { applier: ReturnType<typeof useRoutingApply> }) {
  const held = applier.confirming
  if (!held) return null

  return (
    <Dialog open onOpenChange={(open) => !open && applier.cancel()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            Apply this change?
            <ImpactBadge impact={held.plan.impact} />
          </DialogTitle>
          <DialogDescription>{impactHint(held.plan.impact)}</DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <PlanReasons plan={held.plan} />
          <Disclosure summary="What would change">
            <PlanDiff plan={held.plan} />
          </Disclosure>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={applier.cancel}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={applier.confirm} disabled={applier.busy}>
            Apply anyway
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function PageSkeleton() {
  return (
    <div className="space-y-6">
      <Skeleton className="h-8 w-40" />
      <Skeleton className="h-40 w-full" />
      <Skeleton className="h-56 w-full" />
    </div>
  )
}
