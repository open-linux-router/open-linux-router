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

/**
 * The SOCKS5 proxy, under Shadowsocks.
 *
 * Same card, same write path, one difference that decides the copy: SOCKS5 is
 * plaintext, so *where it listens* is the whole story. By default only a
 * device already on WireGuard can reach it, and the card says so; when it is
 * exposed to the internet the daemon's own warning is shown verbatim rather
 * than a second wording that could drift from it.
 */
export function SocksCard({
  status,
  applier,
}: {
  status?: SocksStatus
  applier: ReturnType<typeof useRemoteApply>
}) {
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
          <CardDescription>
            An unencrypted proxy with a username and password. Clients reach the internet through
            this router, and nothing on your network.
          </CardDescription>
          <CardAction className="flex items-center gap-2">
            {enabled && !missing && (
              <Button
                size="sm"
                variant="outline"
                onClick={showLink}
                disabled={fetchLink.isPending}
              >
                {fetchLink.isPending ? 'Fetching…' : 'Show link'}
              </Button>
            )}
            <Switch
              checked={enabled}
              onCheckedChange={(next) => applier.submit(socksChange.enabled(next))}
              disabled={applier.busy}
              aria-label="SOCKS5"
            />
          </CardAction>
        </CardHeader>

        <CardContent className="space-y-2 text-sm text-muted-foreground">
          {missing ? (
            <Alert variant="destructive">
              <TriangleAlert />
              <AlertTitle>No SOCKS5 server on this box</AlertTitle>
              <AlertDescription className="whitespace-pre-line font-mono text-xs">
                {status?.binary_error}
              </AlertDescription>
            </Alert>
          ) : enabled ? (
            <p>
              Listening on TCP port <span className="font-mono">{status?.listen_port}</span>,{' '}
              {status?.exposed
                ? 'on every interface, including the internet.'
                : 'inside WireGuard only. Connect over WireGuard first, then use this proxy.'}
            </p>
          ) : (
            <p>
              Off. Turning it on generates a password and starts the server. By default it listens
              inside WireGuard only, because SOCKS5 does not encrypt anything.
            </p>
          )}

          {enabled && status?.warning && (
            <Alert variant="destructive">
              <TriangleAlert />
              <AlertTitle>Open to the internet, unencrypted</AlertTitle>
              <AlertDescription>{status.warning}</AlertDescription>
            </Alert>
          )}

          {enabled && !missing && !running && status?.service && (
            <p className="text-destructive">
              The configuration is stored, but the server is not running.
            </p>
          )}

          {status?.drifted && (
            <p>
              The box no longer matches what is stored here — the file or the service was changed
              somewhere else.
            </p>
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
