import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Router, TriangleAlert } from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { api } from '@/lib/api'

/**
 * The first screen on a box nobody has set up.
 *
 * olrd serves its UI from the moment it is installed, and an unclaimed box
 * answers 409 for every API route except this one (docs/system.md §3). So this
 * is not a welcome mat — it is the only thing that works, and clicking it is
 * what makes the rest of the app usable.
 *
 * Password setup is the primary action; deliberate password-free access is
 * offered with its consequence stated beside the button.
 */
export function SetupScreen() {
  const queryClient = useQueryClient()
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')

  const claim = useMutation({
    mutationFn: (noPassword: boolean) => api.post('/api/system/access/claim', { password: noPassword ? '' : password, no_password: noPassword }),
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
          <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); claim.mutate(false) }}>
            <div className="space-y-2">
              <Label htmlFor="setup-password">Administrator password</Label>
              <Input id="setup-password" type="password" autoComplete="new-password" value={password} onChange={(event) => setPassword(event.target.value)} minLength={12} maxLength={1024} required disabled={claim.isPending} />
              <p className="text-sm text-muted-foreground">Use at least 12 characters.</p>
            </div>
            <div className="space-y-2">
              <Label htmlFor="setup-confirmation">Repeat password</Label>
              <Input id="setup-confirmation" type="password" autoComplete="new-password" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} required disabled={claim.isPending} />
            </div>
            <Button type="submit" className="h-11 w-full" disabled={claim.isPending || [...password].length < 12 || password !== confirmation}>
              {claim.isPending ? 'Setting up…' : 'Set password and continue'}
            </Button>
          </form>
          <div className="flex gap-3 rounded-md border border-amber-500/40 bg-amber-500/5 p-3">
            <TriangleAlert className="mt-0.5 size-4 shrink-0 text-amber-600" aria-hidden />
            <p className="text-sm text-muted-foreground">
              If you continue without a password, anyone who can reach this router can configure it.
            </p>
          </div>


          {claim.error && (
            <p className="text-sm text-destructive" role="alert">
              {claim.error instanceof Error ? claim.error.message : 'Could not set this router up.'}
            </p>
          )}

          <Button variant="outline" className="h-11 w-full" onClick={() => claim.mutate(true)} disabled={claim.isPending}>
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
