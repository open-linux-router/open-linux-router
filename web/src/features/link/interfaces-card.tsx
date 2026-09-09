import { AlertTriangle } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { ListEmpty } from '@/components/ui/list'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { useApplyLinkConfig, useInterfaces, useLinkConfig } from '@/features/link/queries'
import { ApiError } from '@/lib/api'
import type { InterfaceRow } from '@/lib/api-types'
import type { DhcpConfig } from '@/lib/config-types'
import { cn } from '@/lib/utils'

/**
 * Which interfaces this router has been given.
 *
 * This is the first thing on the page for a reason: olr refuses to serve
 * anything on an interface nobody handed it (design.md §3.4), so on a fresh
 * install every other card on this screen will reject whatever you type until
 * one row here is switched on. Before this card existed that refusal arrived as
 * a validation error on a form field, naming a permission the UI gave you no
 * way to grant.
 *
 * It lives on the DHCP page rather than in a section of its own. Adoption is
 * read by dns and gateway too, so a case could be made for either — but this is
 * the page where it is *needed first*, and a fifth nav entry for one switch per
 * interface would be a section that is visited once and then never again.
 */
export function InterfacesCard({
  dhcp,
  disabled,
}: {
  /** The stored DHCP config, so a release can warn about the pool it breaks. */
  dhcp?: DhcpConfig
  disabled?: boolean
}) {
  const interfaces = useInterfaces()
  const config = useLinkConfig()
  const apply = useApplyLinkConfig()

  /** A release held back because a pool still names the interface. */
  const [confirming, setConfirming] = useState<InterfaceRow | null>(null)

  const busy = disabled || apply.isPending
  const rows = interfaces.data?.interfaces ?? []
  const adopted = config.data?.adopted ?? []

  /**
   * Writes the new adopted set, announcing only what actually landed.
   *
   * `done` is awaited rather than fired alongside the call: a toast published
   * before the mutation resolves would sit on screen next to the error toast
   * from the same failed write, telling the operator two different things about
   * one click.
   */
  async function write(next: string[], done: string) {
    try {
      await apply.mutateAsync({ adopted: next.length ? next : undefined })
      toast.success(done)
    } catch (error) {
      if (error instanceof ApiError) {
        toast.error(error.message, {
          description: error.problems.map((p) => p.message).join('\n') || undefined,
        })
      } else {
        toast.error(String(error))
      }
    }
  }

  function adopt(row: InterfaceRow) {
    void write([...adopted, row.name], `${row.name} is now yours to configure`)
  }

  function release(row: InterfaceRow) {
    void write(
      adopted.filter((name) => name !== row.name),
      `${row.name} handed back`,
    )
  }

  function toggle(row: InterfaceRow, on: boolean) {
    if (on) {
      adopt(row)
      return
    }
    // Releasing an interface a pool still names does not undo the pool — it
    // makes it invalid, so the next apply of anything on this page fails with a
    // message about adoption. Asking first is cheaper than explaining that.
    if (poolsOn(dhcp, row.name).length > 0) {
      setConfirming(row)
      return
    }
    release(row)
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Interfaces</CardTitle>
        <CardDescription>
          Hand an interface to this router before serving addresses on it.
          Switching one on changes nothing by itself — no address is set and no
          service is started.
        </CardDescription>
      </CardHeader>

      <CardContent className="space-y-3">
        {interfaces.isPending && <Skeleton className="h-32 w-full rounded-xl" />}

        {interfaces.isError && (
          <Alert variant="destructive">
            <AlertTriangle />
            <AlertTitle>Could not read this machine's interfaces</AlertTitle>
            <AlertDescription>{(interfaces.error as Error).message}</AlertDescription>
          </Alert>
        )}

        {interfaces.isSuccess && rows.length === 0 && (
          <ListEmpty>This machine reports no network interfaces.</ListEmpty>
        )}

        {rows.length > 0 && (
          <ul className="divide-y divide-border overflow-hidden rounded-xl border">
            {rows.map((row) => (
              <InterfaceItem
                key={row.name}
                row={row}
                busy={!!busy}
                onToggle={(on) => toggle(row, on)}
              />
            ))}
          </ul>
        )}

        {interfaces.isSuccess && rows.length > 0 && adopted.length === 0 && (
          <p className="text-sm text-muted-foreground">
            Nothing is adopted yet. Switch on the interface facing your home
            network — usually the one with an address on it.
          </p>
        )}
      </CardContent>

      <ReleaseDialog
        row={confirming}
        pools={poolsOn(dhcp, confirming?.name)}
        onCancel={() => setConfirming(null)}
        onConfirm={() => {
          if (confirming) release(confirming)
          setConfirming(null)
        }}
      />
    </Card>
  )
}

/* -------------------------------------------------------------------------- */

function InterfaceItem({
  row,
  busy,
  onToggle,
}: {
  row: InterfaceRow
  busy: boolean
  onToggle: (on: boolean) => void
}) {
  const id = `adopt-${row.name}`
  const state = describeState(row)

  return (
    <li className="flex min-h-14 items-center gap-3 bg-card px-4 py-2.5">
      <span
        className={cn('size-2 shrink-0 rounded-full', state.dot)}
        aria-hidden
      />

      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate font-mono text-sm font-medium">{row.name}</span>
          {state.badge && <Badge variant={state.badgeVariant}>{state.badge}</Badge>}
        </div>
        <div className="truncate text-[0.8rem] text-muted-foreground">
          {row.prefixes?.length ? row.prefixes.join(' · ') : state.detail}
        </div>
      </div>

      <Label htmlFor={id} className="sr-only">
        Give {row.name} to this router
      </Label>
      <Switch
        id={id}
        checked={row.adopted}
        // Loopback has no clients on it and nothing olr can usefully serve
        // there, so the server refuses it. Disabling the control says the same
        // thing without making the operator find out by being rejected.
        disabled={busy || row.loopback}
        onCheckedChange={onToggle}
      />
    </li>
  )
}

/**
 * The one sentence that distinguishes the states an operator has to act on
 * differently. "Up" alone cannot say "up with no cable in it", which is the most
 * common reason a freshly configured DHCP server appears to do nothing.
 */
function describeState(row: InterfaceRow): {
  dot: string
  detail: string
  badge?: string
  badgeVariant?: 'default' | 'warning' | 'destructive' | 'secondary'
} {
  if (!row.present) {
    return {
      dot: 'bg-destructive',
      detail: 'This machine has no interface with this name.',
      badge: 'Not found',
      badgeVariant: 'destructive',
    }
  }
  if (row.loopback) {
    return { dot: 'bg-muted-foreground/40', detail: 'The machine talking to itself.' }
  }
  if (!row.up) {
    return { dot: 'bg-muted-foreground/40', detail: 'Down.', badge: 'Down', badgeVariant: 'secondary' }
  }
  if (!row.running) {
    return {
      dot: 'bg-warning',
      detail: 'Up, but nothing is plugged in.',
      badge: 'No cable',
      badgeVariant: 'warning',
    }
  }
  if (!row.prefixes?.length) {
    return {
      dot: 'bg-warning',
      detail: 'No address. Give it one before serving a range here.',
      badge: 'No address',
      badgeVariant: 'warning',
    }
  }
  return { dot: 'bg-success', detail: 'Connected.' }
}

/** Which pools name this interface, for the release warning. */
function poolsOn(dhcp: DhcpConfig | undefined, name: string | undefined) {
  if (!dhcp || !name) return []
  return (dhcp.pools ?? []).filter((p) => p.interface === name)
}

function ReleaseDialog({
  row,
  pools,
  onCancel,
  onConfirm,
}: {
  row: InterfaceRow | null
  pools: { interface: string; start: string; end: string }[]
  onCancel: () => void
  onConfirm: () => void
}) {
  return (
    <Dialog open={row !== null} onOpenChange={(open) => !open && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <AlertTriangle className="size-4 text-warning" aria-hidden />
            An address range uses {row?.name}
          </DialogTitle>
          <DialogDescription>
            Taking {row?.name} back does not remove the range below, and does not
            stop the devices already using it. What it does is make the range
            invalid, so the next change you save on this page will be refused
            until you either adopt {row?.name} again or remove the range.
          </DialogDescription>
        </DialogHeader>

        <ul className="space-y-1 font-mono text-xs text-muted-foreground">
          {pools.map((p) => (
            <li key={p.interface}>
              {p.interface}: {p.start} – {p.end}
            </li>
          ))}
        </ul>

        <DialogFooter>
          <Button variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={onConfirm}>
            Take it back anyway
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
