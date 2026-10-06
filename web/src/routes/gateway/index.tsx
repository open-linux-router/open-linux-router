import { AlertTriangle, ChevronRight } from 'lucide-react'
import { Link } from 'react-router'

import { SettingsList } from '@/components/layout/settings-list'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { ApplyOutcome, useGatewayEditor } from '@/features/gateway/editor'
import { DIRECT } from '@/features/gateway/network-list'
import { InterfaceVisual, interfaceState } from '@/features/link/interface-visual'
import { useInterfaces } from '@/features/link/queries'
import { useUplink } from '@/features/dial/queries'
import { gatewayChange, useGatewayStatus, useReapplyGateway } from '@/features/gateway/queries'

/**
 * The gateway, on the page you land on.
 *
 * Two things, in the order docs/gateway.md §1.3 puts them: the one setting
 * every network follows, and the networks that follow it. Ways out, usage and
 * somebody else's routing rules are each a page of their own — they are what
 * you configure once, where this is what you look at.
 */
export function GatewayPage() {
  const { config, busy, change, applier, gate } = useGatewayEditor()
  const status = useGatewayStatus()
  const reapply = useReapplyGateway()
  const interfaces = useInterfaces()
  const uplink = useUplink()

  if (!config) return gate
  const exits = config.exits ?? []
  const foreign = status.data?.foreign ?? []

  return (
    <div className="space-y-6">
      <ApplyOutcome applier={applier} />

      {status.data && !status.data.known && (
        <Alert>
          <AlertTriangle />
          <AlertTitle>These settings are saved but not in force</AlertTitle>
          <AlertDescription>
            This router could not read its own network configuration, so nothing below is
            actually running. On Linux this usually means the daemon is missing permission to
            change routing.
          </AlertDescription>
        </Alert>
      )}

      {status.data?.drifted && (
        <Alert>
          <AlertTriangle />
          <AlertTitle>The router is not doing what these settings say</AlertTitle>
          <AlertDescription className="space-y-3">
            <p>
              Something changed the gateway outside olr — or an earlier change stopped
              halfway. Putting it back re-programs the kernel from these settings and
              changes none of them.
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

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(18rem,0.42fr)]">
        <Card>
          <CardHeader>
            <CardTitle>Interfaces</CardTitle>
            <CardDescription>Connections on this router. Open one to inspect its link, addresses, and settings.</CardDescription>
          </CardHeader>
          <CardContent>
            {interfaces.isError ? (
              <Alert variant="destructive" role="alert"><AlertTriangle /><AlertTitle>Could not load interfaces</AlertTitle><AlertDescription>{interfaces.error.message}</AlertDescription></Alert>
            ) : interfaces.isPending ? (
              <p className="text-sm text-muted-foreground">Loading interfaces…</p>
            ) : !interfaces.data.interfaces.length ? (
              <p className="text-sm text-muted-foreground">This machine reports no interfaces.</p>
            ) : (
              <ul className="divide-y rounded-xl border">
                {interfaces.data.interfaces.map((row) => {
                  const isUplink = row.name === uplink.data?.uplink?.interface
                  const state = interfaceState(row)
                  return <li key={row.name}>
                    <Link to={`/gateway/interfaces/${encodeURIComponent(row.name)}`} className="flex min-h-20 items-center gap-4 px-4 py-3 transition-colors hover:bg-muted/40 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring">
                      <InterfaceVisual row={row} uplink={isUplink} />
                      <span className="min-w-0 flex-1">
                        <span className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5"><span className="font-mono text-sm font-semibold">{row.name}</span><span className="text-xs text-muted-foreground">{isUplink ? 'Internet uplink' : row.network ? `${row.network} network` : row.loopback ? 'Local only' : row.adopted ? 'Available to configure' : 'Not managed'}</span></span>
                        <span className="mt-1 block truncate text-xs text-muted-foreground">{row.prefixes?.join(' · ') || 'No IP address'}</span>
                      </span>
                      <span className={`hidden shrink-0 text-xs font-medium sm:block ${state.tone}`}>{state.label}</span>
                      <ChevronRight className="size-4 shrink-0 text-muted-foreground" aria-hidden />
                    </Link>
                  </li>
                })}
              </ul>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Internet via</CardTitle>
            <CardDescription>Default route for devices on your networks. An interface can choose a different way out in its details.</CardDescription>
            <CardAction>
              <Switch aria-label="Apply gateway settings" checked={config.enabled} disabled={busy} onCheckedChange={(enabled) => change(gatewayChange.settings({ enabled }))} />
            </CardAction>
          </CardHeader>
          <CardContent className="space-y-3">
            <Select value={config.default || DIRECT} disabled={busy} onValueChange={(v) => change(gatewayChange.settings({ default: !v || v === DIRECT ? '' : v }))}>
              <SelectTrigger className="w-full" aria-label="Default internet route"><SelectValue>{(v: string) => v === DIRECT ? 'This router’s own connection' : v}</SelectValue></SelectTrigger>
              <SelectContent><SelectItem value={DIRECT}>This router&rsquo;s own connection</SelectItem>{exits.map((e) => <SelectItem key={e.name} value={e.name}>{e.name}</SelectItem>)}</SelectContent>
            </Select>
            {!config.enabled && <p className="text-xs text-muted-foreground">Gateway rules are off. Networks use this router&rsquo;s connection.</p>}
          </CardContent>
        </Card>
      </div>

      <SettingsList
        section="/gateway"
        rows={[
          {
            slug: 'networks',
            value: interfaces.data ? `${interfaces.data.networks.length} configured` : undefined,
          },
          {
            slug: 'exits',
            value: exits.length ? exits.map((e) => e.name).join(', ') : 'None yet',
          },
          { slug: 'usage', value: (config.stats ?? true) ? 'Counting' : 'Off' },
          {
            slug: 'ipv6',
            value:
              config.ipv6_forwarding === undefined
                ? 'Not managed'
                : config.ipv6_forwarding
                  ? 'Forwarding'
                  : 'Off',
          },
          // Only when there is something to show. design.md §3.4 wants
          // somebody else's rules legible, not a permanent empty page.
          ...(foreign.length
            ? [{ slug: 'unmanaged', value: `${foreign.length} rule${foreign.length === 1 ? '' : 's'}` }]
            : []),
        ]}
      />
    </div>
  )
}
