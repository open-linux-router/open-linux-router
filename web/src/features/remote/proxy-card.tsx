import { Globe, TriangleAlert } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
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
import type { ProxyStatus } from '@/lib/api-types'
import type { Shadowsocks } from '@/lib/config-types'

import { WayMark } from './way-mark'

import { ProxyLinkDialog } from './proxy-link'
import { shadowsocksChange, useShadowsocksLink } from './queries'
import { ShadowsocksSettings } from './shadowsocks-settings'
import type { useRemoteApply } from './use-apply'

/**
 * The proxy, beside the tunnel on the same page.
 *
 * One card rather than a second page, and the reason is the sentence it opens
 * with. These two objects look interchangeable on a settings screen and are
 * not: a device on the tunnel is *inside* the network and can reach the NAS; a
 * device on the proxy can reach exactly what the internet can reach, from this
 * router's address. An operator may well want both, and the only place that
 * distinction can be made clear is where they sit side by side.
 *
 * So the title is the protocol's plain name, and the description says what a
 * client gets from it — and what it does not.
 */
export function ProxyCard({
  status,
  config,
  applier,
}: {
  status?: ProxyStatus
  config: Shadowsocks
  applier: ReturnType<typeof useRemoteApply>
}) {
  const [editing, setEditing] = useState(false)
  const [link, setLink] = useState<{ url: string; label?: string } | null>(null)
  const fetchLink = useShadowsocksLink()

  const enabled = status?.enabled ?? false
  const running = status?.service?.active ?? false
  const missing = Boolean(status?.binary_error)

  function toggle(next: boolean) {
    // The same submit path as every other change on this page, so a disruptive
    // proxy change lands in the same confirmation dialog rather than growing a
    // second one that would have to stay in step with it.
    applier.submit(shadowsocksChange.enabled(next))
  }

  function showLink() {
    fetchLink.mutate(undefined, {
      onSuccess: (got) => setLink({ url: got.url, label: got.label }),
      onError: (err: Error) => {
        // The daemon answers 409 when the configuration is not far enough along
        // to produce a link, and its message says which half is missing — no
        // endpoint, or no password yet. Passing it straight through is better
        // than a generic failure, because it is already the instruction.
        toast.error(err.message)
      },
    })
  }

  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <WayMark way="shadowsocks" fallback={Globe} className="size-4" />
            Shadowsocks
            <ProxyBadge status={status} />
          </CardTitle>
          <CardDescription>
            An encrypted proxy. Clients reach the internet through this router, and nothing on
            your network.
          </CardDescription>
          <CardAction className="flex items-center gap-2">
            <Button size="sm" variant="outline" onClick={() => setEditing(true)} disabled={applier.busy}>
              Settings
            </Button>
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
              onCheckedChange={toggle}
              disabled={applier.busy}
              aria-label="Shadowsocks"
            />
          </CardAction>
        </CardHeader>

        <CardContent className="space-y-2 text-sm text-muted-foreground">
          {missing ? (
            // Not a failure state to apologise for: the operator has to fetch a
            // binary, and the message from the daemon is the instructions.
            // Rendered whitespace-pre-line because those instructions are
            // shell, verbatim and multi-line.
            <Alert variant="destructive">
              <TriangleAlert />
              <AlertTitle>No Shadowsocks server on this box</AlertTitle>
              <AlertDescription className="whitespace-pre-line font-mono text-xs">
                {status?.binary_error}
              </AlertDescription>
            </Alert>
          ) : enabled ? (
            <p>
              Listening on TCP and UDP port{' '}
              <span className="font-mono">{status?.listen_port}</span>, encrypted with{' '}
              <span className="font-mono">{status?.cipher}</span>.
              {!status?.udp && ' UDP is turned off, so name lookups and QUIC will not go through.'}
            </p>
          ) : (
            <p>
              Off. Turning it on generates a password and starts the server. Every client uses the
              same link.
            </p>
          )}

          {enabled && !missing && !running && status?.service && (
            <p className="text-destructive">
              The configuration is stored, but the server is not running.
            </p>
          )}

          {status?.drifted && (
            // Drift is design.md §5.4: intent and the box disagree. Said plainly
            // rather than fixed silently, because something outside olr changed
            // the file or stopped the unit and the operator should know that
            // happened.
            <p>
              The box no longer matches what is stored here — the file or the service was changed
              somewhere else.
            </p>
          )}
        </CardContent>
      </Card>

      {editing && (
        <ShadowsocksSettings
          initial={config}
          onOpenChange={setEditing}
          onSubmit={(fields) => applier.submit(shadowsocksChange.settings(fields))}
        />
      )}

      {link && (
        <ProxyLinkDialog
          protocol="Shadowsocks"
          url={link.url}
          label={link.label}
          open
          onOpenChange={(open) => !open && setLink(null)}
        />
      )}
    </>
  )
}

/** Shared with the SOCKS5 card: both are a supervised unit and a binary. */
export function ProxyBadge({
  status,
}: {
  status?: Pick<ProxyStatus, 'enabled' | 'binary_error' | 'service' | 'service_error'>
}) {
  if (!status) return null
  if (status.binary_error) return <Badge variant="destructive">Not installed</Badge>
  if (!status.enabled) return null
  // "We could not tell" and "it is off" are different answers and only one of
  // them is honest (design.md §3.4), so a box with no D-Bus gets neither badge.
  if (status.service_error) return null
  return status.service?.active ? (
    <Badge variant="secondary">Running</Badge>
  ) : (
    <Badge variant="destructive">Stopped</Badge>
  )
}
