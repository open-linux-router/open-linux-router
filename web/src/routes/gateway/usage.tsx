import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { useDeviceList } from '@/features/devices/queries'
import { SubPage } from '@/components/layout/sub-page'
import { SwitchField } from '@/components/ui/editable-field'
import { ApplyOutcome, useGatewayEditor } from '@/features/gateway/editor'
import { gatewayChange, useGatewayTraffic } from '@/features/gateway/queries'
import { TrafficList } from '@/features/gateway/traffic-list'

export function GatewayUsagePage() {
  const { config, busy, change, applier, gate } = useGatewayEditor()
  const traffic = useGatewayTraffic()
  const devices = useDeviceList()

  if (!config) return gate
  const counting = config.stats ?? true

  return (
    <SubPage section="/gateway" slug="usage">
      <ApplyOutcome applier={applier} />

      <SwitchField
        id="gateway-stats"
        label="Count how much each device uses"
        hint="Counts forwarded traffic by device. Turning off clears the current counters."
        checked={counting}
        busy={busy}
        onChange={(stats) => change(gatewayChange.settings({ stats }))}
      />

      {counting && traffic.isError ? <Alert variant="destructive"><AlertTitle>Could not read traffic counters</AlertTitle><AlertDescription>{traffic.error.message}</AlertDescription></Alert> : <TrafficList traffic={traffic.data} enabled={counting} devices={devices.data?.devices} />}
    </SubPage>
  )
}
