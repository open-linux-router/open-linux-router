import { AlertTriangle, ArrowLeft } from 'lucide-react'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { api } from '@/lib/api'

type Config = { enabled: boolean; proxy?: string }
type Status = { enabled: boolean; service: { active: boolean }; problem?: string }

export function SocksOutPage() {
  const client = useQueryClient()
  const config = useQuery({ queryKey: ['socksout', 'config'], queryFn: () => api.get<Config>('/api/socksout/config') })
  const status = useQuery({ queryKey: ['socksout', 'status'], queryFn: () => api.get<Status>('/api/socksout/status'), refetchInterval: 5000 })
  const [draft, setDraft] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const save = useMutation({
    mutationFn: (next: Config) => api.put('/api/socksout/config', next),
    onSuccess: () => {
      setError(null)
      setDraft(null)
      void client.invalidateQueries({ queryKey: ['socksout'] })
    },
    onError: (err: Error) => setError(err.message),
  })

  return (
    <div className="mx-auto max-w-2xl space-y-5">
      <Link to="/access" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"><ArrowLeft className="size-4" /> Access</Link>
      <div>
        <h1 className="font-heading text-2xl font-semibold">SOCKS5 from home</h1>
        <p className="mt-1 text-sm text-muted-foreground">OLR creates a TUN interface so selected home devices can use an external SOCKS5 proxy without changing their settings.</p>
      </div>
      {config.isError && <Alert variant="destructive"><AlertTriangle /><AlertTitle>Could not load settings</AlertTitle><AlertDescription>{(config.error as Error).message}</AlertDescription></Alert>}
      {error && <Alert variant="destructive"><AlertTriangle /><AlertTitle>Could not apply</AlertTitle><AlertDescription>{error}</AlertDescription></Alert>}
      {status.data?.problem && <Alert><AlertTriangle /><AlertTitle>Backend needs attention</AlertTitle><AlertDescription>{status.data.problem}</AlertDescription></Alert>}
      <Card>
        <CardHeader>
          <CardTitle>Proxy connection</CardTitle>
          <CardDescription>{status.data?.service.active ? 'TUN is running' : 'TUN is not running'} · IPv4 only · no SOCKS5 authentication yet</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <Label htmlFor="socks-out-proxy">SOCKS5 server</Label>
          <Input id="socks-out-proxy" value={draft ?? config.data?.proxy ?? ''} onChange={(e) => setDraft(e.target.value)} placeholder="socks5://192.0.2.10:1080" autoComplete="off" spellCheck={false} />
          <p className="text-xs text-muted-foreground">Use an IP address and port. SOCKS5 credentials and hostnames are not supported yet.</p>
          <div className="flex flex-wrap gap-2">
            <Button disabled={save.isPending || config.isPending || !(draft ?? config.data?.proxy)} onClick={() => save.mutate({ enabled: true, proxy: draft ?? config.data?.proxy })}>Save and start</Button>
            {config.data?.enabled && <Button variant="outline" disabled={save.isPending} onClick={() => save.mutate({ ...config.data, enabled: false })}>Stop</Button>}
          </div>
        </CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle>Choose who uses it</CardTitle><CardDescription>Starting the proxy does not change any device's route. Add an interface exit for <code>olrsocks0</code>, then assign that exit to a network or device. IPv6 and backend failure block rather than bypass the proxy.</CardDescription></CardHeader>
        <CardContent><Button variant="outline" render={<Link to="/gateway/exits">Open ways out</Link>} /></CardContent>
      </Card>
    </div>
  )
}
