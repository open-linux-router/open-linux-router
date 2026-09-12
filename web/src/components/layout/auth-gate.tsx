import { useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound } from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { SetupScreen } from '@/components/layout/setup-screen'
import { ApiError, api, setToken } from '@/lib/api'

/** What GET /api/system/access answers. */
interface AccessView {
  claimed: boolean
  password_set: boolean
}

/**
 * Decides which of three screens the box gets: set up, log in, or the app.
 *
 * The order matters and is not arbitrary. **Claim is asked first**, because an
 * unclaimed box answers 409 for every route except this one — probing anything
 * else would report "conflict" for a box whose actual state is "brand new", and
 * the operator would be reading a status code instead of a sentence. GET
 * /api/system/access is served even while unclaimed for exactly this reason
 * (internal/daemon, openWhileUnclaimed).
 *
 * The token prompt stays for boxes started with --auth, and it works now: the
 * SPA is no longer served from behind the credential it collects, which is the
 * bug that made this component unreachable for its whole existence before
 * v0.1.6.
 */
export function AuthGate({ children }: { children: React.ReactNode }) {
  const access = useQuery({
    queryKey: ['system', 'access'],
    queryFn: () => api.get<AccessView>('/api/system/access'),
    retry: (count, error) => !(error instanceof ApiError && error.unauthorized) && count < 2,
  })

  if (access.isPending) {
    return (
      <div className="mx-auto max-w-6xl space-y-4 p-6">
        <Skeleton className="h-14 w-full" />
        <Skeleton className="h-48 w-full" />
      </div>
    )
  }

  // A box with --auth turns even this away until a token is set, so the token
  // prompt comes before the claim screen can be considered.
  if (access.error instanceof ApiError && access.error.unauthorized) {
    return <TokenPrompt />
  }

  if (access.data && !access.data.claimed) {
    return <SetupScreen />
  }

  return <>{children}</>
}

function TokenPrompt() {
  const queryClient = useQueryClient()
  const [value, setValue] = useState('')

  function submit(event: React.FormEvent) {
    event.preventDefault()
    setToken(value.trim() || null)
    queryClient.invalidateQueries()
  }

  return (
    <div className="flex min-h-svh items-center justify-center p-6">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <KeyRound className="size-4" aria-hidden />
            API token required
          </CardTitle>
          <CardDescription>
            olrd generated a token on first start. Read it on the router with{' '}
            <code className="rounded bg-muted px-1 py-0.5 text-xs">
              sudo cat /etc/open-linux-router/api-token
            </code>
            .
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={submit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="token">Token</Label>
              <Input
                id="token"
                type="password"
                autoFocus
                autoComplete="off"
                spellCheck={false}
                value={value}
                onChange={(e) => setValue(e.target.value)}
              />
            </div>
            <Button type="submit" className="w-full" disabled={!value.trim()}>
              Continue
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
