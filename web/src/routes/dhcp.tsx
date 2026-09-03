import { AlertTriangle, Check, Plus, X } from 'lucide-react'
import { useEffect, useState } from 'react'

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
import { Label } from '@/components/ui/label'
import { List, ListEmpty, ListRow } from '@/components/ui/list'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { PoolDialog } from '@/features/dhcp/pool-dialog'
import { PlanDiff, PlanReasons, impactHint } from '@/features/dhcp/impact'
import { useDhcpConfig, useDhcpLeases, useDhcpStatus } from '@/features/dhcp/queries'
import { ReservationDialog } from '@/features/dhcp/reservation-dialog'
import { useDhcpApply } from '@/features/dhcp/use-apply'
import type { DhcpLeases, DhcpStatus, PoolUsage } from '@/lib/api-types'
import type { DhcpConfig, Pool, Reservation } from '@/lib/config-types'

export function DhcpPage() {
  const config = useDhcpConfig()
  const status = useDhcpStatus()
  const leases = useDhcpLeases()
  const applier = useDhcpApply()

  if (config.isPending) return <PageSkeleton />
  if (config.isError) {
    return (
      <Alert variant="destructive">
        <AlertTriangle />
        <AlertTitle>Could not load the address settings</AlertTitle>
        <AlertDescription>{(config.error as Error).message}</AlertDescription>
      </Alert>
    )
  }

  const current = config.data

  /** Every edit is a whole new config sent through the same path. */
  const change = (next: DhcpConfig) => applier.submit(next)

  const connected = leases.data?.leases.filter((l) => l.active).length

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl font-semibold tracking-tight">Addresses</h1>
        <p className="text-sm text-muted-foreground">
          Devices that join your network get an address from this router.
        </p>
      </header>

      {applier.failure && (
        <Alert variant="destructive" role="alert">
          <AlertTriangle />
          <AlertTitle>Only part of the change went through</AlertTitle>
          <AlertDescription className="space-y-3">
            <p>
              The steps that finished have already taken effect and will not be
              undone. Fixing the cause and applying again is safe — it picks up
              where this left off.
            </p>
            <Disclosure summary="Which steps ran">
              <ul className="space-y-1 pt-1 font-mono text-xs">
                {applier.failure.steps?.map((step) => (
                  <li key={step.description} className="flex gap-2">
                    {step.done ? (
                      <Check className="mt-0.5 size-3.5 shrink-0 text-success" aria-label="done" />
                    ) : (
                      <X className="mt-0.5 size-3.5 shrink-0 text-destructive" aria-label="failed" />
                    )}
                    <span>
                      {step.description}
                      {step.error && <span className="block opacity-80">{step.error}</span>}
                    </span>
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

      <StatusCard
        config={current}
        status={status.data}
        error={status.error as Error | null}
        connected={connected}
        busy={applier.busy}
        onChange={change}
      />

      <PoolsCard config={current} onChange={change} busy={applier.busy} usage={leases.data?.usage} />

      <ReservationsCard config={current} onChange={change} busy={applier.busy} />

      <LeasesCard leases={leases.data} />

      <AdvancedCard config={current} onChange={change} busy={applier.busy} />

      <ConfirmDialog applier={applier} />
    </div>
  )
}

/* -------------------------------------------------------------------------- */

/**
 * The one thing an operator wants on arriving: is this working, and can I turn
 * it off?
 *
 * This replaces a three-column grid of "Server / Configuration / Checked". Two
 * of those were facts about the daemon rather than about the network, and the
 * third was a clock. All three are still here, one disclosure down.
 */
function StatusCard({
  config,
  status,
  error,
  connected,
  busy,
  onChange,
}: {
  config: DhcpConfig
  status?: DhcpStatus
  error: Error | null
  connected?: number
  busy: boolean
  onChange: (next: DhcpConfig) => void
}) {
  const summary = describeStatus(config, status, error)

  return (
    <Card>
      <CardContent className="space-y-4">
        <div className="flex items-center gap-3">
          <span className={`size-2.5 shrink-0 rounded-full ${summary.dot}`} aria-hidden />
          <div className="min-w-0 flex-1">
            <div className="font-medium">{summary.headline}</div>
            <div className="text-sm text-muted-foreground">
              {config.enabled && connected !== undefined
                ? `${connected} device${connected === 1 ? '' : 's'} connected`
                : summary.detail}
            </div>
          </div>
          <Label htmlFor="dhcp-enabled" className="sr-only">
            Hand out addresses
          </Label>
          <Switch
            id="dhcp-enabled"
            checked={config.enabled}
            disabled={busy}
            onCheckedChange={(enabled) => onChange({ ...config, enabled })}
          />
        </div>

        {status?.drifted && !status.drift_error && (
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="warning">Not yet applied</Badge>
            <span className="text-sm text-muted-foreground">
              The running server is behind these settings.
            </span>
          </div>
        )}

        <Disclosure summary="Technical details">
          <dl className="space-y-1.5 pt-1 text-xs text-muted-foreground">
            <Detail term="Service">
              {status?.service
                ? `${status.service.unit} — ${status.service.state}${
                    status.service.sub_state ? ` (${status.service.sub_state})` : ''
                  }`
                : (status?.service_error ?? 'unknown')}
            </Detail>
            <Detail term="Matches the running server">
              {status?.drift_error
                ? `unknown — ${status.drift_error}`
                : status?.drifted
                  ? 'no'
                  : 'yes'}
            </Detail>
            <Detail term="Read at">
              {status ? new Date(status.as_of).toLocaleTimeString() : '—'}
            </Detail>
            <p className="pt-1">
              Read from the system on every request and never cached, so this is
              what is true right now rather than what was last written.
            </p>
          </dl>
        </Disclosure>
      </CardContent>
    </Card>
  )
}

function Detail({ term, children }: { term: string; children: React.ReactNode }) {
  return (
    <div className="flex gap-2">
      <dt className="shrink-0">{term}:</dt>
      <dd className="min-w-0 break-words font-mono">{children}</dd>
    </div>
  )
}

/**
 * "We could not tell" is a third answer and must not be flattened into
 * "stopped" (design.md §5.4) — hence the `Status unknown` branch, which is
 * reached when the daemon could not read the unit at all, as distinct from
 * reading it as inactive.
 */
function describeStatus(
  config: DhcpConfig,
  status: DhcpStatus | undefined,
  error: Error | null,
): { headline: string; detail: string; dot: string } {
  if (error) {
    return { headline: 'Cannot reach the router', detail: error.message, dot: 'bg-destructive' }
  }
  if (!status) {
    return {
      headline: 'Checking…',
      detail: 'Reading the current state.',
      dot: 'bg-muted-foreground/40',
    }
  }
  if (!config.enabled) {
    return {
      headline: 'Off',
      detail: 'Devices will not get an address from this router.',
      dot: 'bg-muted-foreground/40',
    }
  }
  if (!status.service) {
    return {
      headline: 'Status unknown',
      detail: status.service_error ?? 'The router could not read the service state.',
      dot: 'bg-warning',
    }
  }
  return status.service.active
    ? { headline: 'Handing out addresses', detail: 'Working normally.', dot: 'bg-success' }
    : {
        headline: 'Not running',
        detail: 'Turned on, but the service is stopped.',
        dot: 'bg-destructive',
      }
}

/* -------------------------------------------------------------------------- */

function PoolsCard({
  config,
  onChange,
  busy,
  usage,
}: {
  config: DhcpConfig
  onChange: (next: DhcpConfig) => void
  busy: boolean
  usage?: PoolUsage[]
}) {
  const [editing, setEditing] = useState<Pool | undefined>(undefined)
  const [open, setOpen] = useState(false)
  const pools = config.pools ?? []

  function upsert(pool: Pool) {
    const rest = pools.filter((p) => p.interface !== pool.interface)
    onChange({ ...config, pools: [...rest, pool] })
  }

  function remove(iface: string) {
    onChange({ ...config, pools: pools.filter((p) => p.interface !== iface) })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Address ranges</CardTitle>
        <CardDescription>The addresses this router is allowed to hand out.</CardDescription>
        <CardAction>
          <Button
            size="sm"
            disabled={busy}
            onClick={() => {
              setEditing(undefined)
              setOpen(true)
            }}
          >
            <Plus className="size-4" aria-hidden /> Add
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent>
        {pools.length === 0 ? (
          <ListEmpty>No ranges yet. Add one so devices can get an address.</ListEmpty>
        ) : (
          <List>
            {pools.map((pool) => {
              const u = usage?.find((x) => x.interface === pool.interface)
              return (
                <ListRow
                  key={pool.interface}
                  title={pool.interface}
                  subtitle={`${pool.start} – ${pool.end}`}
                  trailing={u ? `${u.active} of ${u.size} in use` : undefined}
                  onSelect={
                    busy
                      ? undefined
                      : () => {
                          setEditing(pool)
                          setOpen(true)
                        }
                  }
                />
              )
            })}
          </List>
        )}
      </CardContent>

      <PoolDialog
        key={editing?.interface ?? 'new'}
        open={open}
        onOpenChange={setOpen}
        initial={editing}
        onSubmit={upsert}
        onRemove={editing ? () => remove(editing.interface) : undefined}
      />
    </Card>
  )
}

/* -------------------------------------------------------------------------- */

function ReservationsCard({
  config,
  onChange,
  busy,
}: {
  config: DhcpConfig
  onChange: (next: DhcpConfig) => void
  busy: boolean
}) {
  const [editing, setEditing] = useState<Reservation | undefined>(undefined)
  const [open, setOpen] = useState(false)
  const reservations = config.reservations ?? []

  function upsert(reservation: Reservation) {
    const key = reservation.mac.toLowerCase()
    const rest = reservations.filter((r) => r.mac.toLowerCase() !== key)
    onChange({ ...config, reservations: [...rest, reservation] })
  }

  function remove(mac: string) {
    onChange({ ...config, reservations: reservations.filter((x) => x.mac !== mac) })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Reserved addresses</CardTitle>
        <CardDescription>Devices that should always get the same address.</CardDescription>
        <CardAction>
          <Button
            size="sm"
            disabled={busy}
            onClick={() => {
              setEditing(undefined)
              setOpen(true)
            }}
          >
            <Plus className="size-4" aria-hidden /> Add
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent>
        {reservations.length === 0 ? (
          <ListEmpty>No reserved addresses.</ListEmpty>
        ) : (
          <List>
            {reservations.map((r) => (
              <ListRow
                key={r.mac}
                title={r.hostname ?? r.ip}
                subtitle={r.hostname ? `${r.ip} · ${r.mac}` : r.mac}
                onSelect={
                  busy
                    ? undefined
                    : () => {
                        setEditing(r)
                        setOpen(true)
                      }
                }
              />
            ))}
          </List>
        )}
      </CardContent>

      <ReservationDialog
        key={editing?.mac ?? 'new'}
        open={open}
        onOpenChange={setOpen}
        initial={editing}
        onSubmit={upsert}
        onRemove={editing ? () => remove(editing.mac) : undefined}
      />
    </Card>
  )
}

/* -------------------------------------------------------------------------- */

function LeasesCard({ leases }: { leases?: DhcpLeases }) {
  if (!leases) return <LeasesSkeleton />

  // Active first, then named before unnamed, then alphabetically. Sorting on
  // the displayed label alone would file a nameless device under its address
  // and float it above every named one, since digits sort before letters.
  const sorted = [...leases.leases].sort(
    (a, b) =>
      Number(b.active) - Number(a.active) ||
      Number(Boolean(b.hostname)) - Number(Boolean(a.hostname)) ||
      (a.hostname ?? a.ip).localeCompare(b.hostname ?? b.ip, undefined, { numeric: true }),
  )

  return (
    <Card>
      <CardHeader>
        <CardTitle>Connected devices</CardTitle>
        <CardDescription>
          What is actually on the network right now, not what was configured.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {sorted.length === 0 ? (
          <ListEmpty>Nothing has asked for an address yet.</ListEmpty>
        ) : (
          <List>
            {sorted.map((l) => (
              <ListRow
                key={`${l.ip}-${l.mac ?? ''}`}
                title={l.hostname ?? l.ip}
                subtitle={l.hostname ? `${l.ip} · ${l.mac ?? 'no MAC'}` : (l.mac ?? 'no MAC')}
                trailing={expiryLabel(l.expires, l.active)}
              />
            ))}
          </List>
        )}
        <p className="text-xs text-muted-foreground">
          Read {new Date(leases.as_of).toLocaleTimeString()}.
        </p>
      </CardContent>
    </Card>
  )
}

/** Plain wording for a lease clock. An operator wants "how long", not a date. */
function expiryLabel(expires: string | null, active: boolean): string {
  if (!active) return 'Not active'
  if (expires === null) return 'Never expires'
  const ms = new Date(expires).getTime() - Date.now()
  if (ms <= 0) return 'Expired'
  const minutes = Math.round(ms / 60_000)
  if (minutes < 60) return `${Math.max(1, minutes)} min left`
  const hours = Math.round(minutes / 60)
  if (hours < 48) return `${hours}h left`
  return `${Math.round(hours / 24)} days left`
}

/* -------------------------------------------------------------------------- */

function AdvancedCard({
  config,
  onChange,
  busy,
}: {
  config: DhcpConfig
  onChange: (next: DhcpConfig) => void
  busy: boolean
}) {
  const stored = config.extra_dnsmasq_conf ?? ''
  const [draft, setDraft] = useState(stored)

  // Free text, so this is the one field that does not apply as you type.
  useEffect(() => setDraft(stored), [stored])

  return (
    <Card>
      <CardContent>
        {/* Open by default only when there is something in it — an operator who
            has used this wants to see it; one who never has should not have to
            scroll past a dnsmasq textarea to reach the end of the page. */}
        <Disclosure summary="Advanced" defaultOpen={stored !== ''}>
          <div className="space-y-3 pt-2">
            <p className="text-sm text-muted-foreground">
              Settings this router does not model, passed straight through to
              dnsmasq. Kept here rather than hand-edited into the daemon's file,
              so they stay part of your configuration. Directives the router
              writes itself are refused.
            </p>
            <Textarea
              aria-label="Extra dnsmasq configuration"
              className="min-h-28 font-mono text-xs"
              spellCheck={false}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
            />
            <div className="flex justify-end gap-2">
              <Button
                variant="ghost"
                size="sm"
                disabled={draft === stored}
                onClick={() => setDraft(stored)}
              >
                Revert
              </Button>
              <Button
                size="sm"
                disabled={busy || draft === stored}
                onClick={() => onChange({ ...config, extra_dnsmasq_conf: draft || undefined })}
              >
                Save
              </Button>
            </div>
          </div>
        </Disclosure>
      </CardContent>
    </Card>
  )
}

/* -------------------------------------------------------------------------- */

function ConfirmDialog({ applier }: { applier: ReturnType<typeof useDhcpApply> }) {
  const pending = applier.confirming

  return (
    <Dialog open={pending !== null} onOpenChange={(open) => !open && applier.cancel()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <AlertTriangle className="size-4 text-destructive" aria-hidden />
            This will disconnect devices
          </DialogTitle>
          <DialogDescription>
            {pending && impactHint(pending.plan.impact)} Every other change
            applies the moment you make it — this one asks first.
          </DialogDescription>
        </DialogHeader>

        {/* The reasons stay in front of the operator; the diff is the evidence
            behind them. design.md §6.4 needs the diff to stay inspectable, so
            it is one click away rather than the first thing in the dialog. */}
        {pending && (
          <div className="space-y-2">
            <PlanReasons plan={pending.plan} />
            <Disclosure summary="Show exactly what will change">
              <div className="max-h-[45vh] overflow-auto pt-2">
                <PlanDiff plan={pending.plan} />
              </div>
            </Disclosure>
          </div>
        )}

        <DialogFooter>
          {/* No impact badge here: the title already says "This will disconnect
              devices", and the only plan that reaches this dialog is the
              disruptive one. The per-file badges inside the diff still earn
              their place, because a change can span files of mixed impact. */}
          <Button variant="ghost" onClick={applier.cancel}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={applier.confirm}>
            Apply anyway
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/* -------------------------------------------------------------------------- */

/**
 * Skeletons match the height and rhythm of what replaces them, so the page does
 * not jump when the data lands.
 */
function PageSkeleton() {
  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <Skeleton className="h-8 w-40" />
        <Skeleton className="h-4 w-72 max-w-full" />
      </div>
      <Skeleton className="h-32 w-full rounded-xl" />
      <Skeleton className="h-56 w-full rounded-xl" />
      <Skeleton className="h-56 w-full rounded-xl" />
    </div>
  )
}

function LeasesSkeleton() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Connected devices</CardTitle>
        <CardDescription>
          What is actually on the network right now, not what was configured.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="divide-y overflow-hidden rounded-xl border">
          {[0, 1, 2].map((i) => (
            <div key={i} className="flex min-h-14 items-center gap-3 px-4 py-2.5">
              <div className="flex-1 space-y-1.5">
                <Skeleton className="h-4 w-32" />
                <Skeleton className="h-3 w-48 max-w-full" />
              </div>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  )
}
