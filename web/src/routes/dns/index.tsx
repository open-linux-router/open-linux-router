import { AlertTriangle, ChevronRight } from 'lucide-react'
import { Link } from 'react-router'

import { SettingsList } from '@/components/layout/settings-list'
import { StatusDetail, StatusStrip } from '@/components/layout/status-strip'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { ActivityCard } from '@/features/dns/activity'
import { ApplyOutcome, useDnsEditor } from '@/features/dns/editor'
import { useDnsStatus } from '@/features/dns/queries'
import { RELAY_UNIT, RESOLVER_UNIT, UnitLabel, serviceOf } from '@/features/dns/units'
import { useInterfaces } from '@/features/link/queries'
import type { DnsStatus } from '@/lib/api-types'
import type { DnsConfig } from '@/lib/config-types'

/**
 * DNS, on the page you land on.
 *
 * Two questions and no others: is this working, and what did it see. The
 * settings that used to sit under them — six cards, each with its paragraph —
 * are a list of six rows at the bottom, each showing what it currently says, so
 * most visits are answered without opening any of them.
 *
 * The query log stays on this page rather than becoming a seventh row, because
 * it is not a setting: it is the entire reason olr owns :53 (docs/dns.md §4),
 * and a section whose landing page opened on a form would bury the most
 * valuable thing in the module one level down.
 */
export function DnsPage() {
  const { config, busy, change, applier, gate } = useDnsEditor()
  const status = useDnsStatus()
  const interfaces = useInterfaces()

  if (!config) return gate

  // The one prerequisite this section cannot supply for itself. Turning DNS on
  // now chooses its own listen address (internal/dns WithDerivedListen), but it
  // chooses from the interfaces the operator has handed over — and with none,
  // there is nothing to choose. Said here, before the switch is touched, rather
  // than as the refusal that used to arrive after it.
  const nothingAdopted =
    interfaces.isSuccess && !interfaces.data.interfaces.some((i) => i.adopted)

  return (
    <div className="space-y-6">
      <ApplyOutcome applier={applier} />

      {nothingAdopted && (
        <Alert>
          <AlertTriangle />
          <AlertTitle>This router has not been given an interface yet</AlertTitle>
          <AlertDescription className="space-y-2">
            <p>
              DNS cannot answer anywhere until one of this machine's interfaces
              is handed over. Switching one on changes nothing by itself.
            </p>
            <Link
              to="/dhcp/interfaces"
              className="inline-flex items-center gap-1 text-sm font-medium underline underline-offset-4"
            >
              Choose an interface
              <ChevronRight className="size-3.5" aria-hidden />
            </Link>
          </AlertDescription>
        </Alert>
      )}

      <StatusCard
        config={config}
        status={status.data}
        error={status.error as Error | null}
        busy={busy}
        onChange={change}
      />

      <ActivityCard config={config} busy={busy} onChange={change} />

      <SettingsList
        section="/dns"
        rows={[
          { slug: 'blocking', value: describeCount(config.policies?.length, 'rule') },
          { slug: 'names', value: describeCount(config.hosts?.length, 'name') },
          { slug: 'resolving', value: summariseUpstream(config) },
          { slug: 'listening', value: config.listen?.join(', ') || 'Nowhere yet' },
          { slug: 'enforcement', value: config.hijack.enabled ? 'On' : 'Off' },
          { slug: 'advanced', value: config.extra_unbound_conf ? 'Customised' : undefined },
        ]}
      />
    </div>
  )
}

function describeCount(n: number | undefined, noun: string): string {
  if (!n) return 'None'
  return `${n} ${noun}${n === 1 ? '' : 's'}`
}

/* -------------------------------------------------------------------------- */

function StatusCard({
  config,
  status,
  error,
  busy,
  onChange,
}: {
  config: DnsConfig
  status?: DnsStatus
  error: Error | null
  busy: boolean
  onChange: (next: DnsConfig) => void
}) {
  const summary = describeStatus(config, status, error)

  // Reported on its own line rather than folded into the headline: a unit that
  // is running now and not enabled costs nothing until the next reboot, and
  // then costs the whole network its name resolution at once.
  //
  // "running now" is load-bearing and used to be missing from the predicate,
  // so a unit that was merely *stopped* and not enabled produced the sentence
  // "is running, but is not set to start at boot" directly under a headline
  // saying nothing was serving DNS. A first install shows exactly that: no unit
  // is enabled or active until an apply succeeds, so the page's first
  // impression was two lines contradicting each other.
  //
  // A unit that is not installed is kept, stopped or not — it is the one case
  // where "not enabled" has a cause worth naming, and the branch below names
  // it. Everything else that is off is already the headline's business.
  const notAtBoot = (status?.services ?? []).filter(
    (s) => s.status && !s.status.enabled && (s.status.active || s.status.installed === false),
  )

  return (
    <StatusStrip
      headline={summary.headline}
      detail={summary.detail}
      dot={summary.dot}
      control={{
        id: 'dns-enabled',
        label: 'Answer DNS for this network',
        checked: config.enabled,
        busy,
        onChange: (enabled) => onChange({ ...config, enabled }),
      }}
      // Only while it is on: see the note in routes/overview.tsx. A module
      // that is off has nothing running to be behind.
      drifted={config.enabled && status?.drifted && !status.drift_error}
      details={
        <>
          {/* Both backends, separately. Averaging them would hide the thing
              worth knowing: the resolver behind dying and the thing owning
              :53 dying are different faults with different fixes, and only
              one of them is ours. */}
          {(status?.services ?? []).map((s) => (
            <StatusDetail key={s.unit} term={s.unit}>
              {s.status
                ? `${s.status.state}${s.status.sub_state ? ` (${s.status.sub_state})` : ''}`
                : (s.error ?? 'unknown')}
            </StatusDetail>
          ))}
          <StatusDetail term="Resolving by">{describeUpstream(config)}</StatusDetail>
          <StatusDetail term="Matches what is running">
            {status?.drift_error
              ? `unknown — ${status.drift_error}`
              : status?.drifted
                ? 'no'
                : 'yes'}
          </StatusDetail>
          <StatusDetail term="Read at">
            {status ? new Date(status.as_of).toLocaleTimeString() : '—'}
          </StatusDetail>
          <p className="pt-1">
            Read from the system on every request and never cached, so this is
            what is true right now rather than what was last written.
          </p>
        </>
      }
    >
      {/* Tinted rather than given an Alert variant of its own: the shared
          component only ships default and destructive, and this is not
          destructive — nothing is wrong yet, which is exactly the problem. */}
      {notAtBoot.length > 0 && (
        <Alert className="border-warning/40 bg-warning/10 text-warning-foreground">
          <AlertTriangle />
          <AlertTitle>DNS will not come back after a reboot</AlertTitle>
          <AlertDescription className="text-warning-foreground/90">
            {notAtBoot.map((s) => (
              <p key={s.unit}>
                {s.status?.installed === false
                  ? `${UnitLabel(s.unit)} is not installed on this box — reinstall the olr package.`
                  : `${UnitLabel(s.unit)} is running, but is not set to start at boot.`}
              </p>
            ))}
          </AlertDescription>
        </Alert>
      )}
    </StatusStrip>
  )
}

/**
 * The headline.
 *
 * Two daemons make this harder than dhcp's version, and the extra cases are the
 * point rather than an inconvenience. "Devices can reach us but we cannot look
 * anything up" and "devices cannot reach us at all" both present to a human as
 * the internet being broken, and they have completely different fixes.
 *
 * "We could not tell" stays a third answer throughout (design.md §5.4): a box
 * with no system bus reports unknown, and flattening that into "stopped" would
 * put a red dot on a working router.
 */
function describeStatus(
  config: DnsConfig,
  status: DnsStatus | undefined,
  error: Error | null,
): { headline: string; detail: string; dot: string } {
  if (error) {
    return { headline: 'Cannot reach the router', detail: error.message, dot: 'bg-destructive' }
  }
  if (!status) {
    return {
      headline: 'Checking…',
      detail: 'Reading the current state.',
      dot: 'bg-muted-foreground/40',
    }
  }
  if (!config.enabled) {
    return {
      headline: 'Off',
      detail: 'Devices cannot look up names through this router.',
      dot: 'bg-muted-foreground/40',
    }
  }

  const relay = serviceOf(status, RELAY_UNIT)
  const resolver = serviceOf(status, RESOLVER_UNIT)

  if (!relay?.status && !resolver?.status) {
    return {
      headline: 'Status unknown',
      detail: relay?.error ?? resolver?.error ?? 'The router could not read the service state.',
      dot: 'bg-warning',
    }
  }
  if (relay?.status && !relay.status.active) {
    return {
      headline: 'Not answering',
      detail: 'Turned on, but nothing is serving DNS. Devices cannot look up names.',
      dot: 'bg-destructive',
    }
  }
  if (resolver?.status && !resolver.status.active) {
    return {
      headline: 'Answering, but nothing is resolving',
      detail:
        'Devices reach this router, but it cannot look anything up behind the scenes. Lookups will fail.',
      dot: 'bg-destructive',
    }
  }
  return {
    headline: 'Answering queries',
    detail: describeUpstream(config),
    dot: 'bg-success',
  }
}

/**
 * The same fact as describeUpstream, in the width a list row has.
 *
 * Separate rather than shortening the sentence for both: the strip is prose
 * under a headline and reads as one ("Answering queries — forwarding to
 * 1.1.1.1"), while the row is a value beside a label and a full stop in it
 * would look like a mistake.
 */
function summariseUpstream(config: DnsConfig): string {
  const upstream = config.upstream
  if ((upstream.mode || 'recurse') !== 'forward') return 'From the root servers'
  const servers = upstream.servers?.length ? upstream.servers.join(', ') : 'Nowhere yet'
  return upstream.tls ? `${servers}, encrypted` : servers
}

function describeUpstream(config: DnsConfig): string {
  const upstream = config.upstream
  if ((upstream.mode || 'recurse') === 'forward') {
    const servers = upstream.servers?.length ? upstream.servers.join(', ') : 'nowhere yet'
    return upstream.tls
      ? `Forwarding to ${servers} over an encrypted connection.`
      : `Forwarding to ${servers}.`
  }
  return 'Looking names up from the root servers itself.'
}
