import { useQueryClient } from '@tanstack/react-query'
import { KeyRound } from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { getToken, setToken } from '@/lib/api'

/**
 * Sets the API token the UI sends as a bearer credential.
 *
 * olrd generates the token into /etc/open-linux-router/api-token on first
 * start. There is no user model yet — design.md §10 folds "who may log into
 * olr" into the unbuilt system module — so this is deliberately a credential
 * box and not a login form. It should be replaced wholesale, not grown into
 * one.
 */
export function TokenButton() {
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [value, setValue] = useState('')

  function save() {
    setToken(value.trim() || null)
    setOpen(false)
    // Every query failed for the same reason; refetch them all rather than
    // making the operator reload the page.
    queryClient.invalidateQueries()
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (next) setValue(getToken() ?? '')
      }}
    >
      <DialogTrigger
        render={
          <Button variant="ghost" size="icon" aria-label="Set API token">
            <KeyRound className="size-4" aria-hidden />
          </Button>
        }
      />
      <DialogContent>
        <DialogHeader>
          <DialogTitle>API token</DialogTitle>
          <DialogDescription>
            Sent as a bearer token with every request. olrd writes it to
            <code className="mx-1 rounded bg-muted px-1 py-0.5 text-xs">
              /etc/open-linux-router/api-token
            </code>
            on first start. Not needed over the unix socket, which is
            authenticated by its file permissions.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-2">
          <Label htmlFor="api-token">Token</Label>
          <Input
            id="api-token"
            type="password"
            autoComplete="off"
            spellCheck={false}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && save()}
          />
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button onClick={save}>Save</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
