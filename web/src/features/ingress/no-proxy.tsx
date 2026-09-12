import { AlertTriangle } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'

/**
 * Why this box cannot obtain a certificate, and what to do about it.
 *
 * One component rather than the same markup in two places, because the *title*
 * has to follow the body and the two are easy to let drift: the daemon reports
 * two different situations through one message, and "No proxy is installed"
 * above a message naming the proxy's own path reads as a bug in olr.
 *
 * The likelier of the two is the second. A stock Caddy — the distro's, Caddy's
 * own apt repository's, or the plain download — has no DNS provider modules at
 * all, because they are compiled in rather than loaded at runtime. So the
 * operator who did the obvious thing lands here, and telling them their proxy is
 * missing would send them to fix an installation that is fine.
 *
 * The message is the daemon's, whole and unstyled-but-monospaced: it contains
 * the two ways to get a usable binary, and on this path the text *is* the fix.
 */
export function NoProxyAlert({ message, binary }: { message: string; binary?: string }) {
  return (
    <Alert variant="destructive">
      <AlertTriangle />
      <AlertTitle>
        {binary ? 'The installed proxy cannot get a certificate' : 'No proxy is installed'}
      </AlertTitle>
      <AlertDescription className="whitespace-pre-wrap font-mono text-xs">
        {message}
      </AlertDescription>
    </Alert>
  )
}
