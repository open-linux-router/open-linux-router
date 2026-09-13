import { Plus } from 'lucide-react'
import { useState } from 'react'

import { SubPage } from '@/components/layout/sub-page'
import { Button } from '@/components/ui/button'
import { ApplyOutcome, useGatewayEditor } from '@/features/gateway/editor'
import { ExitDialog } from '@/features/gateway/exit-dialog'
import { ExitList } from '@/features/gateway/exit-list'
import { gatewayChange, useGatewayStatus } from '@/features/gateway/queries'
import type { Exit } from '@/lib/config-types'

export function GatewayExitsPage() {
  const { config, change, applier, gate } = useGatewayEditor()
  const status = useGatewayStatus()
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<Exit | null>(null)

  if (!config) return gate

  return (
    <SubPage section="/gateway" slug="exits">
      <ApplyOutcome applier={applier} />

      <div className="flex justify-end">
        <Button size="sm" variant="outline" onClick={() => setAdding(true)}>
          <Plus />
          Add
        </Button>
      </div>

      <ExitList exits={config.exits ?? []} status={status.data?.exits} onEdit={setEditing} />

      <ExitDialog
        open={adding}
        onOpenChange={setAdding}
        onSubmit={(exit) => change(gatewayChange.saveExit(exit.name, exit))}
      />
      {editing && (
        <ExitDialog
          open
          onOpenChange={(open) => !open && setEditing(null)}
          initial={editing}
          // The path carries the name this exit had; the body carries what it
          // should become. When those differ it is a rename, and the daemon
          // moves `default` and every assignment along with it — which is why
          // the name field can be edited here at all.
          onSubmit={(exit) => change(gatewayChange.saveExit(editing.name, exit))}
          onRemove={() => {
            setEditing(null)
            change(gatewayChange.removeExit(editing.name))
          }}
        />
      )}
    </SubPage>
  )
}
