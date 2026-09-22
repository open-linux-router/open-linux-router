import { AlertTriangle, Check, X } from 'lucide-react'
import { useState } from 'react'

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
import type { InterfaceRow, UplinkStatus } from '@/lib/api-types'
import { cn } from '@/lib/utils'

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
export function UplinkCard({ interfaces }: { interfaces: InterfaceRow[] }) {
  const editor = useUplinkEditor()
  const [open, setOpen] = useState(false)

  if (editor.isPending) return <Skeleton className="h-24 w-full rounded-xl" />
  if (editor.error) {
    return (
      <Alert variant="destructive">
        <AlertTriangle />
        <AlertTitle>Could not read the uplink</AlertTitle>
        <AlertDescription>{editor.error.message}</AlertDescription>
      </Alert>
    )
  }

  const uplink = editor.uplink

  return (
    <>
      <PartialApply editor={editor} />

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
              {uplink ? 'Change' : 'Set up'}
            </Button>
            {uplink && (
              <Button
                size="sm"
                variant="ghost"
                disabled={editor.busy}
                onClick={() => void editor.remove()}
              >
                Hand it back
              </Button>
            )}
          </div>
        </CardContent>
      </Card>

      {open && (
        <UplinkDialog
          open={open}
          onOpenChange={setOpen}
          initial={uplink}
          interfaces={interfaces}
          onSubmit={(next) => {
            void editor.save(next)
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
  const route = describeRoute(uplink)

  return (
    <div className="space-y-3">
      <div className="flex items-start gap-3">
        <span className={cn('mt-1.5 size-2 shrink-0 rounded-full', route.dot)} aria-hidden />
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
          <p className={cn('text-sm', route.tone)}>{route.line}</p>
        </div>
      </div>

      {(uplink.addresses?.length ?? 0) > 1 && (
        // Two managers on one interface. olr reports it and removes nothing —
        // stripping what a distribution's DHCP client put there is the failure
        // this whole object exists to stop.
        <p className="text-sm text-warning">
          {uplink.interface} also has {uplink.addresses?.filter((a) => a !== uplink.address).join(', ')}{' '}
          on it. Something else is addressing this interface too — usually your distribution&rsquo;s
          network configuration. olr leaves those alone; leave one of the two in charge, or the
          address will come and go.
        </p>
      )}

      {(uplink.dns?.length ?? 0) > 0 && (
        <p className="text-sm text-muted-foreground">
          Resolvers recorded: {uplink.dns?.join(', ')}. Nothing reads them yet — this router
          resolves names from the root by default, which needs no upstream at all.
        </p>
      )}

      {uplink.problems?.map((p) => (
        <p key={p.path + p.message} className="text-sm text-warning">
          {p.message}
        </p>
      ))}
    </div>
  )
}

/**
 * The sentence that answers "so is it actually working".
 *
 * Three states, and they must not read alike: no default route at all, a route
 * going where we asked, and a route going somewhere else. The last is the one
 * this card exists for.
 */
function describeRoute(u: UplinkStatus): { line: string; dot: string; tone: string } {
  if (!u.present) {
    return {
      line: 'This machine has no interface with that name.',
      dot: 'bg-destructive',
      tone: 'text-destructive',
    }
  }
  if (!u.route_via) {
    return {
      line: 'This router has no default route — it cannot reach the internet.',
      dot: 'bg-destructive',
      tone: 'text-destructive',
    }
  }
  if (!u.gateway) {
    return {
      line: `The default route goes via ${u.route_via} on ${u.route_dev ?? '?'}, which olr did not configure.`,
      dot: 'bg-muted-foreground/40',
      tone: 'text-muted-foreground',
    }
  }
  if (u.route_via !== u.gateway || u.route_dev !== u.interface) {
    return {
      line: `The default route actually goes via ${u.route_via} on ${u.route_dev ?? '?'} — not what is set above. Save again to re-apply it.`,
      dot: 'bg-warning',
      tone: 'text-warning',
    }
  }
  if (!u.up) {
    return { line: `${u.interface} is down.`, dot: 'bg-warning', tone: 'text-warning' }
  }
  return {
    line: `The default route goes via ${u.route_via} on ${u.route_dev}, as set.`,
    dot: 'bg-success',
    tone: 'text-muted-foreground',
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
  const pending = editor.pending
  return (
    <Dialog open={pending !== null} onOpenChange={(o) => !o && editor.cancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>This changes how this router reaches the internet</DialogTitle>
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

        <Disclosure summary="What changes on the box">
          <pre className="overflow-x-auto pt-1 font-mono text-xs">
            {pending?.plan.changes.map((c) => c.diff).join('')}
          </pre>
        </Disclosure>

        <DialogFooter>
          <Button variant="outline" onClick={editor.cancel}>
            Cancel
          </Button>
          <Button variant="destructive" disabled={editor.busy} onClick={editor.confirm}>
            Apply anyway
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** An uplink change reaches the kernel and can land halfway; §5.2 gives it no
 * rollback, so what did happen has to be reported rather than swallowed. */
function PartialApply({ editor }: { editor: ReturnType<typeof useUplinkEditor> }) {
  if (!editor.failure) return null
  return (
    <Alert variant="destructive" role="alert">
      <AlertTriangle />
      <AlertTitle>Only part of the change went through</AlertTitle>
      <AlertDescription className="space-y-3">
        <p>
          The steps that finished have already taken effect and will not be undone. Fixing the
          cause and applying again is safe — it picks up where this left off.
        </p>
        {editor.failure.error && <p>{editor.failure.error.message}</p>}
        <Disclosure summary="Which steps ran">
          <ul className="space-y-1 pt-1 font-mono text-xs">
            {editor.failure.steps?.map((step) => (
              <li key={step.description} className="flex gap-2">
                {step.done ? (
                  <Check className="mt-0.5 size-3.5 shrink-0 text-success" aria-label="done" />
                ) : (
                  <X className="mt-0.5 size-3.5 shrink-0 text-destructive" aria-label="failed" />
                )}
                <span>
                  {step.description}
                  {step.error && <span className="block opacity-80">{step.error}</span>}
                </span>
              </li>
            ))}
          </ul>
        </Disclosure>
        <Button size="sm" variant="outline" onClick={editor.dismissFailure}>
          Dismiss
        </Button>
      </AlertDescription>
    </Alert>
  )
}
