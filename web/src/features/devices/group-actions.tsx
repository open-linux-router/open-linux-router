import { useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { GroupNameDialog } from '@/features/devices/group-name-dialog'
import {
  describeError,
  useCreateDeviceGroup,
  useDeleteDeviceGroup,
  useRenameDeviceGroup,
  useSetDeviceGroup,
} from '@/features/devices/queries'
import type { DeviceRow } from '@/lib/api-types'

/**
 * Making, renaming and removing groups, and moving a device between them.
 *
 * Shaped like useDeviceActions: a hook that owns the dialogs and hands back
 * functions to open them, so the map can offer "Rename" from a card's menu
 * without owning any of the forms. None of this goes through a plan preview —
 * a group carries no policy today, so nothing here can disconnect anybody,
 * and §5.1's instant apply is the right shape. The one destructive action,
 * deleting a group, still asks, because it undoes work: every membership in it
 * is dropped, and there is no list anywhere to rebuild it from.
 */
export function useGroupActions() {
  const create = useCreateDeviceGroup()
  const rename = useRenameDeviceGroup()
  const remove = useDeleteDeviceGroup()
  const setGroup = useSetDeviceGroup()

  const [naming, setNaming] = useState<{ from?: string } | null>(null)
  const [deleting, setDeleting] = useState<{ name: string; members: number } | null>(null)

  /**
   * Moves one device, with a toast rather than a dialog: the menu item already
   * said where it was going, and asking again would be a confirmation of a
   * choice the operator made one click ago.
   */
  async function move(device: DeviceRow, group: string) {
    try {
      await setGroup.mutateAsync({ mac: device.mac, group })
      const who = device.name || device.mac
      toast.success(group ? `${who} moved to ${group}` : `${who} is no longer in a group`)
    } catch (error) {
      toastError(error)
    }
  }

  const dialogs = (
    <>
      {naming && (
        <GroupNameDialog
          // Keyed so reopening for a different group starts from its name
          // rather than the last draft.
          key={naming.from ?? ' new'}
          from={naming.from}
          busy={create.isPending || rename.isPending}
          onOpenChange={(open) => !open && setNaming(null)}
          onSubmit={async (name) => {
            if (naming.from === undefined) {
              await create.mutateAsync(name)
              toast.success(`Group ${name} created`)
            } else if (name !== naming.from) {
              await rename.mutateAsync({ from: naming.from, to: name })
              toast.success(`Renamed to ${name}`)
            }
            setNaming(null)
          }}
        />
      )}

      {deleting && (
        <Dialog open onOpenChange={(open) => !open && setDeleting(null)}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Delete {deleting.name}?</DialogTitle>
              <DialogDescription>
                {deleting.members === 0
                  ? 'Nothing is in it, so nothing else changes.'
                  : `Its ${deleting.members === 1 ? 'device' : `${deleting.members} devices`} will show as ungrouped. Names, addresses and how ${deleting.members === 1 ? 'it connects' : 'they connect'} stay exactly as they are.`}
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setDeleting(null)}>
                Cancel
              </Button>
              <Button
                variant="destructive"
                disabled={remove.isPending}
                onClick={async () => {
                  try {
                    await remove.mutateAsync(deleting.name)
                    toast.success(`Group ${deleting.name} deleted`)
                    setDeleting(null)
                  } catch (error) {
                    toastError(error)
                  }
                }}
              >
                Delete group
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </>
  )

  return {
    create: () => setNaming({}),
    rename: (name: string) => setNaming({ from: name }),
    remove: (name: string, members: number) => setDeleting({ name, members }),
    move,
    dialogs,
  }
}

function toastError(error: unknown) {
  toast.error(describeError(error))
}
