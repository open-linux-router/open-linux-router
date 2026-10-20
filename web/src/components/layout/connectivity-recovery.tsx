import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'

interface Recovery {
  state: 'idle' | 'pending' | 'recovering' | 'failed'
  id?: string
  deadline?: string
  error?: string
}

export function ConnectivityRecovery() {
  const client = useQueryClient()
  const [now, setNow] = useState(Date.now())
  const status = useQuery({ queryKey: ['connectivity-recovery'], queryFn: () => api.get<Recovery>('/api/recovery/status'), refetchInterval: 1000, retry: false })
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [])
  const action = useMutation({
    mutationFn: (name: 'confirm' | 'revert') => api.post(`/api/recovery/${name}`, { id: status.data?.id }),
    onSuccess: () => { void client.invalidateQueries() },
  })
  const trial = status.data
  if (!trial || trial.state === 'idle') return null
  const seconds = Math.max(0, Math.ceil((Date.parse(trial.deadline ?? '') - now) / 1000))
  return (
    <div className="mx-auto w-full max-w-7xl px-4 pt-4 sm:px-6 xl:px-8">
      <Alert>
        <AlertTitle>{trial.state === 'pending' ? 'Keep this network change?' : 'Restoring network settings'}</AlertTitle>
        <AlertDescription>
          <p>{trial.state === 'pending' ? `Check that you can reach the router and your network works. Previous settings will return automatically in ${seconds} seconds unless you keep this change. If disconnected, reconnect to the previous router address after the countdown.` : 'The change was not confirmed. Recovery will retry until the previous settings are restored.'}</p>
          {(trial.error || action.error) && <p role="alert" className="break-words">{trial.error || action.error?.message}</p>}
          {status.isError && <p role="alert">Cannot reach the router. Confirmation has not been sent; automatic recovery remains active.</p>}
          <div className="mt-3 flex flex-wrap gap-2">
            <Button size="sm" disabled={action.isPending || status.isError || trial.state !== 'pending' || seconds === 0} onClick={() => action.mutate('confirm')}>Keep changes</Button>
            <Button size="sm" variant="outline" disabled={action.isPending} onClick={() => action.mutate('revert')}>Revert now</Button>
          </div>
        </AlertDescription>
      </Alert>
    </div>
  )
}
