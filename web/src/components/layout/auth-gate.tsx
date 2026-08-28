import { useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound } from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { ApiError, api, setToken } from '@/lib/api'

/**
 * Asks for the API token when olrd rejects us, and otherwise gets out of the way.
 *
 * The probe is GET /api/modules — the cheapest authenticated endpoint that
 * exists on every deployment. Over the unix socket, or with --no-auth on
 * loopback, it succeeds without a token and this component renders nothing of
 * its own.
 */
export function AuthGate({ children }: { children: React.ReactNode }) {
  const probe = useQuery({
    queryKey: ['modules'],
    queryFn: () => api.get<{ modules: string[] }>('/api/modules'),
    retry: (count, error) => !(error instanceof ApiError && error.unauthorized) && count < 2,
  })

  if (probe.isPending) {
    return (
      <div className="mx-auto max-w-6xl space-y-4 p-6">
        <Skeleton className="h-14 w-full" />
        <Skeleton className="h-48 w-full" />
      </div>
    )
  }

  if (probe.error instanceof ApiError && probe.error.unauthorized) {
    return <TokenPrompt />
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
