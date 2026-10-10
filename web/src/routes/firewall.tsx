import { AlertTriangle } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { StatusStrip } from '@/components/layout/status-strip'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useFirewallStatus, useReapplyFirewall, useSetFirewall } from '@/features/firewall/queries'
import { TrafficMap } from '@/features/firewall/traffic-map'
import { ApiError } from '@/lib/api'
import type { FirewallApplyResult, FirewallStatus } from '@/lib/api-types'

/**
 * The firewall: one switch, and a map of what can reach this router or its
 * inside networks.
 *
 * There is no rule editor, and that is the design rather than a gap
 * (docs/firewall.md). Every port listed below comes from something already
 * configured elsewhere, so the map is read-only — the way to close a port is
 * to switch off the thing that opened it.
 */
export function FirewallPage() {
  const status = useFirewallStatus()
  const set = useSetFirewall()
  const reapply = useReapplyFirewall()

  /** The daemon held the switch because it would cut this browser off. */
  const [lockout, setLockout] = useState<string | null>(null)

  async function send(enabled: boolean, confirm = false) {
    setLockout(null)
    try {
      await set.mutateAsync({ enabled, confirm })
      toast.success(enabled ? 'The firewall is on' : 'The firewall is off')
    } catch (error) {
      if (!(error instanceof ApiError)) {
        toast.error(String(error))
        return
      }
      const body = error.body as FirewallApplyResult | undefined
      if (error.status === 409 && body?.plan?.impact === 'disruptive') {
        setLockout(body.plan.warnings?.[0]?.message ?? error.message)
        return
      }
      toast.error(error.message, {
        description: error.problems.map((p) => p.message).join('\n') || undefined,
      })
    }
  }

  if (!status.data) {
    return (
      <div className="space-y-6">
        {status.error ? (
          <Alert variant="destructive">
            <AlertTriangle />
            <AlertTitle>Cannot reach the router</AlertTitle>
            <AlertDescription>{status.error.message}</AlertDescription>
          </Alert>
        ) : (
          <Skeleton className="h-24 w-full" />
        )}
      </div>
    )
  }
  const st = status.data
  const summary = summarize(st)

  return (
    <div className="space-y-6">
      <StatusStrip
        headline={summary.headline}
        detail={summary.detail}
        dot={summary.dot}
        control={{
          id: 'firewall-enabled',
          label: 'Firewall',
          checked: st.enabled,
          busy: set.isPending,
          onChange: (on) => void send(on),
        }}
        drifted={st.enabled && st.drifted}
        driftNote="The kernel does not hold the firewall's rules — something removed them, or an earlier change stopped halfway."
        driftAction={{
          label: 'Put them back',
          busy: reapply.isPending,
          onClick: () => reapply.mutate(),
        }}
      />

      {lockout && (
        <Alert variant="destructive">
          <AlertTriangle />
          <AlertTitle>This would cut you off</AlertTitle>
          <AlertDescription className="space-y-3">
            <p>{lockout}</p>
            <div className="flex gap-2">
              <Button size="sm" variant="outline" onClick={() => setLockout(null)}>
                Cancel
              </Button>
              <Button
                size="sm"
                variant="destructive"
                disabled={set.isPending}
                onClick={() => void send(true, true)}
              >
                Turn on anyway
              </Button>
            </div>
          </AlertDescription>
        </Alert>
      )}

      {st.problem && (
        <Alert>
          <AlertTriangle />
          <AlertTitle>Nothing is inside yet</AlertTitle>
          <AlertDescription>{st.problem}</AlertDescription>
        </Alert>
      )}

      <TrafficMap status={st} />
    </div>
  )
}

function summarize(st: FirewallStatus): {
  headline: string
  detail: string
  dot: string
} {
  if (!st.enabled) {
    return {
      headline: 'Off',
      detail:
        'Anything that can reach this router may connect to it, and to devices with public IPv6 addresses.',
      dot: 'bg-muted-foreground/40',
    }
  }
  if (!st.known) {
    return {
      headline: 'Status unknown',
      detail: 'The router could not read its own firewall rules.',
      dot: 'bg-warning',
    }
  }
  if (st.drifted || st.problem) {
    return {
      headline: 'Rules not verified',
      detail: 'The stored setting is on, but the configured protection could not be verified.',
      dot: 'bg-warning',
    }
  }
  const blocked = st.blocked_input + st.blocked_forward
  return {
    headline: 'On',
    detail:
      blocked > 0
        ? `${blocked.toLocaleString()} packets from outside turned away since the rules were last written.`
        : 'Connections from outside are blocked unless something here asked for them.',
    dot: 'bg-success',
  }
}
