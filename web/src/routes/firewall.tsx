import { t, useLocale } from '@/lib/i18n'
import { AlertTriangle } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router'
import { toast } from 'sonner'

import { StatusStrip } from '@/components/layout/status-strip'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useFirewallStatus, useReapplyFirewall, useSetFirewall } from '@/features/firewall/queries'
import { firewallSummary } from '@/features/firewall/summary'
import { TrafficPermissions } from '@/features/firewall/traffic-permissions'
import { ApiError } from '@/lib/api'
import type { FirewallApplyResult } from '@/lib/api-types'

/** Firewall arrivals and status; native inspection and editing live on Rules. */
export function FirewallPage() {
  useLocale()
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
            <AlertTitle>{t('Cannot reach the router')}</AlertTitle>
            <AlertDescription>{status.error.message}</AlertDescription>
          </Alert>
        ) : (
          <Skeleton className="h-24 w-full" />
        )}
        <Button variant="outline" nativeButton={false} render={<Link to="/firewall/rules" />}>{t('Rules')}</Button>
      </div>
    )
  }
  const st = status.data
  const summary = firewallSummary(st)

  return (
    <div className="space-y-6">
      <StatusStrip
        headline={summary.headline}
        detail={summary.detail}
        tone={summary.tone}
        control={{
          id: 'firewall-enabled',
          get label() { return t('Firewall') },
          checked: st.enabled,
          busy: set.isPending,
          onChange: (on) => void send(on),
        }}
        drifted={st.enabled && st.drifted}
        driftNote="The kernel does not hold the firewall's rules — something removed them, or an earlier change stopped halfway."
        driftAction={{
          get label() { return t('Put them back') },
          busy: reapply.isPending,
          onClick: () => reapply.mutate(),
        }}
      />

      {lockout && (
        <Alert variant="destructive">
          <AlertTriangle />
          <AlertTitle>{t('This would cut you off')}</AlertTitle>
          <AlertDescription className="space-y-3">
            <p>{lockout}</p>
            <div className="flex gap-2">
              <Button size="sm" variant="ghost" onClick={() => setLockout(null)}>{t('Cancel')}</Button>
              <Button
                size="sm"
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
      <Button variant="outline" nativeButton={false} render={<Link to="/firewall/rules" />}>{t('Rules')}</Button>
    </div>
  )
}
