import { AlertTriangle } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
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
import { useUplink } from '@/features/dial/queries'
import { useApplyLinkConfig, useInterfaces, useLinkConfig } from '@/features/link/queries'
import { ApiError } from '@/lib/api'
import type { GroupRow, InterfaceRow } from '@/lib/api-types'
import type { DhcpConfig, Pool } from '@/lib/config-types'
import { cn } from '@/lib/utils'

/**
 * Which interfaces this router has been given.
 *
 * olr refuses to serve anything on an interface nobody handed it (design.md
 * §3.4), so on a fresh install every range and every resolver is rejected until
 * one row here is switched on. Before this existed, that refusal arrived as a
 * validation error on a form field, naming a permission the UI gave you no way
 * to grant — and the DHCP and DNS landing pages now say so *before* the switch
 * is touched, linking to the page this card sits on.
 *
 * That page is routes/networks, which is where adoption belongs: the next thing
 * you do with an adopted interface is put a network on it, and that form is
 * directly below. It was a sub-page of DHCP until it moved, and the heading
 * above it is still the page's rather than this component's — a card heading
 * under a section heading would say "Interfaces" twice.
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
  // Read only to label the row that is the way out. This card owns adoption and
  // nothing else; the uplink is configured in its own section on the same page.
  const uplink = useUplink()

  /** A release held back because a pool still names the interface. */
  const [confirming, setConfirming] = useState<InterfaceRow | null>(null)

  // Locked until the stored config has been read, because write() needs it to
  // build a complete body and a switch that silently does nothing is worse than
  // one that is visibly not ready yet.
  const busy = disabled || apply.isPending || config.isPending
  const rows = interfaces.data?.interfaces ?? []
  const adopted = config.data?.adopted ?? []

  /**
   * Writes the new adopted set, announcing only what actually landed.
   *
   * The whole stored document goes on the wire, not just the field this card
   * owns. `PUT /api/link/config` is `BodyFull` — it *replaces* the module's
   * config — so a body of `{adopted}` alone deleted every network, and deleted
   * them silently in the worst possible way: PlanAddrs walks the groups, so an
   * empty list produced no kernel steps at all, and each interface kept the
   * address whose reason for existing had just been thrown away. Flipping one
   * switch here showed a success toast and lost the network on a different NIC.
   *
   * Nothing is written before the config has been read, for the same reason: an
   * undefined `config.data` spread into the body is a body with no networks in
   * it, which is the bug again with a narrower window.
   *
   * `done` is awaited rather than fired alongside the call: a toast published
   * before the mutation resolves would sit on screen next to the error toast
   * from the same failed write, telling the operator two different things about
   * one click.
   */
  async function write(next: string[], done: string) {
    if (!config.isSuccess) return
    try {
      await apply.mutateAsync({
        ...config.data,
        adopted: next.length ? next : undefined,
      })
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
    // Releasing an interface a pool still reaches does not undo the pool — it
    // makes the network invalid, and the pool on it with it, so the next apply
    // fails with a message about adoption. Asking first is cheaper than
    // explaining that.
    if (poolsOn(dhcp, interfaces.data?.groups, row.name).length > 0) {
      setConfirming(row)
      return
    }
    release(row)
  }

  return (
    <Card>
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
                role={roleOf(row, uplink.data?.uplink?.interface)}
                busy={!!busy}
                onToggle={(on) => toggle(row, on)}
              />
            ))}
          </ul>
        )}

        {interfaces.isSuccess && rows.length > 0 && adopted.length === 0 && (
          <p className="text-sm text-muted-foreground">
            Nothing is adopted yet. Switch on the interface facing your home
            network — usually the one with an address on it — and then add a
            network for it below. Switch on a second one if this router serves
            two subnets.
          </p>
        )}
      </CardContent>

      <ReleaseDialog
        row={confirming}
        pools={poolsOn(dhcp, interfaces.data?.groups, confirming?.name)}
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

/**
 * What an adopted interface is *for*, in one word.
 *
 * Two roles and they are mutually exclusive by construction: an interface
 * either carries a network this router serves or is the one way out, and the
 * server refuses an uplink on a network's member. Showing it here is what makes
 * that legible from the list rather than only from the two sections below it —
 * "which NIC is my WAN" was previously answerable only by reading addresses and
 * guessing.
 */
function roleOf(row: InterfaceRow, uplink: string | undefined): string | undefined {
  if (row.name === uplink) return 'uplink'
  return row.group
}

function InterfaceItem({
  row,
  role,
  busy,
  onToggle,
}: {
  row: InterfaceRow
  role?: string
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
          {role && <Badge variant="secondary">{role}</Badge>}
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

/**
 * Which pools would stop working if this interface were released.
 *
 * Two hops now rather than one: a pool names a network and the network names
 * the interface. The indirection is the point — it is what lets a network move
 * to a different NIC without every pool having to be re-pointed — but the
 * warning still has to reach all the way back to the interface the operator is
 * about to take away.
 */
function poolsOn(
  dhcp: DhcpConfig | undefined,
  groups: GroupRow[] | undefined,
  name: string | undefined,
) {
  if (!dhcp || !name) return []
  const onThisInterface = new Set(
    (groups ?? []).filter((g) => g.members.includes(name)).map((g) => g.name),
  )
  return (dhcp.pools ?? []).filter((p) => onThisInterface.has(p.group))
}

function ReleaseDialog({
  row,
  pools,
  onCancel,
  onConfirm,
}: {
  row: InterfaceRow | null
  pools: Pool[]
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
            <li key={p.group}>
              {p.group}
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
