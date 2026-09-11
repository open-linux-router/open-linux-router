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
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { ForwardDialog } from '@/features/firewall/forward-dialog'
import { ImpactBadge, PlanDiff, PlanReasons, impactHint } from '@/features/firewall/plan-preview'
import {
  firewallChange,
  useFirewallConfig,
  useFirewallStatus,
  type FirewallChange,
} from '@/features/firewall/queries'
import { useFirewallApply } from '@/features/firewall/use-apply'
import type { ForwardStatus } from '@/lib/api-types'
import type { Forward } from '@/lib/config-types'
import { formatBytes } from '@/lib/utils'

export function FirewallPage() {
  const config = useFirewallConfig()
  const status = useFirewallStatus()
  const applier = useFirewallApply()

  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<Forward | null>(null)

  if (config.isPending) return <PageSkeleton />
  if (config.isError) {
    return (
      <Alert variant="destructive">
        <AlertTriangle />
        <AlertTitle>Could not load the port forwards</AlertTitle>
        <AlertDescription>{(config.error as Error).message}</AlertDescription>
      </Alert>
    )
  }

  const current = config.data
  const forwards = current.forwards ?? []

  /** Every edit names the one thing it changes and sends that. */
  const change = (c: FirewallChange) => applier.submit(c)

  return (
    <div className="space-y-6">
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
          <AlertTitle>These forwards are saved but not in force</AlertTitle>
          <AlertDescription>
            This router could not read its own firewall rules, so nothing below is actually running.
            On Linux this usually means the daemon is missing permission to change them.
          </AlertDescription>
        </Alert>
      )}

      {status.data?.drifted && (
        <Alert>
          <AlertTriangle />
          <AlertTitle>The router is not doing what these settings say</AlertTitle>
          <AlertDescription>
            Something changed the firewall rules outside olr. Saving any change here puts them back.
          </AlertDescription>
        </Alert>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Port forwarding</CardTitle>
          <CardDescription>
            Something on the internet connects to this router on a port, and reaches one device on
            your network instead.
          </CardDescription>
          <CardAction>
            <Switch
              aria-label="Apply these port forwards"
              checked={current.enabled}
              disabled={applier.busy}
              onCheckedChange={(enabled) => change(firewallChange.settings({ enabled }))}
            />
          </CardAction>
        </CardHeader>
        {!current.enabled && (
          <CardContent>
            <p className="text-sm text-muted-foreground">
              These forwards are saved but switched off, so nothing from outside is reaching a
              device here.
            </p>
          </CardContent>
        )}
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Forwards</CardTitle>
          <CardDescription>
            Each one is a port on this router and the device it leads to. The count says whether
            anything has actually arrived.
          </CardDescription>
          <CardAction>
            <Button size="sm" variant="outline" onClick={() => setAdding(true)}>
              <Plus />
              Add
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent className="space-y-4">
          <ForwardList forwards={forwards} status={status.data?.forwards} onEdit={setEditing} />
          {forwards.length > 0 && status.data?.known && (
            // Said once under the list rather than on every row: a small number
            // here is usually explained by a recent edit, and an operator
            // reading "never" wants to know that before they go and debug their
            // ISP.
            <p className="text-[0.8rem] text-muted-foreground">
              Counts start again whenever a forward is added, changed or removed, and when the
              router restarts.
            </p>
          )}
        </CardContent>
      </Card>

      {status.data?.foreign?.length ? (
        <Card>
          <CardHeader>
            <CardTitle>Something else is filtering forwarded traffic</CardTitle>
            {/* design.md §3.4: pretending we are the only actor is a bug. This
                one cannot be fixed from here — in nftables a drop is final — so
                it is reported with enough detail to go and find it. */}
            <CardDescription>
              Another program on this box drops traffic passing through the router by default. olr
              cannot override that, so a forward here may be correct and still not reach.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <pre className="overflow-auto font-mono text-xs leading-relaxed">
              {status.data.foreign
                .map((f) => `${f.family} ${f.table}  chain ${f.chain}  policy ${f.policy}`)
                .join('\n')}
            </pre>
            <p className="text-[0.8rem] text-muted-foreground">
              It may well be accepting this traffic already — olr cannot tell. The count beside each
              forward is what settles it.
            </p>
          </CardContent>
        </Card>
      ) : null}

      <ForwardDialog
        open={adding}
        onOpenChange={setAdding}
        onSubmit={(forward) => change(firewallChange.saveForward(forward.name, forward))}
      />
      {editing && (
        <ForwardDialog
          open
          onOpenChange={(open) => !open && setEditing(null)}
          initial={editing}
          // The path carries the name this forward had; the body carries what it
          // should become. When those differ it is a rename, and the daemon
          // keeps the slot — and so the counter — across it.
          onSubmit={(forward) => change(firewallChange.saveForward(editing.name, forward))}
          onRemove={() => {
            setEditing(null)
            change(firewallChange.removeForward(editing.name))
          }}
        />
      )}

      <ConfirmDialog applier={applier} />
    </div>
  )
}

function ForwardList({
  forwards,
  status,
  onEdit,
}: {
  forwards: Forward[]
  status?: ForwardStatus[]
  onEdit: (forward: Forward) => void
}) {
  if (forwards.length === 0) {
    return (
      <ListEmpty>
        Nothing is being forwarded in. Add one to let something on the internet reach a device on
        your network — a web server, a game, a camera.
      </ListEmpty>
    )
  }

  return (
    <List>
      {forwards.map((f) => {
        const row = status?.find((s) => s.name === f.name)
        return (
          <ListRow
            key={f.name}
            title={f.name}
            subtitle={describeForward(f, row)}
            trailing={row ? <ArrivedBadge row={row} /> : undefined}
            onSelect={() => onEdit(f)}
          />
        )
      })}
    </List>
  )
}

/**
 * Whether anything has arrived, which is the first question when a port does not
 * work — and it has three answers, not two.
 *
 * "Nothing yet" says the rule is in the kernel and no packet has matched it,
 * which points outward: the ISP, the modem, or somebody else's filter. "Not
 * counted" says we could not read the counter, which points at this box.
 * Collapsing them into a zero would send an operator to debug the wrong half.
 */
function ArrivedBadge({ row }: { row: ForwardStatus }) {
  if (!row.counted) return <Badge variant="secondary">Not counted</Badge>
  if (row.packets === 0) return <Badge variant="secondary">Nothing yet</Badge>
  return <Badge variant="success">{formatBytes(row.bytes)} in</Badge>
}

/** The schema word for `both` is not a label; the other two already are. */
const PROTOCOL_LABEL: Record<string, string> = {
  '': 'TCP',
  tcp: 'TCP',
  udp: 'UDP',
  both: 'TCP+UDP',
}

function describeForward(f: Forward, row?: ForwardStatus): string {
  const proto = PROTOCOL_LABEL[f.protocol ?? ''] ?? 'TCP'
  const hairpin = f.hairpin === false ? ' · not from inside' : ''

  // The badge beside this row is `hidden sm:block`, so on a phone it is the only
  // thing that would say nothing has ever arrived — which is the most
  // operationally important fact on the screen and the worst one to drop at the
  // width most people will read it at. Said in front of the subtitle, where it
  // survives both the breakpoint and the truncation.
  const quiet = row?.counted && row.packets === 0 ? 'Nothing has arrived · ' : ''
  return `${quiet}${f.in} ${proto} ${f.port} → ${f.to}${hairpin}`
}

/**
 * The one thing that interrupts an operator (design.md §6.3).
 *
 * Everything else applies on the click. This appears only when the plan came
 * back `disruptive`, which here means a forward that is currently carrying
 * traffic is being taken away — the connections through it will not be refused,
 * they will stop mid-stream.
 */
function ConfirmDialog({ applier }: { applier: ReturnType<typeof useFirewallApply> }) {
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
      <Skeleton className="h-28 w-full" />
      <Skeleton className="h-56 w-full" />
    </div>
  )
}
