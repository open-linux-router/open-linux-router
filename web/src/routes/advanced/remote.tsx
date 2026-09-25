import { AlertTriangle, KeyRound, Plus } from 'lucide-react'
import { useState } from 'react'

import { BlockerAlerts } from '@/components/layout/blockers'
import { StatusStrip } from '@/components/layout/status-strip'
import { SubPage } from '@/components/layout/sub-page'
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
import { ClientConfigDialog } from '@/features/remote/client-config'
import { ImpactBadge, PlanDiff, PlanReasons, impactHint } from '@/features/remote/impact'
import { PeerDialog } from '@/features/remote/peer-dialog'
import { ProxyCard } from '@/features/remote/proxy-card'
import {
  remoteChange,
  useRemoteConfig,
  useRemoteStatus,
  useReapplyRemote,
  useShadowsocksStatus,
  type RemoteChangeRequest,
} from '@/features/remote/queries'
import { SettingsDialog } from '@/features/remote/settings-dialog'
import { useRemoteApply } from '@/features/remote/use-apply'
import type { RemotePeer, RemoteStatus } from '@/lib/api-types'

export function RemotePage() {
  const config = useRemoteConfig()
  const status = useRemoteStatus()
  const proxy = useShadowsocksStatus()
  const applier = useRemoteApply()
  const reapply = useReapplyRemote()

  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<RemotePeer | null>(null)
  const [settings, setSettings] = useState(false)

  if (config.isPending) return <PageSkeleton />
  if (config.isError) {
    return (
      <Alert variant="destructive">
        <AlertTriangle />
        <AlertTitle>Could not load remote access</AlertTitle>
        <AlertDescription>{(config.error as Error).message}</AlertDescription>
      </Alert>
    )
  }

  const wg = config.data.wireguard
  const peers = status.data?.peers ?? []
  const change = (c: RemoteChangeRequest) => applier.submit(c)

  return (
    <SubPage section="/advanced" slug="remote">
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
                    {s.done ? 'done   ' : 'failed '} {s.description}
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

      {/* Ordered by what has to be true first. Without wireguard-tools nothing
          below it can work, so the blocker comes before the switch that would
          fail against it. */}
      <BlockerAlerts blockers={status.data?.blockers} module="remote" />

      {status.data?.interface_foreign && (
        <Alert variant="destructive">
          <AlertTriangle />
          <AlertTitle>{status.data.interface} belongs to something else</AlertTitle>
          <AlertDescription>
            An interface with that name already exists on this box and is not WireGuard&rsquo;s, so
            olr will not touch it. Give the tunnel a different name, or remove the other one.
          </AlertDescription>
        </Alert>
      )}

      {status.data?.drifted && !status.data.interface_foreign && (
        <Alert>
          <AlertTriangle />
          <AlertTitle>The router is not doing what these settings say</AlertTitle>
          <AlertDescription className="space-y-3">
            <p>
              The tunnel is not set up the way it is configured — after a reboot, or because
              something removed it. Putting it back does not disturb anyone already connected.
            </p>
            <Button
              size="sm"
              variant="outline"
              disabled={reapply.isPending}
              onClick={() => reapply.mutate()}
            >
              Put it back
            </Button>
          </AlertDescription>
        </Alert>
      )}

      <StatusStrip
        headline={headline(wg.enabled, peers)}
        detail={
          wg.enabled
            ? 'Your devices reach everything on your network from anywhere, as if they were at home.'
            : 'These devices are saved but switched off, so none of them can get in.'
        }
        dot={!wg.enabled ? 'bg-muted-foreground/40' : dotFor(status.data)}
        control={{
          id: 'remote-enabled',
          label: 'Allow devices in',
          checked: wg.enabled,
          busy: applier.busy,
          onChange: (enabled) => change(remoteChange.settings({ enabled })),
        }}
      />

      <ReachCard status={status.data} onEdit={() => setSettings(true)} />

      <Card>
        <CardHeader>
          <CardTitle>Devices</CardTitle>
          <CardDescription>
            One entry per device. Each gets its own key, so removing one leaves the others alone.
          </CardDescription>
          <CardAction>
            <Button size="sm" variant="outline" onClick={() => setAdding(true)}>
              <Plus />
              Add
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent>
          <PeerList peers={peers} onEdit={setEditing} />
        </CardContent>
      </Card>

      {/* Below the tunnel, deliberately. The two are not alternatives and the
          order says which one an operator usually wants: getting *in* to your own
          network is the reason this page exists, and borrowing its way *out* is
          the narrower second thing. Putting them side by side as equals would
          invite picking one. */}
      <ProxyCard status={proxy.data} applier={applier} />

      <PeerDialog
        open={adding}
        onOpenChange={setAdding}
        subnet={status.data?.subnet}
        onSubmit={(peer) =>
          change(
            remoteChange.savePeer(peer.name, {
              routes: peer.routes,
              public_key: peer.public_key,
            }),
          )
        }
      />
      {editing && (
        <PeerDialog
          open
          onOpenChange={(open) => !open && setEditing(null)}
          initial={editing}
          subnet={status.data?.subnet}
          onSubmit={(peer) => change(remoteChange.savePeer(editing.name, { routes: peer.routes }))}
          onRemove={() => {
            setEditing(null)
            change(remoteChange.removePeer(editing.name))
          }}
        />
      )}
      <SettingsDialog
        open={settings}
        onOpenChange={setSettings}
        initial={config.data}
        onSubmit={async (endpoint, wireguard) => {
          // Two owners, so up to two requests — and the endpoint first,
          // because it is the one the daemon may refuse. Sending the tunnel's
          // fields first would leave them applied against an endpoint the
          // operator then declined to change.
          if (endpoint !== (config.data.endpoint ?? '')) {
            if (!(await change(remoteChange.endpoint(endpoint)))) return
          }
          if (Object.keys(wireguard).length > 0) {
            await change(remoteChange.settings(wireguard))
          }
        }}
      />

      {applier.issued && (
        <ClientConfigDialog result={applier.issued} onClose={applier.dismissIssued} />
      )}
      <ConfirmDialog applier={applier} />
    </SubPage>
  )
}

function headline(enabled: boolean, peers: RemotePeer[]): string {
  if (!enabled) return 'Off'
  if (peers.length === 0) return 'Nobody can get in yet'
  const online = peers.filter((p) => p.online).length
  if (online > 0) return `${online} of ${peers.length} connected`
  return `${peers.length} device${peers.length === 1 ? '' : 's'} can get in`
}

/**
 * Green only when the tunnel is actually up.
 *
 * "Switched on" and "running" are different claims here in a way they are not
 * on a file-rendering module: this one's configuration lives in the kernel, so
 * a reboot leaves the settings intact and the tunnel gone. Painting that green
 * would be the screen agreeing with itself rather than with the box.
 */
function dotFor(status?: RemoteStatus): string {
  if (!status || !status.kernel_known) return 'bg-muted-foreground/40'
  if (status.interface_foreign) return 'bg-destructive'
  return status.interface_present && status.interface_up ? 'bg-success' : 'bg-destructive'
}

/**
 * Where devices dial, and the key they check.
 *
 * The public key is on screen rather than hidden behind an "advanced" anything.
 * It is not a secret — it is in every configuration this router has ever
 * handed out — and it is the one value somebody completing a file by hand, or
 * comparing against what their phone shows, actually needs.
 */
function ReachCard({ status, onEdit }: { status?: RemoteStatus; onEdit: () => void }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <KeyRound className="size-4 text-muted-foreground" />
          How devices reach this router
          {status && !status.endpoint && <Badge variant="destructive">Not set</Badge>}
        </CardTitle>
        <CardDescription>
          Set once. Every device&rsquo;s configuration is written from it.
        </CardDescription>
        <CardAction>
          <Button size="sm" variant="outline" onClick={onEdit}>
            Settings
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className="space-y-1 text-sm text-muted-foreground">
        {status?.endpoint ? (
          <p>
            Devices dial <span className="font-mono">{status.endpoint}</span> on UDP/
            {status.listen_port}, and land on <span className="font-mono">{status.subnet}</span>.
          </p>
        ) : (
          <p>
            No public address is set, so no device can be given a working configuration yet. It is
            the one thing this router cannot work out for itself.
          </p>
        )}
        {status?.public_key && (
          <p className="truncate font-mono text-xs">{status.public_key}</p>
        )}
      </CardContent>
    </Card>
  )
}

function PeerList({
  peers,
  onEdit,
}: {
  peers: RemotePeer[]
  onEdit: (peer: RemotePeer) => void
}) {
  if (peers.length === 0) {
    return (
      <ListEmpty>
        No device can dial in yet. Add one and you get a configuration to scan into it — a phone, a
        laptop, anything that should reach your network from outside.
      </ListEmpty>
    )
  }

  return (
    <List>
      {peers.map((p) => (
        <ListRow
          key={p.name}
          title={p.name}
          subtitle={describePeer(p)}
          trailing={<PeerBadge peer={p} />}
          onSelect={() => onEdit(p)}
        />
      ))}
    </List>
  )
}

function describePeer(p: RemotePeer): string {
  const sends =
    p.routes === 'everything' ? 'sends everything home' : 'reaches your network'
  return `${p.address ?? '—'} · ${sends}`
}

/**
 * Three states, not two, and the third is the one that matters.
 *
 * A device that has *never* connected almost always means the configuration was
 * never imported, or the port is not reachable from outside — a setup problem
 * with somewhere to look. A device last seen yesterday is working and asleep.
 * Collapsing them into "offline" would hide the only failure this module
 * reliably produces on a first install.
 */
function PeerBadge({ peer }: { peer: RemotePeer }) {
  if (peer.unknown) return <Badge variant="secondary">Unknown</Badge>
  if (peer.online) return <Badge variant="success">Connected</Badge>
  if (!peer.last_handshake) return <Badge variant="warning">Never connected</Badge>
  return <Badge variant="secondary">{lastSeen(peer.last_handshake)}</Badge>
}

function lastSeen(iso: string): string {
  const minutes = Math.round((Date.now() - new Date(iso).getTime()) / 60_000)
  if (minutes < 60) return `${minutes} min ago`
  const hours = Math.round(minutes / 60)
  if (hours < 48) return `${hours}h ago`
  return `${Math.round(hours / 24)}d ago`
}

/**
 * The one thing that interrupts an operator (design.md §6.3).
 *
 * Everything else applies on the click. This appears only when the plan came
 * back `disruptive`, which on this screen means somebody loses their way into
 * the network from outside it — or a configuration already on their phone stops
 * working. Neither is undone by trying again.
 */
function ConfirmDialog({ applier }: { applier: ReturnType<typeof useRemoteApply> }) {
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
      <Skeleton className="h-28 w-full" />
      <Skeleton className="h-56 w-full" />
    </div>
  )
}
