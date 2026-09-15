import { AlertTriangle, ChevronRight } from 'lucide-react'
import { Link } from 'react-router'

import { SettingsList } from '@/components/layout/settings-list'
import { BlockerAlerts } from '@/components/layout/blockers'
import { StatusDetail, StatusStrip } from '@/components/layout/status-strip'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { ApplyOutcome, useDhcpEditor } from '@/features/dhcp/editor'
import { useDhcpLeases, useDhcpStatus } from '@/features/dhcp/queries'
import { useInterfaces } from '@/features/link/queries'
import type { DhcpStatus } from '@/lib/api-types'
import type { DhcpConfig } from '@/lib/config-types'

/**
 * DHCP, on the page you land on.
 *
 * Is it handing out addresses, and how full are the ranges. The devices
 * themselves are deliberately not here — they are the body of the overview, and
 * showing the same list twice is what took the connected-devices card off this
 * page in the first place.
 */
export function DhcpPage() {
  const { config, busy, change, applier, gate } = useDhcpEditor()
  const status = useDhcpStatus()
  const leases = useDhcpLeases()
  const interfaces = useInterfaces()

  if (!config) return gate

  const connected = leases.data?.leases.filter((l) => l.active).length
  const pools = config.pools ?? []
  const nothingAdopted =
    interfaces.isSuccess && !interfaces.data.interfaces.some((i) => i.adopted)

  return (
    <div className="space-y-6">
      <ApplyOutcome applier={applier} />

      {/* The prerequisite, said before it is needed rather than as a refusal
          afterwards: olr will not serve anything on an interface nobody handed
          it (design.md §3.4), so on a fresh box every range added below is
          rejected until one switch on that page is on. */}
      {nothingAdopted && (
        <Alert>
          <AlertTriangle />
          <AlertTitle>This router has not been given an interface yet</AlertTitle>
          <AlertDescription className="space-y-2">
            <p>
              Address ranges are refused until one of this machine's interfaces
              is handed over. Switching one on changes nothing by itself — no
              address is set and no service is started.
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
        connected={connected}
        busy={busy}
        onChange={change}
      />

      <SettingsList
        section="/dhcp"
        rows={[
          {
            slug: 'ranges',
            value: pools.length
              ? pools.map((p) => p.interface).join(', ')
              : 'None — nothing is handed out',
          },
          {
            slug: 'reservations',
            value: config.reservations?.length
              ? `${config.reservations.length} device${config.reservations.length === 1 ? '' : 's'}`
              : 'None',
          },
          {
            slug: 'interfaces',
            value: interfaces.data
              ? describeAdopted(interfaces.data.interfaces.filter((i) => i.adopted).length)
              : undefined,
          },
          { slug: 'advanced', value: config.extra_dnsmasq_conf ? 'Customised' : undefined },
        ]}
      />
    </div>
  )
}

function describeAdopted(n: number): string {
  if (n === 0) return 'None yet'
  return `${n} interface${n === 1 ? '' : 's'}`
}

/* -------------------------------------------------------------------------- */

function StatusCard({
  config,
  status,
  error,
  connected,
  busy,
  onChange,
}: {
  config: DhcpConfig
  status?: DhcpStatus
  error: Error | null
  connected?: number
  busy: boolean
  onChange: (next: DhcpConfig) => void
}) {
  const summary = describeStatus(config, status, error)

  return (
    <StatusStrip
      headline={summary.headline}
      detail={
        config.enabled && connected !== undefined
          ? `${connected} device${connected === 1 ? '' : 's'} connected`
          : summary.detail
      }
      dot={summary.dot}
      control={{
        id: 'dhcp-enabled',
        label: 'Hand out addresses',
        checked: config.enabled,
        busy,
        onChange: (enabled) => onChange({ ...config, enabled }),
      }}
      // Only while it is on: see the note in routes/overview.tsx.
      drifted={config.enabled && status?.drifted && !status.drift_error}
      driftNote="The running server is behind these settings."
      details={
        <>
          <StatusDetail term="Service">
            {status?.service
              ? `${status.service.unit} — ${status.service.state}${
                  status.service.sub_state ? ` (${status.service.sub_state})` : ''
                }`
              : (status?.service_error ?? 'unknown')}
          </StatusDetail>
          <StatusDetail term="Matches the running server">
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
      {/* Usually the same dnsmasq the DNS page is complaining about: the
          distribution's unit takes UDP/67 and :53 together, so one
          `apt install dnsmasq` blocks both modules. Same component and the
          same words on both pages, so it reads as one problem rather than
          two. */}
      <BlockerAlerts module="dhcp" blockers={status?.blockers} />
    </StatusStrip>
  )
}

/**
 * "We could not tell" is a third answer and must not be flattened into
 * "stopped" (design.md §5.4) — hence the `Status unknown` branch, which is
 * reached when the daemon could not read the unit at all, as distinct from
 * reading it as inactive.
 */
function describeStatus(
  config: DhcpConfig,
  status: DhcpStatus | undefined,
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
      detail: 'Devices will not get an address from this router.',
      dot: 'bg-muted-foreground/40',
    }
  }
  if (!status.service) {
    return {
      headline: 'Status unknown',
      detail: status.service_error ?? 'The router could not read the service state.',
      dot: 'bg-warning',
    }
  }
  return status.service.active
    ? { headline: 'Handing out addresses', detail: 'Working normally.', dot: 'bg-success' }
    : {
        headline: 'Not running',
        detail: 'Turned on, but the service is stopped.',
        dot: 'bg-destructive',
      }
}
