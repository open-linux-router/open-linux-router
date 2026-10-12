import { useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound } from 'lucide-react'
import { useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { SetupScreen } from '@/components/layout/setup-screen'
import { ApiError, api, setToken } from '@/lib/api'
import type { AccessView } from '@/lib/api'

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
 * Access status is public. Credentials protect the API; the SPA must load to
 * collect them. Session expiry returns here without exposing cached pages.
 */
export function AuthGate({ children }: { children: React.ReactNode }) {
  const queryClient = useQueryClient()
  const access = useQuery({
    queryKey: ['system', 'access'],
    queryFn: () => api.get<AccessView>('/api/system/access'),
    retry: (count, error) => !(error instanceof ApiError && error.unauthorized) && count < 2,
    refetchInterval: 30_000,
  })
  useEffect(() => {
    const refresh = () => queryClient.invalidateQueries({ queryKey: ['system', 'access'] })
    window.addEventListener('olr:unauthorized', refresh)
    return () => window.removeEventListener('olr:unauthorized', refresh)
  }, [queryClient])

  if (access.isPending) {
    return (
      <div className="mx-auto max-w-[90rem] space-y-4 p-6">
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

  if (access.error) {
    return <div className="mx-auto max-w-md space-y-4 p-6" role="alert">
      <p>Could not reach the router: {access.error.message}</p>
      <Button onClick={() => access.refetch()}>Try again</Button>
    </div>
  }
  if (access.data?.token_required && !access.data.authenticated) return <TokenPrompt />
  if (access.data?.password_set && !access.data.authenticated) return <PasswordPrompt />

  return <>{children}</>
}

function PasswordPrompt() {
  const queryClient = useQueryClient()
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  async function submit(event: React.FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError('')
    try {
      await api.post('/api/system/access/login', { password })
      setPassword('')
      queryClient.clear()
      window.location.reload()
    } catch (error) {
      setError(error instanceof Error ? error.message : 'Could not sign in')
    } finally { setBusy(false) }
  }
  return <div className="flex min-h-svh items-center justify-center p-6">
    <Card className="w-full max-w-md">
      <CardHeader>
        <CardTitle>Sign in to this router</CardTitle>
        <CardDescription>Enter the administrator password.</CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={submit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="password">Password</Label>
            <Input id="password" type="password" autoComplete="current-password" autoFocus value={password} onChange={(event) => setPassword(event.target.value)} disabled={busy} aria-describedby={error ? 'login-error' : undefined} />
          </div>
          {error && <p id="login-error" className="text-sm text-destructive" role="alert">{error}</p>}
          <Button type="submit" className="h-11 w-full" disabled={busy || !password}>{busy ? 'Signing in…' : 'Sign in'}</Button>
        </form>
      </CardContent>
    </Card>
  </div>
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
