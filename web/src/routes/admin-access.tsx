import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { PageHeading } from '@/components/layout/page-heading'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { api, setToken } from '@/lib/api'
import type { AccessView } from '@/lib/api'

export function AdminAccessPage() {
  const client = useQueryClient()
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [removing, setRemoving] = useState(false)
  const access = useQuery({ queryKey: ['system', 'access'], queryFn: () => api.get<AccessView>('/api/system/access') })
  const save = useMutation({
    mutationFn: (noPassword: boolean) => api.put('/api/system/access/password', { password: noPassword ? '' : password, no_password: noPassword }),
    onSuccess: () => { setPassword(''); setConfirmation(''); client.clear(); window.location.reload() },
  })
  const logout = useMutation({
    mutationFn: () => api.post('/api/system/access/logout'),
    onSuccess: () => { setToken(null); client.clear(); window.location.reload() },
  })
  return <div className="space-y-8">
    <PageHeading title="Administrator access" back={{ to: '/', label: 'Network overview' }} />
    <Card>
      <CardHeader>
        <CardTitle>Password and sessions</CardTitle>
        <CardDescription>{access.data?.password_set ? 'A password protects this router.' : 'No administrator password is set.'} Changing it signs out every browser.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4 [&>*]:max-w-[45rem]">
        <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); save.mutate(false) }}>
          <div className="space-y-2">
            <Label htmlFor="new-password">New password</Label>
            <Input id="new-password" type="password" autoComplete="new-password" value={password} onChange={(event) => setPassword(event.target.value)} minLength={12} maxLength={1024} required disabled={save.isPending} />
            <p className="text-sm text-muted-foreground">Use at least 12 characters.</p>
          </div>
          <div className="space-y-2">
            <Label htmlFor="repeat-password">Repeat password</Label>
            <Input id="repeat-password" type="password" autoComplete="new-password" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} required disabled={save.isPending} />
          </div>
          <Button type="submit" className="h-11 w-full" disabled={save.isPending || [...password].length < 12 || password !== confirmation}>{save.isPending ? 'Saving…' : 'Save password'}</Button>
        </form>
        {save.error && <p className="text-sm text-destructive" role="alert">{save.error.message}</p>}
        {access.data?.password_set && <>
          {!removing ? <Button variant="outline" className="h-11 w-full" onClick={() => setRemoving(true)}>Remove password…</Button> : <div className="space-y-3 rounded-md border p-3">
            <p className="text-sm">Anyone who can reach this router will be able to configure it. Remove the password?</p>
            <div className="flex gap-3">
              <Button variant="destructive" className="h-11" onClick={() => save.mutate(true)} disabled={save.isPending}>Remove password</Button>
              <Button variant="outline" className="h-11" onClick={() => setRemoving(false)} disabled={save.isPending}>Cancel</Button>
            </div>
          </div>}
        </>}
        <Button variant="outline" className="h-11 w-full" onClick={() => logout.mutate()} disabled={logout.isPending}>Sign out</Button>
        {logout.error && <p className="text-sm text-destructive" role="alert">{logout.error.message}</p>}
      </CardContent>
    </Card>
  </div>
}
