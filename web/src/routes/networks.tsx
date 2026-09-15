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
import { NetworkDialog } from '@/features/link/network-dialog'
import { useNetworkEditor } from '@/features/link/use-networks'
import type { GroupRow } from '@/lib/api-types'
import type { Group } from '@/lib/config-types'

/**
 * The networks this router serves.
 *
 * This page is the answer to a specific dead end. Adding an address range used
 * to mean choosing an interface and typing a range inside whatever subnet that
 * interface already had — and if you wanted a different subnet, the form said
 * so and there was nowhere in olr to go and change it. The subnet is declared
 * here now, and the range is checked against it.
 */
export function NetworksPage() {
  const editor = useNetworkEditor()
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
    <div className="space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Networks</h1>
        <p className="max-w-prose text-sm text-muted-foreground">
          A network is a subnet, the interface it lives on, and this router's address on it.
          Address ranges and internet access are configured against a network, not against an
          interface.
        </p>
      </header>

      <PartialApply editor={editor} />

      <div className="flex justify-end">
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
