import { AlertTriangle, ChevronRight } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Link, useLocation } from 'react-router'

import { SettingsList } from '@/components/layout/settings-list'
import { NetworksContent } from '@/routes/gateway/networks'
import { DhcpContent } from '@/routes/dhcp/index'
import { DnsContent } from '@/routes/dns/index'
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
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Disclosure } from '@/components/ui/disclosure'
import { Switch } from '@/components/ui/switch'
import { ApplyOutcome, useGatewayEditor } from '@/features/gateway/editor'
import { DIRECT } from '@/features/gateway/network-list'
import { ImpactBadge, PlanDiff, PlanReasons, impactHint } from '@/features/gateway/plan-preview'
import { InterfaceVisual, interfaceState } from '@/features/link/interface-visual'
import { useInterfaces } from '@/features/link/queries'
import { useUplink } from '@/features/dial/queries'
import { gatewayChange, useGatewayStatus, usePlanGatewayRepair, useReapplyGateway } from '@/features/gateway/queries'
import { ApiError } from '@/lib/api'
import type { GatewayApplyResult, GatewayPlan } from '@/lib/api-types'

/**
 * The gateway, on the page you land on.
 *
 * The default route, interfaces, networks, DHCP and DNS are visible here.
 * Less frequently changed routing details keep their own pages.
 */
export function GatewayPage() {
  const { config, busy, change, applier, gate } = useGatewayEditor()
  const status = useGatewayStatus()
  const preview = usePlanGatewayRepair()
  const reapply = useReapplyGateway()
  const [repairPlan, setRepairPlan] = useState<GatewayPlan | null>(null)
  const [repairError, setRepairError] = useState<string | null>(null)
  const [repairFailure, setRepairFailure] = useState<GatewayApplyResult | null>(null)
  const [repairAttempted, setRepairAttempted] = useState(false)
  const interfaces = useInterfaces()
  const uplink = useUplink()
  const { hash } = useLocation()

  useEffect(() => {
    if (config?.enabled !== undefined && hash) {
      document.getElementById(hash.slice(1))?.scrollIntoView()
    }
  }, [config?.enabled, hash])

  if (!config) return gate
  const exits = config.exits ?? []
  const foreign = status.data?.foreign ?? []

  async function reviewRepair() {
    setRepairAttempted(false)
    setRepairError(null)
    setRepairFailure(null)
    try {
      const plan = await preview.mutateAsync()
      if (!plan.known) throw new Error('The router could not read its current routing rules. Nothing was changed.')
      if (plan.blocked) throw new Error(plan.blocked)
      setRepairPlan(plan)
    } catch (error) {
      setRepairError(error instanceof Error ? error.message : String(error))
    }
  }

  async function confirmRepair() {
    setRepairAttempted(true)
    setRepairError(null)
    try {
      await reapply.mutateAsync()
      setRepairPlan(null)
      setRepairFailure(null)
    } catch (error) {
      setRepairPlan(null)
      const result = error instanceof ApiError ? error.body as GatewayApplyResult | undefined : undefined
      setRepairFailure(result?.steps?.length ? result : null)
      setRepairError(error instanceof Error ? error.message : String(error))
    }
  }

  return (
    <div className="space-y-6">
      <ApplyOutcome applier={applier} />

      {repairError && (
        <Alert variant="destructive">
          <AlertTriangle />
          <AlertTitle>Could not {repairAttempted ? 'restore' : 'review'} saved routing settings</AlertTitle>
          <AlertDescription className="space-y-2">
            <p>{repairError}</p>
            {repairFailure?.steps?.length ? (
              <Disclosure summary="Which steps ran">
                <ul className="space-y-1 font-mono text-xs">
                  {repairFailure.steps.map((step, i) => (
                    <li key={i}>{step.done ? 'done' : step.error ? 'failed' : 'skipped'} {step.description}{step.error ? ` — ${step.error}` : ''}</li>
                  ))}
                </ul>
              </Disclosure>
            ) : null}
            <p>{repairAttempted ? 'Some changes may already be in effect. Check the cause before trying again.' : 'No changes were made.'}</p>
          </AlertDescription>
        </Alert>
      )}

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
          <AlertTitle>Saved routing settings do not match what is running</AlertTitle>
          <AlertDescription className="space-y-2">
            <p>
              Some devices may be using a different internet route than the one shown here.
              This can happen after a restart, an interrupted change, or another program changing routing.
            </p>
            <Button
              size="sm"
              variant="outline"
              disabled={preview.isPending || reapply.isPending}
              onClick={() => void reviewRepair()}
            >
              {preview.isPending ? 'Checking…' : 'Review restore'}
            </Button>
          </AlertDescription>
        </Alert>
      )}

      <Dialog open={repairPlan !== null} onOpenChange={(open) => !open && !reapply.isPending && setRepairPlan(null)}>
        <DialogContent className="sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>Restore saved routing settings?</DialogTitle>
            <DialogDescription>
              This applies the settings shown on this page to the router. It does not edit your saved settings.
              Routing may change again if another program manages it.
            </DialogDescription>
          </DialogHeader>
          {repairPlan && (
            <div className="space-y-3">
              <div className="flex items-center gap-2 text-sm">
                <ImpactBadge impact={repairPlan.impact} />
                <span>{impactHint(repairPlan.impact)}</span>
              </div>
              {repairPlan.impact === 'disruptive' && (
                <p className="text-sm font-medium text-destructive">You may lose access to this page. Make sure you can reconnect to the router before continuing.</p>
              )}
              <PlanReasons plan={repairPlan} />
              <Disclosure summary="What would change"><PlanDiff plan={repairPlan} /></Disclosure>
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" disabled={reapply.isPending} onClick={() => setRepairPlan(null)}>Cancel</Button>
            <Button disabled={reapply.isPending} onClick={() => void confirmRepair()}>
              {reapply.isPending ? 'Restoring…' : 'Restore saved settings'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

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

      <section id="networks" className="space-y-4 border-t pt-6" aria-labelledby="networks-heading">
        <h2 id="networks-heading" className="text-xl font-semibold tracking-tight">Networks</h2>
        <NetworksContent />
      </section>

      <section id="dhcp" className="space-y-4 border-t pt-6" aria-labelledby="dhcp-heading">
        <h2 id="dhcp-heading" className="text-xl font-semibold tracking-tight">DHCP</h2>
        <DhcpContent />
      </section>

      <section id="dns" className="space-y-4 border-t pt-6" aria-labelledby="dns-heading">
        <h2 id="dns-heading" className="text-xl font-semibold tracking-tight">DNS</h2>
        <DnsContent />
      </section>

      <SettingsList
        section="/gateway"
        rows={[
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
