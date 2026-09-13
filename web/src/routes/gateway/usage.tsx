import { SubPage } from '@/components/layout/sub-page'
import { SwitchField } from '@/components/ui/editable-field'
import { ApplyOutcome, useGatewayEditor } from '@/features/gateway/editor'
import { gatewayChange, useGatewayTraffic } from '@/features/gateway/queries'
import { TrafficList } from '@/features/gateway/traffic-list'

export function GatewayUsagePage() {
  const { config, busy, change, applier, gate } = useGatewayEditor()
  const traffic = useGatewayTraffic()

  if (!config) return gate
  const counting = config.stats ?? true

  return (
    <SubPage section="/gateway" slug="usage">
      <ApplyOutcome applier={applier} />

      <SwitchField
        id="gateway-stats"
        label="Count how much each device uses"
        hint="Per-device counters on the forwarding path. Turning this off stops counting and discards what has been counted so far."
        checked={counting}
        busy={busy}
        onChange={(stats) => change(gatewayChange.settings({ stats }))}
      />

      <TrafficList traffic={traffic.data} enabled={counting} />
    </SubPage>
  )
}
