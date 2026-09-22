import { AlertTriangle, Check, Plus, X } from 'lucide-react'
import { useState } from 'react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
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
import { useDhcpConfig } from '@/features/dhcp/queries'
import { UplinkCard } from '@/features/dial/uplink-card'
import { InterfacesCard } from '@/features/link/interfaces-card'
import { NetworkDialog } from '@/features/link/network-dialog'
import { useNetworkEditor } from '@/features/link/use-networks'
import type { GroupRow } from '@/lib/api-types'
import type { Group } from '@/lib/config-types'

/**
 * Everything about this router's own interfaces: which ones it has been given,
 * and what networks they carry.
 *
 * This page is the answer to a specific dead end. Adding an address range used
 * to mean choosing an interface and typing a range inside whatever subnet that
 * interface already had — and if you wanted a different subnet, the form said
 * so and there was nowhere in olr to go and change it. The subnet is declared
 * here now, and the range is checked against it.
 *
 * ## The three sections, and why they are one page
 *
 * Interfaces, then networks, then the uplink — which is the order of the work
 * and, deliberately, not the order of importance. An interface has to be handed
 * over before anything can use it; after that it becomes either a network this
 * router *serves* or the one way *out*, and those are different objects with
 * different owners. Splitting them across pages is what produced the dead end
 * below and the one the uplink section closes: somebody with three NICs gave
 * the modem-facing one a static address under Networks, because that was the
 * only place in olr that would take an address, and then had nowhere to put a
 * gateway.
 *
 * ## Why adoption is on this page, above the networks
 *
 * It used to be a sub-page of DHCP, and the dead end it produced was the same
 * shape as the one above. A network needs an adopted interface; the dialog's
 * only way of saying so was a line of small print telling you to go and find a
 * switch under a *different* section, listed after this one. Somebody setting
 * up two NICs could not find it at all.
 *
 * So it is here, and it is first. The order is the order of the work: hand an
 * interface over, then say what network it carries. It costs a configured box a
 * short list above the one it came for, which is a fair price — that list is
 * also the only place in olr that answers "is the cable in", and it is read
 * exactly when a network looks configured and does not work.
 */
export function NetworksPage() {
  const editor = useNetworkEditor()
  // Read here so a release can name the ranges it is about to invalidate; the
  // card joins the two and olr never guesses on the operator's behalf.
  const dhcp = useDhcpConfig()
  const [editing, setEditing] = useState<Group | undefined>(undefined)
  const [open, setOpen] = useState(false)

  if (editor.isPending) return <PageSkeleton />
  if (editor.error) {
    return (
      <Alert variant="destructive">
        <AlertTriangle />
        <AlertTitle>Could not load the networks</AlertTitle>
        <AlertDescription>{editor.error.message}</AlertDescription>
      </Alert>
    )
  }

  const stored = editor.config?.groups ?? []
  const taken = new Set(stored.flatMap((g) => g.members))

  function upsert(group: Group) {
    const rest = stored.filter((g) => g.name !== group.name)
    editor.save([...rest, group])
    setOpen(false)
  }

  function remove(name: string) {
    editor.save(stored.filter((g) => g.name !== name))
    setOpen(false)
  }

  return (
    // No page header. components/layout/app-shell renders the section's title
    // and blurb on a section's own landing page, and this page was drawing a
    // second identical <h1> underneath it — two "Networks" headings, one above
    // the other, since the day the section was added.
    <div className="space-y-6">
      <PartialApply editor={editor} />

      <section className="space-y-3">
        <div className="space-y-1">
          <h2 className="text-lg font-medium tracking-tight">Interfaces</h2>
          <p className="max-w-prose text-sm text-muted-foreground">
            Which of this machine's interfaces this router may use. Switching one on changes
            nothing by itself — no address is set and no service is started — but until one is
            on, it cannot carry a network.
          </p>
        </div>
        <InterfacesCard dhcp={dhcp.data} />
      </section>

      <section className="space-y-3">
        {/* items-start, so Add sits level with the heading rather than at the
            foot of a three-line paragraph. */}
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1">
            <h2 className="text-lg font-medium tracking-tight">Networks</h2>
            <p className="max-w-prose text-sm text-muted-foreground">
              A network is a subnet, the interface it lives on, and this router's address on
              it — one per subnet you serve. Address ranges and internet access are configured
              against a network, not against an interface.
            </p>
          </div>
          <Button
            size="sm"
            disabled={editor.busy}
            onClick={() => {
              setEditing(undefined)
              setOpen(true)
            }}
          >
            <Plus className="size-4" aria-hidden /> Add
          </Button>
        </div>

        {editor.groups.length === 0 ? (
          <ListEmpty>
            No networks yet. Add one to say what subnet this router serves — an address range
            needs a network to sit in.
          </ListEmpty>
        ) : (
          <List>
            {editor.groups.map((g) => (
              <ListRow
                key={g.name}
                title={g.name}
                subtitle={subtitleOf(g)}
                trailing={g.members.join(', ') || 'no interface'}
                onSelect={() => {
                  setEditing(stored.find((s) => s.name === g.name))
                  setOpen(true)
                }}
              />
            ))}
          </List>
        )}
      </section>

      <section className="space-y-3">
        <div className="space-y-1">
          <h2 className="text-lg font-medium tracking-tight">Internet uplink</h2>
          <p className="max-w-prose text-sm text-muted-foreground">
            How this router itself reaches the internet: the interface facing your modem, its
            address, and where to send everything else. Not a network — a network is one this
            router serves, and it hands out addresses there. Leave this alone if something else
            on the box already provides the default route.
          </p>
        </div>
        <UplinkCard interfaces={editor.interfaces} />
      </section>

      {editor.problems.length > 0 && (
        <Alert>
          <AlertTriangle />
          <AlertTitle>Worth knowing</AlertTitle>
          <AlertDescription>
            <ul className="space-y-1">
              {editor.problems.map((p) => (
                <li key={p.path + p.message}>{p.message}</li>
              ))}
            </ul>
          </AlertDescription>
        </Alert>
      )}

      {open && (
        <NetworkDialog
          open={open}
          onOpenChange={setOpen}
          initial={editing}
          interfaces={editor.interfaces}
          taken={taken}
          onSubmit={upsert}
          onRemove={editing ? () => remove(editing.name) : undefined}
        />
      )}

      <ConfirmDisruptive editor={editor} />
    </div>
  )
}

/**
 * The subtitle carries both halves an operator reads separately: the subnet,
 * and where this box sits on it. The derived range is here too because it is
 * the answer to "so what will DHCP hand out" — asked on this page far more
 * often than it is asked on the DHCP one.
 */
function subtitleOf(g: GroupRow): string {
  if (!g.subnet) return 'No IPv4 — router advertisement only'
  const parts = [`${g.subnet}, this router at ${g.router}`]
  if (g.suggested_start) parts.push(`range ${g.suggested_start}–${g.suggested_end}`)
  return parts.join(' · ')
}

/**
 * The confirmation a disruptive network change stops at.
 *
 * Every other module applies instantly and only stops for `disruptive`. This
 * one stops harder than the rest: §5.5's lockout guard is not built, so nothing
 * reverts a change that takes the operator's own route to this box away. The
 * server attaches a warning naming the interface and address when it can see
 * that is what is about to happen, and it is shown here verbatim.
 */
function ConfirmDisruptive({ editor }: { editor: ReturnType<typeof useNetworkEditor> }) {
  const pending = editor.pending
  return (
    <Dialog open={pending !== null} onOpenChange={(o) => !o && editor.cancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>This drops devices from the network</DialogTitle>
          <DialogDescription>
            Every device on this network loses the address it is holding and has to ask for a
            new one. Devices that are already on keep the old address until they renew.
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

/** A network change reaches the kernel and can land halfway; §5.2 gives it no
 * rollback, so what did happen has to be reported rather than swallowed. */
function PartialApply({ editor }: { editor: ReturnType<typeof useNetworkEditor> }) {
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

function PageSkeleton() {
  return (
    <div className="space-y-6">
      <Skeleton className="h-8 w-40" />
      <Skeleton className="h-24 w-full" />
    </div>
  )
}
