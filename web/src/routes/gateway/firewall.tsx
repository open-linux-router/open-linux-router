import { AlertTriangle } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { StatusStrip } from '@/components/layout/status-strip'
import { SubPage } from '@/components/layout/sub-page'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { List, ListRow } from '@/components/ui/list'
import { Skeleton } from '@/components/ui/skeleton'
import { useFirewallStatus, useReapplyFirewall, useSetFirewall } from '@/features/firewall/queries'
import { ApiError } from '@/lib/api'
import type { FirewallApplyResult, FirewallStatus } from '@/lib/api-types'

/**
 * The firewall: one switch, and the answer to "what can the internet reach
 * here?".
 *
 * There is no rule editor, and that is the design rather than a gap
 * (docs/firewall.md). Every port listed below comes from something already
 * configured elsewhere, so the list is read-only — the way to close a port is
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
      <SubPage section="/gateway" slug="firewall">
        {status.error ? (
          <Alert variant="destructive">
            <AlertTriangle />
            <AlertTitle>Cannot reach the router</AlertTitle>
            <AlertDescription>{status.error.message}</AlertDescription>
          </Alert>
        ) : (
          <Skeleton className="h-24 w-full" />
        )}
      </SubPage>
    )
  }
  const st = status.data
  const summary = summarize(st)

  return (
    <SubPage section="/gateway" slug="firewall">
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

      <section className="space-y-2">
        <h2 className="px-1 text-sm font-medium text-muted-foreground">
          Reachable from the internet
        </h2>
        <List>
          <ListRow
            title="Port forwards"
            subtitle="Each forward is its own permission, to the one device it names."
            to="/advanced/forwards"
          />
          {st.openings.map((o) => (
            <ListRow
              key={`${o.protocol}/${o.port ?? o.from}`}
              title={o.for}
              trailing={
                <span className="font-mono text-sm text-muted-foreground">
                  {o.protocol} {o.from ? `from ${o.from}` : o.port}
                </span>
              }
            />
          ))}
        </List>
        <p className="px-1 text-[0.8rem] text-muted-foreground">
          {st.enabled
            ? 'Everything else from outside is blocked. Replies to connections your devices make are not affected.'
            : 'With the firewall off, these and anything else this router or your devices answer on are reachable.'}
        </p>
      </section>

      <section className="space-y-2">
        <h2 className="px-1 text-sm font-medium text-muted-foreground">Inside</h2>
        <p className="px-1 text-sm">
          {st.inside.length ? (
            <span className="font-mono">{st.inside.join(', ')}</span>
          ) : (
            'No networks yet.'
          )}
        </p>
        <p className="px-1 text-[0.8rem] text-muted-foreground">
          Your networks and the remote access tunnel. Every other interface counts as outside —
          including ones added later.
        </p>
      </section>
    </SubPage>
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
