import { AlertTriangle, ExternalLink, Plus, ShieldCheck } from 'lucide-react'
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
import { CertificateDialog } from '@/features/ingress/certificate-dialog'
import { ImpactBadge, PlanDiff, PlanReasons, impactHint } from '@/features/ingress/impact'
import {
  ingressChange,
  useIngressConfig,
  useIngressStatus,
  useReapplyIngress,
  type IngressChange,
} from '@/features/ingress/queries'
import { NoProxyAlert } from '@/features/ingress/no-proxy'
import { ServiceDialog } from '@/features/ingress/service-dialog'
import { useIngressApply } from '@/features/ingress/use-apply'
import type { IngressStatus } from '@/lib/api-types'
import type { Service } from '@/lib/config-types'

export function IngressPage() {
  const config = useIngressConfig()
  const status = useIngressStatus()
  const applier = useIngressApply()
  const reapply = useReapplyIngress()

  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<Service | null>(null)
  const [cert, setCert] = useState(false)

  if (config.isPending) return <PageSkeleton />
  if (config.isError) {
    return (
      <Alert variant="destructive">
        <AlertTriangle />
        <AlertTitle>Could not load the published services</AlertTitle>
        <AlertDescription>{(config.error as Error).message}</AlertDescription>
      </Alert>
    )
  }

  const current = config.data
  const services = current.services ?? []
  const domain = status.data?.domain
  const change = (c: IngressChange) => applier.submit(c)

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
                    {s.skipped ? 'skipped' : s.done ? 'done   ' : 'failed '} {s.description}
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

      {/* Ordered by what has to be true first. No proxy means nothing below can
          work, so it comes before the certificate, which comes before the list of
          things the certificate serves. */}
      {status.data?.binary_error && (
        <NoProxyAlert message={status.data.binary_error} binary={status.data.binary} />
      )}

      {status.data?.certificate_warnings?.map((warning) => (
        <Alert key={warning}>
          <AlertTriangle />
          <AlertTitle>Certificate</AlertTitle>
          <AlertDescription>{warning}</AlertDescription>
        </Alert>
      ))}

      {status.data?.drifted && (
        <Alert>
          <AlertTriangle />
          <AlertTitle>The router is not doing what these settings say</AlertTitle>
          <AlertDescription className="space-y-3">
            <p>
              Something changed the proxy&rsquo;s configuration outside olr. Putting it back reloads
              the proxy without dropping connections.
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
          <CardTitle>Published services</CardTitle>
          <CardDescription>
            Something on your network, reachable at a proper https:// address instead of an IP and a
            port number.
          </CardDescription>
          <CardAction>
            <Switch
              aria-label="Serve these addresses"
              checked={current.enabled}
              disabled={applier.busy}
              onCheckedChange={(enabled) => change(ingressChange.settings({ enabled }))}
            />
          </CardAction>
        </CardHeader>
        {!current.enabled && (
          <CardContent>
            <p className="text-sm text-muted-foreground">
              These addresses are saved but switched off, so none of them answers.
            </p>
          </CardContent>
        )}
      </Card>

      <CertificateCard
        status={status.data}
        enabled={current.enabled}
        onEdit={() => setCert(true)}
      />

      <Card>
        <CardHeader>
          <CardTitle>Addresses</CardTitle>
          <CardDescription>
            {domain ? (
              <>
                Each one answers under <span className="font-mono">{domain}</span> and is covered by
                the same certificate.
              </>
            ) : (
              'Each one answers under the name your network already uses.'
            )}
          </CardDescription>
          <CardAction>
            <Button size="sm" variant="outline" onClick={() => setAdding(true)}>
              <Plus />
              Add
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent>
          <ServiceList services={services} status={status.data} onEdit={setEditing} />
        </CardContent>
      </Card>

      <ServiceDialog
        open={adding}
        onOpenChange={setAdding}
        domain={domain}
        onSubmit={(service) => change(ingressChange.saveService(service.name, service))}
      />
      {editing && (
        <ServiceDialog
          open
          onOpenChange={(open) => !open && setEditing(null)}
          initial={editing}
          domain={domain}
          // The path carries the name this service has; the body carries what it
          // should become.
          onSubmit={(service) => change(ingressChange.saveService(editing.name, service))}
          onRemove={() => {
            setEditing(null)
            change(ingressChange.removeService(editing.name))
          }}
        />
      )}
      <CertificateDialog
        open={cert}
        onOpenChange={setCert}
        initial={current.certificate}
        domain={domain}
        onSubmit={(fields) => change(ingressChange.settings(fields))}
      />

      <ConfirmDialog applier={applier} />
    </div>
  )
}

/**
 * The certificate, which is the one thing on this screen that goes wrong on its
 * own.
 *
 * Shown whether or not anything is wrong, and that is deliberate: renewal
 * failures are silent for weeks, so a line that only appears once it is too late
 * is a line nobody has learned to read. The number of days is the whole point —
 * it is the only thing on the page that will be different tomorrow.
 */
function CertificateCard({
  status,
  enabled,
  onEdit,
}: {
  status?: IngressStatus
  enabled: boolean
  onEdit: () => void
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <ShieldCheck className="size-4 text-muted-foreground" />
          Certificate
          {status && <CertBadge status={status} enabled={enabled} />}
        </CardTitle>
        <CardDescription>
          One certificate covers every address here, so adding one later needs nothing.
        </CardDescription>
        <CardAction>
          <Button size="sm" variant="outline" onClick={onEdit}>
            Settings
          </Button>
        </CardAction>
      </CardHeader>
      {status?.certificate.certificate && (
        <CardContent>
          <p className="text-sm text-muted-foreground">
            Issued {new Date(status.certificate.certificate.issued_at).toLocaleDateString()}, renews
            itself automatically. Covers{' '}
            <span className="font-mono">{status.certificate.certificate.names.join(', ')}</span>.
          </p>
        </CardContent>
      )}
    </Card>
  )
}

/**
 * Four states, not two, and each points somewhere different.
 *
 * "Not needed yet" is a switched-off module and not a fault. "Waiting" is the
 * normal first minute after enabling — and, if it never changes, a wrong
 * credential. "Renewal overdue" is the one worth a colour: everything is working
 * and has been quietly failing to renew for weeks. Collapsing any of these into
 * the others would send somebody to look in the wrong place.
 */
function CertBadge({ status, enabled }: { status: IngressStatus; enabled: boolean }) {
  const cert = status.certificate
  if (!enabled) return <Badge variant="secondary">Not needed yet</Badge>
  if (status.certificate_error) return <Badge variant="secondary">Unknown</Badge>
  if (!cert.found) return <Badge variant="secondary">Waiting</Badge>
  if ((cert.expires_in_days ?? 0) <= 0) return <Badge variant="destructive">Expired</Badge>
  if (cert.renewal_overdue) return <Badge variant="destructive">Renewal overdue</Badge>
  return <Badge variant="success">Valid for {cert.expires_in_days} days</Badge>
}

function ServiceList({
  services,
  status,
  onEdit,
}: {
  services: Service[]
  status?: IngressStatus
  onEdit: (service: Service) => void
}) {
  if (services.length === 0) {
    return (
      <ListEmpty>
        Nothing is published yet. Add one to reach something on your network — a dashboard, a NAS, a
        media server — by name instead of by address and port.
      </ListEmpty>
    )
  }

  const domain = status?.domain

  return (
    <List>
      {services.map((s) => {
        const url = domain ? `https://${s.name}.${domain}` : undefined
        return (
          <ListRow
            key={s.name}
            title={s.name}
            subtitle={describeService(s, domain)}
            trailing={
              url ? (
                // Opens the thing itself. A published address exists to be
                // visited, and making the operator retype what the row already
                // says would be the small indignity that makes a screen feel
                // like a form rather than a control panel.
                <Button
                  size="sm"
                  variant="ghost"
                  aria-label={`Open ${s.name}`}
                  onClick={(e) => {
                    e.stopPropagation()
                    window.open(url, '_blank', 'noopener,noreferrer')
                  }}
                >
                  <ExternalLink />
                </Button>
              ) : undefined
            }
            onSelect={() => onEdit(s)}
          />
        )
      })}
    </List>
  )
}

function describeService(s: Service, domain?: string): string {
  const where = s.upstream.device || s.upstream.host || '?'
  const scheme = s.upstream.scheme === 'https' ? ' over https' : ''
  const address = domain ? `${s.name}.${domain}` : s.name
  return `${address} → ${where}:${s.upstream.port}${scheme}`
}

/**
 * The one thing that interrupts an operator (design.md §6.3).
 *
 * Everything else applies on the click. This appears only when the plan came back
 * `disruptive`, which here means an address somebody may have open, bookmarked,
 * or wired into another application is about to stop answering — and unlike a
 * dropped connection, reloading does not fix it.
 */
function ConfirmDialog({ applier }: { applier: ReturnType<typeof useIngressApply> }) {
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
