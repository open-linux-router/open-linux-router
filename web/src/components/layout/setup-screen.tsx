import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Router, TriangleAlert } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { api } from '@/lib/api'

/**
 * The first screen on a box nobody has set up.
 *
 * olrd serves its UI from the moment it is installed, and an unclaimed box
 * answers 409 for every API route except this one (docs/system.md §3). So this
 * is not a welcome mat — it is the only thing that works, and clicking it is
 * what makes the rest of the app usable.
 *
 * One choice, stated with its consequence in the same breath. The second
 * option — requiring a password — needs a login screen to be worth anything,
 * and storing a credential that nothing enforces would be worse than not
 * offering it, so it arrives together with that screen rather than before it.
 */
export function SetupScreen() {
  const queryClient = useQueryClient()

  const claim = useMutation({
    mutationFn: () => api.post('/api/system/access/claim', { password: '', no_password: true }),
    // Every query on the box was failing for one reason. Refetch them all
    // rather than making the operator reload the page they just arrived at.
    onSuccess: () => queryClient.invalidateQueries(),
  })

  return (
    <div className="flex min-h-svh items-center justify-center p-6">
      <Card className="w-full max-w-lg">
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Router className="size-4" aria-hidden />
            Set up this router
          </CardTitle>
          <CardDescription>
            Nothing has been configured yet, and until it is, this router will not accept
            changes over the network.
          </CardDescription>
        </CardHeader>

        <CardContent className="space-y-4">
          <div className="flex gap-3 rounded-md border border-amber-500/40 bg-amber-500/5 p-3">
            <TriangleAlert className="mt-0.5 size-4 shrink-0 text-amber-600" aria-hidden />
            <p className="text-sm text-muted-foreground">
              There is <strong className="text-foreground">no password</strong>. Anyone who can
              reach this router on your network will be able to configure it. That is usually
              what you want on a home network you control.
            </p>
          </div>

          <p className="text-sm text-muted-foreground">
            Requiring a password instead is coming in the next release. You can add one then
            without setting this up again.
          </p>

          {claim.error && (
            <p className="text-sm text-destructive" role="alert">
              {claim.error instanceof Error ? claim.error.message : 'Could not set this router up.'}
            </p>
          )}

          <Button className="w-full" onClick={() => claim.mutate()} disabled={claim.isPending}>
            {claim.isPending ? 'Setting up…' : 'Continue without a password'}
          </Button>

          <p className="text-xs text-muted-foreground">
            Only a machine on this network can set this router up. From anywhere else, run{' '}
            <code className="rounded bg-muted px-1 py-0.5">sudo olr claim --no-password</code> on
            the box.
          </p>
        </CardContent>
      </Card>
    </div>
  )
}
