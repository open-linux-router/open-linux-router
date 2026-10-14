import { AlertTriangle } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router'
import { toast } from 'sonner'

import { StatusStrip } from '@/components/layout/status-strip'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useFirewallStatus, useReapplyFirewall, useSetFirewall } from '@/features/firewall/queries'
import { TrafficPermissions } from '@/features/firewall/traffic-permissions'
import { ApiError } from '@/lib/api'
import type { FirewallApplyResult, FirewallStatus } from '@/lib/api-types'

/** Firewall arrivals and status; native inspection and editing live on Rules. */
export function FirewallPage() {
  const status = useFirewallStatus()
  const set = useSetFirewall()
  const reapply = useReapplyFirewall()

  /** The daemon held the switch because it would cut this browser off. */
  const [lockout, setLockout] = useState<string | null>(null)
  const [pendingEnabled, setPendingEnabled] = useState(true)

  async function send(enabled: boolean, confirm = false) {
    setPendingEnabled(enabled)
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

  if (!status.data || status.error) {
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
        <Button variant="outline" nativeButton={false} render={<Link to="/firewall/rules" />}>Rules</Button>
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
                onClick={() => void send(pendingEnabled, true)}
              >
                {pendingEnabled ? 'Turn on anyway' : 'Turn off anyway'}
              </Button>
            </div>
          </AlertDescription>
        </Alert>
      )}

      {st.problem && (
        <Alert>
          <AlertTriangle />
          <AlertTitle>{st.native ? 'Native rules could not be verified' : 'Nothing is inside yet'}</AlertTitle>
          <AlertDescription>{st.problem}</AlertDescription>
        </Alert>
      )}

      <TrafficPermissions status={st} />
      <Button variant="outline" nativeButton={false} render={<Link to="/firewall/rules" />}>Rules</Button>
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
      detail: st.problem ?? 'The router could not read its own firewall rules.',
      dot: 'bg-warning',
    }
  }
  if (st.native) {
    return { headline: st.problem ? 'Native rules not verified' : st.drifted ? 'Native rules changed' : 'On — native nftables rules', detail: st.problem ?? (st.drifted ? 'The kernel table differs from the saved native rules.' : 'The saved native table is in force. Review its chains and policies on the Rules page.'), dot: st.problem || st.drifted ? 'bg-warning' : 'bg-success' }
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
