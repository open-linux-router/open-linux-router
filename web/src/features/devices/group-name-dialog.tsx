import { useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { describeError } from '@/features/devices/queries'

/** internal/devices/config.go MaxGroupNameLen. The server is still the judge. */
const MAX_GROUP_NAME = 32

/**
 * The one form both creating and renaming use: a name.
 *
 * Errors from the server are shown under the field rather than in a toast.
 * The ones worth having — "there is already a group called IoT", "name is 40
 * characters; the limit is 32" — are about what was just typed, and a toast
 * would disappear while the operator was still looking at the field to fix.
 */
export function GroupNameDialog({
  from,
  busy,
  onOpenChange,
  onSubmit,
}: {
  /** Undefined when creating. */
  from?: string
  busy: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (name: string) => Promise<void>
}) {
  const [name, setName] = useState(from ?? '')
  const [error, setError] = useState<string | null>(null)
  const trimmed = name.trim()
  const tooLong = [...trimmed].length > MAX_GROUP_NAME

  async function submit() {
    setError(null)
    try {
      await onSubmit(trimmed)
    } catch (e) {
      setError(describeError(e))
    }
  }

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault()
            if (trimmed && !tooLong) void submit()
          }}
        >
          <DialogHeader>
            <DialogTitle>{from === undefined ? 'New group' : `Rename ${from}`}</DialogTitle>
            <DialogDescription>
              {from === undefined
                ? 'A set of your devices — Serving, Personal, IoT, whatever makes sense to you. It changes how the overview is laid out, and nothing about how anything connects.'
                : 'Every device in it moves with it.'}
            </DialogDescription>
          </DialogHeader>

          <div className="grid gap-2">
            <Label htmlFor="group-name">Name</Label>
            <Input
              id="group-name"
              autoFocus
              value={name}
              placeholder="Personal"
              aria-invalid={Boolean(error) || tooLong}
              onChange={(e) => {
                setName(e.target.value)
                setError(null)
              }}
            />
            {tooLong ? (
              <p className="text-xs text-destructive">
                Keep it to {MAX_GROUP_NAME} characters or fewer.
              </p>
            ) : error ? (
              <p className="text-xs text-destructive">{error}</p>
            ) : null}
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy || !trimmed || tooLong}>
              {from === undefined ? 'Create group' : 'Rename'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
