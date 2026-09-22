import { AlertTriangle } from 'lucide-react'
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
import { DIRECT, NetworkList } from '@/features/gateway/network-list'
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

      <Card>
        <CardHeader>
          <CardTitle>Internet via</CardTitle>
          <CardDescription>
            The setting every network follows unless it has one of its own. This is about the
            networks behind this router, not the router itself — where{' '}
            <em>this box</em> plugs into the internet is the uplink, under{' '}
            <Link to="/networks" className="underline underline-offset-2">
              Networks
            </Link>
            .
          </CardDescription>
          <CardAction>
            <Switch
              aria-label="Apply these settings"
              checked={config.enabled}
              disabled={busy}
              onCheckedChange={(enabled) => change(gatewayChange.settings({ enabled }))}
            />
          </CardAction>
        </CardHeader>
        <CardContent className="space-y-4">
          <Select
            value={config.default || DIRECT}
            disabled={busy}
            onValueChange={(v) =>
              change(gatewayChange.settings({ default: !v || v === DIRECT ? '' : v }))
            }
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
          {!config.enabled && (
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
            config={config}
            status={status.data?.assignments}
            busy={busy}
            onChange={change}
          />
        </CardContent>
      </Card>

      <SettingsList
        section="/gateway"
        rows={[
          {
            slug: 'exits',
            value: exits.length ? exits.map((e) => e.name).join(', ') : 'None yet',
          },
          { slug: 'usage', value: (config.stats ?? true) ? 'Counting' : 'Off' },
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
