import { t, useLocale } from '@/lib/i18n'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { SubPage } from '@/components/layout/sub-page'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useAdminAccess } from '@/features/system/queries'
import { api, setToken } from '@/lib/api'

export function AdministratorAccessPage() {
  useLocale()
  const client = useQueryClient()
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [removing, setRemoving] = useState(false)
  const access = useAdminAccess()
  const save = useMutation({
    mutationFn: (noPassword: boolean) => api.put('/api/system/access/password', { password: noPassword ? '' : password, no_password: noPassword }),
    onSuccess: () => { setPassword(''); setConfirmation(''); client.clear(); window.location.reload() },
  })
  const logout = useMutation({
    mutationFn: () => api.post('/api/system/access/logout'),
    onSuccess: () => { setToken(null); client.clear(); window.location.reload() },
  })
  return <SubPage section="/advanced" slug="access">
    <Card>
      <CardHeader>
        <CardTitle>{t('Password and sessions')}</CardTitle>
        <CardDescription>{access.data?.password_set ? 'A password protects this router.' : 'No administrator password is set.'} {t('Changing it signs out every browser.')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4 [&>*]:max-w-[45rem]">
        <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); save.mutate(false) }}>
          <div className="space-y-2">
            <Label htmlFor="new-password">{t('New password')}</Label>
            <Input id="new-password" type="password" autoComplete="new-password" value={password} onChange={(event) => setPassword(event.target.value)} minLength={12} maxLength={1024} required disabled={save.isPending} />
            <p className="text-sm text-muted-foreground">{t('Use at least 12 characters.')}</p>
          </div>
          <div className="space-y-2">
            <Label htmlFor="repeat-password">{t('Repeat password')}</Label>
            <Input id="repeat-password" type="password" autoComplete="new-password" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} required disabled={save.isPending} />
          </div>
          <Button type="submit" className="h-11 w-full" disabled={save.isPending || [...password].length < 12 || password !== confirmation}>{save.isPending ? t('Saving…') : 'Save password'}</Button>
        </form>
        {save.error && <p className="text-sm text-destructive" role="alert">{save.error.message}</p>}
        {access.data?.password_set && <>
          {!removing ? <Button variant="secondary" className="h-11 w-full" onClick={() => setRemoving(true)}>{t('Remove password…')}</Button> : <div className="space-y-3 rounded-md border p-3">
            <p className="text-sm">{t('Anyone who can reach this router will be able to configure it. Remove the password?')}</p>
            <div className="flex gap-3">
              <Button variant="danger" className="h-11" onClick={() => save.mutate(true)} disabled={save.isPending}>{t('Remove password')}</Button>
              <Button variant="quiet" className="h-11" onClick={() => setRemoving(false)} disabled={save.isPending}>{t('Cancel')}</Button>
            </div>
          </div>}
        </>}
        <Button variant="secondary" className="h-11 w-full" onClick={() => logout.mutate()} disabled={logout.isPending}>{t('Sign out')}</Button>
        {logout.error && <p className="text-sm text-destructive" role="alert">{logout.error.message}</p>}
      </CardContent>
    </Card>
  </SubPage>
}
