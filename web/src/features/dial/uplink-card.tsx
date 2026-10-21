import { t, useLocale } from '@/lib/i18n'
import { AlertTriangle, Check, X } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
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
import { Disclosure } from '@/components/ui/disclosure'
import { ListEmpty } from '@/components/ui/list'
import { Skeleton } from '@/components/ui/skeleton'
import { useUplinkEditor } from '@/features/dial/queries'
import { UplinkDialog } from '@/features/dial/uplink-dialog'
import { useRemoveAddress } from '@/features/link/queries'
import type {
  DelegationStatus,
  NetworkRow,
  InterfaceRow,
  TunnelStatus,
  UplinkStatus,
} from '@/lib/api-types'
import { StatusDot } from '@/components/ui/status'
import type { Tone } from '@/lib/state'

/**
 * How this router itself reaches the internet.
 *
 * The section this page was missing, and the reason somebody with three NICs
 * could not configure a working router here at all: they gave the modem-facing
 * one a static address under Networks, and then there was nowhere to put the
 * gateway. A network is something this box *serves*; the way out is not one,
 * and it needed an object of its own.
 *
 * It renders intent and fact together, which is the whole point of reading it.
 * The top line is what olr was told. The line under it is the default route
 * actually in the kernel, whichever interface it leaves by — so "configured
 * correctly and going out of the wrong NIC" is visible rather than being a
 * conclusion somebody has to reach from a terminal.
 */
export function UplinkCard({
  interfaces,
  networks,
  compact = false,
}: {
  interfaces: InterfaceRow[]
  /** The networks, so the dialog can offer to take one's interface over. */
  networks: NetworkRow[]
  compact?: boolean
}) {
  useLocale()
  const editor = useUplinkEditor()
  const [open, setOpen] = useState(false)

  if (editor.isPending) return <Skeleton className="h-24 w-full rounded-xl" />
  if (editor.error) {
    return (
      <Alert variant="destructive">
        <AlertTriangle />
        <AlertTitle>{t('Could not read the uplink')}</AlertTitle>
        <AlertDescription>{editor.error.message}</AlertDescription>
      </Alert>
    )
  }

  const uplink = editor.uplink

  return (
    <>
      <PartialApply editor={editor} />
      <Button variant="secondary" size="sm" nativeButton={false} render={<Link to="/access/tunnelbroker">Tunnel Broker</Link>} />

      {compact ? (
        <Button size="sm" variant="secondary" disabled={editor.busy} onClick={() => setOpen(true)}>
          {uplink ? 'Change uplink' : 'Set up uplink'}
        </Button>
      ) : (
        <Card>
          <CardContent className="space-y-4">
          {!uplink ? (
            <ListEmpty>
              olr does not own this router&rsquo;s way out. The default route is whatever your
              distribution or a DHCP client put there, and olr leaves it alone — which is right
              when this box sits beside your modem rather than in front of it.
            </ListEmpty>
          ) : (
            <UplinkSummary uplink={uplink} />
          )}

          <div className="flex gap-2">
            <Button size="sm" disabled={editor.busy} onClick={() => setOpen(true)}>
              {uplink ? t('Change') : 'Set up'}
            </Button>
            {uplink && (
              <Button
                size="sm"
                variant="quiet"
                disabled={editor.busy}
                onClick={() => void editor.remove()}
              > {t('Hand it back')} </Button>
            )}
          </div>
          </CardContent>
        </Card>
      )}

      {open && (
        <UplinkDialog
          open={open}
          onOpenChange={setOpen}
          initial={uplink}
          interfaces={interfaces}
          networks={networks}
          onSubmit={(next, replacing) => {
            void editor.save(next, replacing)
            setOpen(false)
          }}
        />
      )}

      <ConfirmDisruptive editor={editor} />
    </>
  )
}

/* -------------------------------------------------------------------------- */

function UplinkSummary({ uplink }: { uplink: UplinkStatus }) {
  useLocale()
  const route = describeRoute(uplink)
  const leftovers = uplink.addresses?.filter((a) => a !== uplink.address) ?? []
  const addresses = useRemoveAddress()

  return (
    <div className="space-y-3">
      <div className="flex items-start gap-3">
        <StatusDot tone={route.tone} size="md" className="mt-1.5" />
        <div className="min-w-0 space-y-1">
          <div className="font-mono text-sm font-medium">
            {uplink.interface}
            {uplink.address ? ` · ${uplink.address}` : ' · no address'}
          </div>
          <p className="text-sm text-muted-foreground">
            {uplink.gateway
              ? `Everything else goes to ${uplink.gateway}.`
              : 'olr owns this interface and writes no address or route on it.'}
          </p>
          <p className="text-sm text-muted-foreground">{route.line}</p>
        </div>
      </div>

      {leftovers.length > 0 && (
        // An address the uplink does not account for. Never removed by an
        // apply — stripping what a distribution's DHCP client put there is the
        // failure this whole object exists to stop — but removable by name,
        // because the other common source is olr itself: a network removed
        // before networks took their addresses with them.
        <div className="space-y-2 text-sm text-warning-foreground">
          <p>
            {uplink.interface} also has {leftovers.join(', ')} on it, which the uplink does not
            account for. If something else on this box is addressing the interface, leave one of
            the two in charge; if it is left over, remove it.
          </p>
          <div className="flex flex-wrap gap-2">
            {leftovers.map((a) => (
              <Button
                key={a}
                size="sm"
                variant="secondary"
                disabled={addresses.busy}
                onClick={() => void addresses.remove(uplink.interface, a)}
              >{t('Remove')}{a}
              </Button>
            ))}
          </div>
        </div>
      )}

      <ResolverLine uplink={uplink} />

      {uplink.tunnel && <TunnelLine tunnel={uplink.tunnel} />}
      {uplink.delegation && <DelegationLine uplink={uplink} delegation={uplink.delegation} />}

      {uplink.problems?.map((p) => (
        <p key={p.path + p.message} className="text-sm text-warning-foreground">
          {p.message}
        </p>
      ))}
    </div>
  )
}

/**
 * Where this router itself looks names up — what it was told beside what it
 * does, like the route above it.
 *
 * The line that was missing on the box that forced it: an uplink exactly as
 * set, a gateway that answered, and a router that could not resolve a single
 * name because the file it resolves through was empty. Nothing on the page
 * said so.
 */
/**
 * The tunnel in one line: what was set, and whether IPv6 is actually going
 * through it — the same intent-beside-fact pairing as the route above.
 */
function TunnelLine({ tunnel }: { tunnel: TunnelStatus }) {
  useLocale()
  const using = tunnel.present && tunnel.up && tunnel.route_dev === tunnel.interface
  let state: string
  if (!tunnel.present) state = `${tunnel.interface} is missing.`
  else if (!tunnel.up) state = `${tunnel.interface} is down.`
  else if (!tunnel.route_dev) state = 'There is no IPv6 default route.'
  else if (!using) state = `IPv6 leaves by ${tunnel.route_dev}, not through the tunnel.`
  else state = 'IPv6 goes out through it.'

  return (
    <div className="space-y-1 text-sm">
      <div className="font-mono">
        IPv6 · {tunnel.address} via tunnel to {tunnel.server}
      </div>
      <p className={using ? 'text-muted-foreground' : 'text-warning-foreground'}>{state}</p>
    </div>
  )
}

/**
 * The delegated prefix: what the ISP gave and until when, or where the loop
 * is instead — and accept_ra, since a prefix with no route out is no IPv6.
 */
function DelegationLine({
  uplink,
  delegation,
}: {
  uplink: UplinkStatus
  delegation: DelegationStatus
}) {
  useLocale()
  const until = (t?: string) => (t ? new Date(t).toLocaleString() : '')
  return (
    <div className="space-y-1 text-sm">
      <div className="font-mono">
        IPv6 · {delegation.prefix ? `${delegation.prefix} delegated by the ISP` : 'no prefix yet'}
      </div>
      {delegation.prefix ? (
        <p className="text-muted-foreground"> {t('Renews')} {until(delegation.renews)}, expires {until(delegation.expires)}.
        </p>
      ) : (
        <p className="text-warning-foreground">
          {delegation.error
            ? `Still asking — the last attempt failed: ${delegation.error}`
            : 'Asking the ISP for a prefix.'}
        </p>
      )}
      {uplink.accept_ra && uplink.accept_ra !== '2' && (
        <p className="text-warning-foreground">
          {uplink.interface} is not taking router advertisements the way prefix delegation
          needs (accept_ra is {uplink.accept_ra}). Applying the uplink again sets it.
        </p>
      )}
    </div>
  )
}

function ResolverLine({ uplink }: { uplink: UplinkStatus }) {
  useLocale()
  const set = uplink.dns ?? []
  const through = uplink.resolving_through ?? []
  const owned = set.length > 0 && Boolean(uplink.address)

  if (owned && sameSet(set, through)) {
    return (
      <p className="text-sm text-muted-foreground"> {t('This router looks names up through')} {through.join(', ')}.
      </p>
    )
  }
  if (owned) {
    // Set, and not in force. The findings below say why when olr knows — a
    // resolv.conf another program maintains; otherwise saving again puts
    // them back.
    return (
      <p className="text-sm text-warning-foreground"> {t('Set to')} {set.join(', ')}, but this router looks names up through{' '}
        {through.length ? through.join(', ') : 'nothing at all'}.
      </p>
    )
  }
  if (through.length === 0) {
    return (
      <p className="text-sm text-warning-foreground">
        This router has no resolver of its own, so it cannot look names up — updates and
        installs fail by name while everything works by address. Add one to the uplink
        {uplink.gateway ? `: usually your modem, ${uplink.gateway}` : ''}.
      </p>
    )
  }
  return (
    <p className="text-sm text-muted-foreground"> {t('This router looks names up through')} {through.join(', ')}, as your distribution set it.
    </p>
  )
}

function sameSet(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((x) => b.includes(x))
}

/**
 * The sentence that answers "so is it actually working".
 *
 * The states must not read alike: no default route at all, a route going
 * somewhere else, and a route going where we asked — which is itself two
 * states, because a gateway on the wrong segment is a route exactly as set that
 * carries nothing. That last one showed green until the neighbour table was
 * read, on the box that had it.
 */
function describeRoute(u: UplinkStatus): { line: string; tone: Tone } {
  if (!u.present) {
    return {
      line: 'This machine has no interface with that name.',
      tone: 'fault',
    }
  }
  if (!u.route_via) {
    return {
      line: 'This router has no default route — it cannot reach the internet.',
      tone: 'fault',
    }
  }
  if (!u.gateway) {
    return {
      line: `The default route goes via ${u.route_via} on ${u.route_dev ?? '?'}, which olr did not configure.`,
      tone: 'off',
    }
  }
  if (u.route_via !== u.gateway || u.route_dev !== u.interface) {
    return {
      line: `The default route actually goes via ${u.route_via} on ${u.route_dev ?? '?'} — not what is set above. Save again to re-apply it.`,
      tone: 'warn',
    }
  }
  if (!u.up) {
    return { line: `${u.interface} is down.`, tone: 'warn' }
  }
  // The route matches what was set; whether anything is on the other end of
  // it is a separate fact, and the one that decides whether the box is online.
  // A gateway on the wrong segment passes every check above.
  if (u.gateway_state === 'silent') {
    return {
      line: u.gateway_seen_on
        ? `${u.gateway} is not answering on ${u.interface}. It answers on ${u.gateway_seen_on} — that is the interface facing it, so change the uplink to ${u.gateway_seen_on}.`
        : `${u.gateway} is not answering on ${u.interface}. Check the cable, and that ${u.interface} is the interface facing your modem.`,
      tone: 'fault',
    }
  }
  if (u.gateway_state !== 'answers') {
    return {
      line: `The default route goes via ${u.route_via} on ${u.route_dev}, as set. Nothing has tried to reach ${u.route_via} yet, so whether it answers is not known.`,
      tone: 'unknown',
    }
  }
  return {
    line: `The default route goes via ${u.route_via} on ${u.route_dev}, as set, and ${u.route_via} answers.`,
    tone: 'ok',
  }
}

/**
 * The confirmation a disruptive uplink change stops at.
 *
 * It stops harder than the other modules' for a reason none of them has: the
 * request that replaces the default route may be arriving over the default
 * route. §5.5's lockout guard is not built, so this dialog and the warning the
 * server attaches to the plan are the whole safety net.
 */
function ConfirmDisruptive({ editor }: { editor: ReturnType<typeof useUplinkEditor> }) {
  useLocale()
  const pending = editor.pending
  return (
    <Dialog open={pending !== null} onOpenChange={(o) => !o && editor.cancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('This changes how this router reaches the internet')}</DialogTitle>
          <DialogDescription>
            The default route is replaced as soon as you apply. If you are reaching this page
            from outside your own network, your connection goes with it.
          </DialogDescription>
        </DialogHeader>

        {pending?.plan.warnings?.map((w) => (
          <Alert variant="destructive" key={w.path + w.message}>
            <AlertTriangle />
            <AlertDescription>{w.message}</AlertDescription>
          </Alert>
        ))}

        <Disclosure summary={t('What changes on the box')}>
          <pre className="overflow-x-auto pt-1 font-mono text-xs">
            {pending?.plan.changes.map((c) => c.diff).join('')}
          </pre>
        </Disclosure>

        <DialogFooter>
          <Button variant="quiet" onClick={editor.cancel}>{t('Cancel')}</Button>
          <Button disabled={editor.busy} onClick={editor.confirm}>{t('Apply anyway')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** An uplink change reaches the kernel and can land halfway; §5.2 gives it no
 * rollback, so what did happen has to be reported rather than swallowed. */
function PartialApply({ editor }: { editor: ReturnType<typeof useUplinkEditor> }) {
  useLocale()
  if (!editor.failure) return null
  return (
    <Alert variant="destructive" role="alert">
      <AlertTriangle />
      <AlertTitle>{t('Only part of the change went through')}</AlertTitle>
      <AlertDescription className="space-y-3">
        <p>{t('The steps that finished have already taken effect and will not be undone. Fixing the cause and applying again is safe — it picks up where this left off.')}</p>
        {editor.failure.error && <p>{editor.failure.error.message}</p>}
        <Disclosure summary={t('Which steps ran')}>
          <ul className="space-y-1 pt-1 font-mono text-xs">
            {editor.failure.steps?.map((step) => (
              <li key={step.description} className="flex gap-2">
                {step.done ? (
                  <Check className="mt-0.5 size-3.5 shrink-0 text-success-foreground" aria-label={t('done')} />
                ) : (
                  <X className="mt-0.5 size-3.5 shrink-0 text-destructive" aria-label={t('failed')} />
                )}
                <span>
                  {step.description}
                  {step.error && <span className="block opacity-80">{step.error}</span>}
                </span>
              </li>
            ))}
          </ul>
        </Disclosure>
        <Button size="sm" variant="secondary" onClick={editor.dismissFailure}>{t('Dismiss')}</Button>
      </AlertDescription>
    </Alert>
  )
}
