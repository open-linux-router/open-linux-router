import { t, useLocale } from '@/lib/i18n'
import { Heading } from '@/components/ui/heading'
import { useMemo } from 'react'
import { Link } from 'react-router'
import type { FirewallStatus } from '@/lib/api-types'
import { trafficFlows } from './traffic-model'
import { VerticalSankey } from './vertical-sankey'

export function TrafficPermissions({ status }: { status: FirewallStatus }) {
  useLocale()
  const verified = status.known && !status.drifted && !status.problem
  const flows = useMemo(() => trafficFlows(status.traffic ?? []), [status.traffic])

  return (
    <section className="panel-surface p-4 sm:p-6" aria-label={t('Traffic permissions')}>
      <Heading>{t('Traffic permissions')}</Heading>
      {!status.enabled ? (
        <p className="mt-4 text-sm text-muted-foreground">{t('The firewall is off. OLR is not enforcing traffic permissions.')}</p>
      ) : status.native ? (
        <p className="mt-4 text-sm text-muted-foreground">{t('Native rules define the traffic permissions.')} <Link className="text-primary underline" to="/firewall/rules">{t('Inspect rules')}</Link></p>
      ) : !verified ? (
        <p className="mt-4 text-sm text-muted-foreground">{t('Traffic permissions could not be verified.')}<Link className="text-primary underline" to="/firewall/rules">{t('Inspect current rules')}</Link></p>
      ) : !status.traffic_known ? (
        <p className="mt-4 text-sm text-muted-foreground">{t('Traffic counters are unavailable.')}</p>
      ) : flows.length === 0 ? (
        <p className="mt-4 text-sm text-muted-foreground">{t('No traffic recorded since the rules were last written.')}</p>
      ) : (
        <VerticalSankey flows={flows} />
      )}
    </section>
  )
}
