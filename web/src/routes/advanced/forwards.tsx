import { AlertTriangle, Plus } from 'lucide-react'
import { useState } from 'react'

import { SettingsList } from '@/components/layout/settings-list'
import { StatusStrip } from '@/components/layout/status-strip'
import { SubPage } from '@/components/layout/sub-page'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { ApplyOutcome, useForwardsEditor } from '@/features/nat/editor'
import { ForwardDialog } from '@/features/nat/forward-dialog'
import { ForwardList } from '@/features/nat/forward-list'
import { forwardsChange, useForwardsStatus } from '@/features/nat/queries'
import type { Forward } from '@/lib/config-types'

/**
 * Port forwards, on their own page under Advanced.
 *
 * One kind of object and looking at it is the whole visit, so the forwards are
 * the page rather than a row leading to one. The only thing behind a row here
 * is the filtering olr did *not* put in place, which matters exactly when a
 * forward looks right and does not work.
 */
export function ForwardsPage() {
  const { config, busy, change, applier, gate } = useForwardsEditor()
  const status = useForwardsStatus()

  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<Forward | null>(null)

  if (!config) return gate
  const forwards = config.forwards ?? []
  const foreign = status.data?.foreign ?? []

  return (
    <SubPage section="/advanced" slug="forwards">
      <ApplyOutcome applier={applier} />

      {status.data && !status.data.known && (
        <Alert>
          <AlertTriangle />
          <AlertTitle>These forwards are saved but not in force</AlertTitle>
          <AlertDescription>
            This router could not read its own NAT rules, so nothing below is actually
            running. On Linux this usually means the daemon is missing permission to change them.
          </AlertDescription>
        </Alert>
      )}

      <StatusStrip
        headline={
          !config.enabled
            ? 'Off'
            : forwards.length === 0
              ? 'Nothing is forwarded in'
              : `${forwards.length} port${forwards.length === 1 ? '' : 's'} open from the internet`
        }
        detail={
          config.enabled
            ? 'Something on the internet connects to this router on a port, and reaches one device on your network instead.'
            : 'The Gateway module is switched off, so nothing from outside is reaching a device here. That one switch also stops this router\u2019s routing policy.'
        }
        dot={
          !config.enabled
            ? 'bg-muted-foreground/40'
            : status.data?.known === false
              ? 'bg-warning'
              : 'bg-success'
        }
        control={{
          id: 'gateway-enabled',
          label: 'Apply this router\u2019s routing policy and port forwards',
          checked: config.enabled,
          busy,
          onChange: (enabled) => change(forwardsChange.settings({ enabled })),
        }}
        drifted={config.enabled && status.data?.drifted}
        driftNote="Something changed the rules outside olr. Saving any change here puts them back."
      />

      <div className="space-y-3">
        <div className="flex items-center justify-between gap-3">
          <h2 className="px-1 text-sm font-medium text-muted-foreground">Forwards</h2>
          <Button size="sm" variant="outline" onClick={() => setAdding(true)}>
            <Plus />
            Add
          </Button>
        </div>

        <ForwardList forwards={forwards} status={status.data?.forwards} onEdit={setEditing} />

        {forwards.length > 0 && status.data?.known && (
          // Said once under the list rather than on every row: a small number
          // here is usually explained by a recent edit, and an operator reading
          // "never" wants to know that before they go and debug their ISP.
          <p className="text-[0.8rem] text-muted-foreground">
            Counts start again whenever a forward is added, changed or removed, and when the router
            restarts.
          </p>
        )}
      </div>

      {foreign.length > 0 && (
        <SettingsList
          section="/advanced"
          rows={[
            {
              slug: 'filtering',
              value: `${foreign.length} chain${foreign.length === 1 ? '' : 's'}`,
            },
          ]}
        />
      )}

      <ForwardDialog
        open={adding}
        onOpenChange={setAdding}
        onSubmit={(forward) => change(forwardsChange.saveForward(forward.name, forward))}
      />
      {editing && (
        <ForwardDialog
          open
          onOpenChange={(open) => !open && setEditing(null)}
          initial={editing}
          // The path carries the name this forward had; the body carries what it
          // should become. When those differ it is a rename, and the daemon
          // keeps the slot — and so the counter — across it.
          onSubmit={(forward) => change(forwardsChange.saveForward(editing.name, forward))}
          onRemove={() => {
            setEditing(null)
            change(forwardsChange.removeForward(editing.name))
          }}
        />
      )}
    </SubPage>
  )
}
