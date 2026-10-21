import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { PageHeading } from '@/components/layout/page-heading'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'

interface Preview {
  modules: string[]
  revision: string
  warning: string
}

export function BackupPage() {
  const client = useQueryClient()
  const [draft, setDraft] = useState<string | null>(null)
  const [preview, setPreview] = useState<Preview | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')

  async function download() {
    setBusy(true)
    setError('')
    try {
      const config = await api.get<string>('/api/configuration/config', text => text)
      const url = URL.createObjectURL(new Blob([config], { type: 'application/json' }))
      const anchor = document.createElement('a')
      anchor.href = url
      anchor.download = 'olr.json'
      anchor.click()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Backup failed.')
    } finally {
      setBusy(false)
    }
  }

  async function validate(file?: File) {
    setDraft(null)
    setPreview(null)
    setMessage('')
    setError('')
    if (!file) return
    setBusy(true)
    try {
      if (file.size > 4 * 1024 * 1024) throw new Error('Configuration backup exceeds 4 MiB.')
      // Preserve native nftables counters and other uint64 values exactly.
      const text = await file.text()
      const result = await api.send<Preview>('POST', '/api/configuration/restore?dry_run=true', text, undefined, value => value as string)
      setDraft(text)
      setPreview(result)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not validate this backup.')
    } finally {
      setBusy(false)
    }
  }

  async function restore() {
    if (!draft || !preview) return
    setBusy(true)
    setError('')
    try {
      await api.send('POST', '/api/configuration/restore?confirm=true', draft, { 'If-Match': preview.revision }, value => value as string)
      setPreview(null)
      setDraft(null)
      setMessage('Settings restored. Check network access and keep the change using the confirmation notice above. If asked to sign in, use the password saved in the backup.')
      void client.invalidateQueries()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Restore failed. Check the recovery notice before retrying.')
      void client.invalidateQueries()
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-4">
      <PageHeading title="Configuration backup" />
      <Card>
        <CardHeader><CardTitle>Back up settings</CardTitle></CardHeader>
        <CardContent className="space-y-4">
          <p className="text-sm text-muted-foreground">Download the complete olr.json, including administrator password hashes, VPN keys and provider credentials. Store it privately and use HTTPS when transferring it.</p>
          <p className="text-sm text-muted-foreground">External TLS files, daemon startup options, Caddy-issued certificates and runtime history are separate. Network interfaces must use the same names when restoring on another machine.</p>
          <Button variant="secondary" disabled={busy} onClick={() => void download()}>Download olr.json</Button>
        </CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle>Restore settings</CardTitle></CardHeader>
        <CardContent className="space-y-4">
          <div className="max-w-xl space-y-2">
            <Label htmlFor="configuration-backup">Backup file</Label>
            <Input id="configuration-backup" type="file" accept=".json,application/json" disabled={busy} onChange={event => void validate(event.target.files?.[0])} />
          </div>
          {preview && <Alert><AlertDescription>
            <p>{preview.warning}</p>
            <p className="break-words">Sections: {preview.modules.join(', ') || 'None (clears all settings)'}</p>
            <p>After restoring, confirm within two minutes or the previous settings will return automatically. You may need the administrator password from this backup.</p>
            <Button className="mt-3" disabled={busy} onClick={() => void restore()}>Replace settings and restore</Button>
          </AlertDescription></Alert>}
          {error && <p role="alert" className="break-words text-sm text-destructive">{error}</p>}
          {message && <p role="status" className="text-sm">{message}</p>}
        </CardContent>
      </Card>
    </div>
  )
}
