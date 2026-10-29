import { t, useLocale } from '@/lib/i18n'
import { Network, TriangleAlert } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Switch } from '@/components/ui/switch'
import type { SocksStatus } from '@/lib/api-types'

import { ProxyBadge } from './proxy-card'
import { ProxyLinkDialog } from './proxy-link'
import { socksChange, useSocksLink } from './queries'
import type { useRemoteApply } from './use-apply'


export function SocksCard({
  status,
  applier,
}: {
  status?: SocksStatus
  applier: ReturnType<typeof useRemoteApply>
}) {
  useLocale()
  const [link, setLink] = useState<{ url: string; label?: string } | null>(null)
  const fetchLink = useSocksLink()

  const enabled = status?.enabled ?? false
  const running = status?.service?.active ?? false
  const missing = Boolean(status?.binary_error)

  function showLink() {
    fetchLink.mutate(undefined, {
      onSuccess: (got) => setLink({ url: got.url, label: got.label }),
      // 409 carries which half is missing — no password yet, or no tunnel
      // address — and that message is already the instruction.
      onError: (err: Error) => toast.error(err.message),
    })
  }

  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Network className="size-4 text-muted-foreground" />
            SOCKS5
            <ProxyBadge status={status} />
          </CardTitle>
          <CardDescription>{t('Unencrypted internet proxy with authentication; no access to your home network.')}</CardDescription>
          <CardAction className="flex items-center gap-2">
            <Switch
              checked={enabled}
              onCheckedChange={(next) => applier.submit(socksChange.enabled(next))}
              busy={applier.busy}
              aria-label="SOCKS5"
            />
          </CardAction>
        </CardHeader>

        <CardContent className="space-y-2 text-sm text-muted-foreground">
          {missing ? (
            <Alert variant="destructive">
              <TriangleAlert />
              <AlertTitle>{t('No SOCKS5 server on this box')}</AlertTitle>
              <AlertDescription className="whitespace-pre-line font-mono text-xs">
                {status?.binary_error}
              </AlertDescription>
            </Alert>
          ) : enabled ? (
            <p>
              {' '}
              {t('Listening on TCP port')} <span className="font-mono">{status?.listen_port}</span>,{' '}
              {t('on every interface, including the internet.')}
            </p>
          ) : (
            <p>{t('Starting generates a password. Listens on every interface; no VPN required.')}</p>
          )}

          {enabled && status?.warning && (
            <Alert variant="destructive">
              <TriangleAlert />
              <AlertTitle>{t('Open to the internet, unencrypted')}</AlertTitle>
              <AlertDescription>{status.warning}</AlertDescription>
            </Alert>
          )}

          {enabled && !missing && !running && status?.service && (
            <p className="text-destructive"> {t('The configuration is stored, but the server is not running.')} </p>
          )}

          {status?.drifted && (
            <p>
              The box no longer matches what is stored here — the file or the service was changed
              somewhere else.
            </p>
          )}

          {enabled && !missing && (
            <div className="pt-1">
              <Button size="sm" variant="secondary" onClick={showLink} disabled={fetchLink.isPending}>
                {fetchLink.isPending ? t('Fetching…') : t('Show link')}
              </Button>
            </div>
          )}
        </CardContent>
      </Card>

      {link && (
        <ProxyLinkDialog
          protocol="SOCKS5"
          url={link.url}
          label={link.label}
          open
          onOpenChange={(open) => !open && setLink(null)}
        />
      )}
    </>
  )
}
