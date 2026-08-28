import { AlertTriangle, Check, Pencil, Plus, X } from 'lucide-react'
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
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { PoolDialog } from '@/features/dhcp/pool-dialog'
import { ImpactBadge, PlanDiff, impactHint } from '@/features/dhcp/plan-preview'
import { useDhcpConfig, useDhcpLeases, useDhcpStatus } from '@/features/dhcp/queries'
import { ReservationDialog } from '@/features/dhcp/reservation-dialog'
import { useDhcpApply } from '@/features/dhcp/use-apply'
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
        <AlertTitle>Could not load the DHCP configuration</AlertTitle>
        <AlertDescription>{(config.error as Error).message}</AlertDescription>
      </Alert>
    )
  }

  const current = config.data

  /** Every edit is a whole new config sent through the same path. */
  const change = (next: DhcpConfig) => applier.submit(next)

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-center gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">DHCP</h1>
          <p className="text-sm text-muted-foreground">
            Hand out addresses to devices on your networks.
          </p>
        </div>
        <div className="ml-auto flex items-center gap-3">
          <Label htmlFor="dhcp-enabled" className="text-sm">
            {current.enabled ? 'Enabled' : 'Disabled'}
          </Label>
          <Switch
            id="dhcp-enabled"
            checked={current.enabled}
            disabled={applier.busy}
            onCheckedChange={(enabled) => change({ ...current, enabled })}
          />
        </div>
      </header>

      {applier.failure && (
        <Alert variant="destructive">
          <AlertTriangle />
          <AlertTitle>The change was applied only partly</AlertTitle>
          <AlertDescription className="space-y-2">
            <p>
              There is no rollback by design: the steps that succeeded have taken
              effect. Fix the cause and apply again — repeating is safe and
              finishes the job.
            </p>
            <ul className="space-y-1 font-mono text-xs">
              {applier.failure.steps?.map((step) => (
                <li key={step.description} className="flex gap-2">
                  {step.done ? (
                    <Check className="mt-0.5 size-3.5 shrink-0 text-emerald-500" aria-label="done" />
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
            <Button size="sm" variant="outline" onClick={applier.dismissFailure}>
              Dismiss
            </Button>
          </AlertDescription>
        </Alert>
      )}

      <StatusCard status={status.data} error={status.error as Error | null} />

      <PoolsCard config={current} onChange={change} busy={applier.busy} usage={leases.data?.usage} />

      <ReservationsCard config={current} onChange={change} busy={applier.busy} />

      <LeasesCard leases={leases.data} />

      <AdvancedCard config={current} onChange={change} busy={applier.busy} />

      <ConfirmDialog applier={applier} />
    </div>
  )
}

/* -------------------------------------------------------------------------- */

function StatusCard({
  status,
  error,
}: {
  status: ReturnType<typeof useDhcpStatus>['data']
  error: Error | null
}) {
  if (error) {
    return (
      <Alert variant="destructive">
        <AlertTriangle />
        <AlertTitle>Could not read status</AlertTitle>
        <AlertDescription>{error.message}</AlertDescription>
      </Alert>
    )
  }
  if (!status) return <Skeleton className="h-24 w-full" />

  return (
    <Card>
      <CardHeader>
        <CardTitle>Status</CardTitle>
        <CardDescription>
          Read from the system every time, never from a cache — so this is what
          is actually true right now.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4 sm:grid-cols-3">
        <Fact label="Server">
          {status.service ? (
            <Badge variant={status.service.active ? 'default' : 'secondary'}>
              {status.service.active ? 'Running' : 'Stopped'}
            </Badge>
          ) : (
            // "We could not tell" is a third answer and must not be flattened
            // into "stopped" (design.md §5.4).
            <span className="text-sm text-muted-foreground" title={status.service_error}>
              Unknown
            </span>
          )}
        </Fact>

        <Fact label="Configuration">
          {status.drift_error ? (
            <span className="text-sm text-muted-foreground">Unknown</span>
          ) : status.drifted ? (
            <Badge variant="secondary" className="bg-amber-500/15 text-amber-700 dark:text-amber-400">
              Not yet applied
            </Badge>
          ) : (
            <Badge variant="secondary" className="bg-emerald-500/15 text-emerald-700 dark:text-emerald-400">
              In sync
            </Badge>
          )}
        </Fact>

        <Fact label="Checked">
          <span className="text-sm text-muted-foreground">
            {new Date(status.as_of).toLocaleTimeString()}
          </span>
        </Fact>

        {status.service_error && (
          <p className="text-xs text-muted-foreground sm:col-span-3">{status.service_error}</p>
        )}
      </CardContent>
    </Card>
  )
}

function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1">
      <div className="text-xs font-medium text-muted-foreground uppercase tracking-wide">{label}</div>
      <div>{children}</div>
    </div>
  )
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
  usage?: { interface: string; active: number; size: number; percent: number }[]
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
        <CardTitle>Pools</CardTitle>
        <CardDescription>One address range per interface.</CardDescription>
        <CardAction>
          <Button
            size="sm"
            disabled={busy}
            onClick={() => {
              setEditing(undefined)
              setOpen(true)
            }}
          >
            <Plus className="size-4" aria-hidden /> Add pool
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent>
        {pools.length === 0 ? (
          <Empty>No pools yet. Add one to start serving addresses.</Empty>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Interface</TableHead>
                <TableHead>Range</TableHead>
                <TableHead>Lease</TableHead>
                <TableHead>IPv6</TableHead>
                <TableHead>In use</TableHead>
                <TableHead className="w-20" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {pools.map((pool) => {
                const u = usage?.find((x) => x.interface === pool.interface)
                return (
                  <TableRow key={pool.interface}>
                    <TableCell className="font-medium">{pool.interface}</TableCell>
                    <TableCell className="font-mono text-xs">
                      {pool.start} – {pool.end}
                    </TableCell>
                    <TableCell>{pool.lease_time ?? '12h'}</TableCell>
                    <TableCell>{pool.ra ?? 'off'}</TableCell>
                    <TableCell className="text-muted-foreground">
                      {u ? `${u.active} of ${u.size}` : '—'}
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label={`Edit ${pool.interface}`}
                        disabled={busy}
                        onClick={() => {
                          setEditing(pool)
                          setOpen(true)
                        }}
                      >
                        <Pencil className="size-4" aria-hidden />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label={`Remove ${pool.interface}`}
                        disabled={busy}
                        onClick={() => remove(pool.interface)}
                      >
                        <X className="size-4" aria-hidden />
                      </Button>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
      </CardContent>

      <PoolDialog
        key={editing?.interface ?? 'new'}
        open={open}
        onOpenChange={setOpen}
        initial={editing}
        onSubmit={upsert}
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

  return (
    <Card>
      <CardHeader>
        <CardTitle>Fixed addresses</CardTitle>
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
          <Empty>No fixed addresses.</Empty>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>MAC</TableHead>
                <TableHead>Address</TableHead>
                <TableHead>Hostname</TableHead>
                <TableHead className="w-20" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {reservations.map((r) => (
                <TableRow key={r.mac}>
                  <TableCell className="font-mono text-xs">{r.mac}</TableCell>
                  <TableCell className="font-mono text-xs">{r.ip}</TableCell>
                  <TableCell>{r.hostname ?? '—'}</TableCell>
                  <TableCell className="text-right">
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={`Edit ${r.mac}`}
                      disabled={busy}
                      onClick={() => {
                        setEditing(r)
                        setOpen(true)
                      }}
                    >
                      <Pencil className="size-4" aria-hidden />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={`Remove ${r.mac}`}
                      disabled={busy}
                      onClick={() =>
                        onChange({
                          ...config,
                          reservations: reservations.filter((x) => x.mac !== r.mac),
                        })
                      }
                    >
                      <X className="size-4" aria-hidden />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>

      <ReservationDialog
        key={editing?.mac ?? 'new'}
        open={open}
        onOpenChange={setOpen}
        initial={editing}
        onSubmit={upsert}
      />
    </Card>
  )
}

/* -------------------------------------------------------------------------- */

function LeasesCard({ leases }: { leases?: { leases: { ip: string; mac?: string; hostname?: string; expires: string | null; active: boolean }[]; as_of: string } }) {
  if (!leases) return <Skeleton className="h-32 w-full" />

  return (
    <Card>
      <CardHeader>
        <CardTitle>Current leases</CardTitle>
        <CardDescription>
          Observed, not configured — read fresh from the lease database as of{' '}
          {new Date(leases.as_of).toLocaleTimeString()}.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {leases.leases.length === 0 ? (
          <Empty>No devices hold a lease.</Empty>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Address</TableHead>
                <TableHead>MAC</TableHead>
                <TableHead>Name</TableHead>
                <TableHead>Expires</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {leases.leases.map((l) => (
                <TableRow key={`${l.ip}-${l.mac ?? ''}`}>
                  <TableCell className="font-mono text-xs">{l.ip}</TableCell>
                  <TableCell className="font-mono text-xs">{l.mac ?? '—'}</TableCell>
                  <TableCell>{l.hostname ?? '—'}</TableCell>
                  <TableCell className="text-muted-foreground">
                    {l.expires === null ? 'never' : new Date(l.expires).toLocaleString()}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
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
      <CardHeader>
        <CardTitle>Advanced</CardTitle>
        <CardDescription>
          Settings olr does not model, passed through to dnsmasq. Kept here
          rather than hand-edited into the daemon's file, so it stays part of
          your configuration. Directives olr renders itself are refused.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <Textarea
          aria-label="Extra dnsmasq configuration"
          className="min-h-28 font-mono text-xs"
          spellCheck={false}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
        />
        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" disabled={draft === stored} onClick={() => setDraft(stored)}>
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
            This will interrupt connected devices
          </DialogTitle>
          <DialogDescription>
            {pending && impactHint(pending.plan.impact)} Everything else applies
            the moment you change it; this one asks first.
          </DialogDescription>
        </DialogHeader>

        {pending && (
          <div className="max-h-[50vh] overflow-auto">
            <PlanDiff plan={pending.plan} />
          </div>
        )}

        <DialogFooter>
          {pending && <ImpactBadge impact={pending.plan.impact} className="mr-auto" />}
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

function Empty({ children }: { children: React.ReactNode }) {
  return <p className="py-6 text-center text-sm text-muted-foreground">{children}</p>
}

function PageSkeleton() {
  return (
    <div className="space-y-6">
      <Skeleton className="h-9 w-40" />
      <Skeleton className="h-24 w-full" />
      <Skeleton className="h-48 w-full" />
    </div>
  )
}
