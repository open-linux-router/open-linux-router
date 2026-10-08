import { AlertTriangle } from 'lucide-react'
import { useState } from 'react'

import { SettingsList } from '@/components/layout/settings-list'
import { SubPage } from '@/components/layout/sub-page'
import { InterfacesCard } from '@/features/link/interfaces-card'
import { useDhcpConfig } from '@/features/dhcp/queries'
import { UplinkCard } from '@/features/dial/uplink-card'
import { useNetworkEditor } from '@/features/link/use-networks'
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
import { SwitchField } from '@/components/ui/editable-field'
import { ApplyOutcome, useGatewayEditor } from '@/features/gateway/editor'
import { DIRECT } from '@/features/gateway/network-list'
import { ImpactBadge, PlanDiff, PlanReasons, impactHint } from '@/features/gateway/plan-preview'
import { gatewayChange, useGatewayStatus, usePlanGatewayRepair, useReapplyGateway } from '@/features/gateway/queries'
import { ApiError } from '@/lib/api'
import type { GatewayApplyResult, GatewayPlan } from '@/lib/api-types'

/** Less common routing controls and the full interface inventory. */
export function GatewayInterfacesPage() {
  const { config, busy, change, applier, gate } = useGatewayEditor()
  const status = useGatewayStatus()
  const dhcp = useDhcpConfig()
  const links = useNetworkEditor()
  const preview = usePlanGatewayRepair()
  const reapply = useReapplyGateway()
  const [repairPlan, setRepairPlan] = useState<GatewayPlan | null>(null)
  const [repairError, setRepairError] = useState<string | null>(null)
  const [repairFailure, setRepairFailure] = useState<GatewayApplyResult | null>(null)
  const [repairAttempted, setRepairAttempted] = useState(false)
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
    <SubPage section="/gateway" slug="interfaces">
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

      <div className="space-y-3">
        <h2 className="text-lg font-medium">All interfaces</h2>
        <InterfacesCard dhcp={dhcp.data} />
        <h2 className="text-lg font-medium">Internet uplink</h2>
        <UplinkCard interfaces={links.interfaces} networks={links.networks} />
      </div>
        <div id="forwarding" className="grid gap-4 scroll-mt-20 lg:grid-cols-2">
          <Card>
            <CardHeader>
              <CardTitle>Gateway routing</CardTitle>
              <CardDescription>
                Apply Gateway routing rules and enable IPv4 forwarding. When off, olr does not
                apply routing or change the machine&rsquo;s forwarding setting.
              </CardDescription>
              <CardAction>
                <Switch aria-label="Apply gateway routing and IPv4 forwarding" checked={config.enabled} disabled={busy} onCheckedChange={(enabled) => change(gatewayChange.settings({ enabled }))} />
              </CardAction>
            </CardHeader>
            <CardContent className="space-y-3">
              <p className="text-sm font-medium">Internet via</p>
              <p className="text-sm text-muted-foreground">Default route for devices on your networks. An interface can choose a different exit in its details.</p>
              <Select value={config.default || DIRECT} disabled={busy} onValueChange={(v) => change(gatewayChange.settings({ default: !v || v === DIRECT ? '' : v }))}>
                <SelectTrigger className="w-full" aria-label="Default internet route"><SelectValue>{(v: string) => v === DIRECT ? 'This router’s own connection' : v}</SelectValue></SelectTrigger>
                <SelectContent><SelectItem value={DIRECT}>This router&rsquo;s own connection</SelectItem>{exits.map((e) => <SelectItem key={e.name} value={e.name}>{e.name}</SelectItem>)}</SelectContent>
              </Select>
            </CardContent>
          </Card>

          <div className="rounded-xl border bg-card p-5">
            <SwitchField
              id="gateway-ipv6-forwarding"
              label="Forward IPv6"
              hint={config.ipv6_forwarding === undefined
                ? 'Not managed yet — this router’s IPv6 forwarding setting is left as it is. Switching on or off hands it to olr.'
                : 'Lets IPv6 pass between networks and the internet. Turning it on may stop this router learning its own IPv6 route from upstream; you will be asked first if that applies.'}
              checked={config.ipv6_forwarding === true}
              busy={busy}
              onChange={(on) => change(gatewayChange.settings({ ipv6_forwarding: on }))}
            />
            {!config.enabled && <p className="mt-3 text-xs text-muted-foreground">Gateway routing is off, so this setting is saved but not applied.</p>}
          </div>
        </div>

        <SettingsList
          section="/gateway"
          heading="Routing settings"
          rows={[
            {
              slug: 'exits',
              value: exits.length ? exits.map((e) => e.name).join(', ') : 'None yet',
            },
            { slug: 'usage', value: (config.stats ?? true) ? 'Counting' : 'Off' },
            // Another program's rules matter only when there is something to show.
            ...(foreign.length
              ? [{ slug: 'unmanaged', value: `${foreign.length} rule${foreign.length === 1 ? '' : 's'}` }]
              : []),
          ]}
        />
    </SubPage>
  )
}
