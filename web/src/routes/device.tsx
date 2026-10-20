import { Heading } from '@/components/ui/heading'
import { MetricValue } from '@/components/ui/metric-value'
import { Link, useParams } from 'react-router'

import { Button } from '@/components/ui/button'
import { ClientQueries } from '@/features/dns/client-queries'
import { DeviceInspection } from '@/features/devices/inspection'
import { DeviceActivity } from '@/features/devices/activity'
import { DeviceInlineEditor } from '@/features/devices/device-inline-editor'
import { useDeviceList } from '@/features/devices/queries'
import { useGatewayTraffic } from '@/features/gateway/queries'
import { DeviceServices } from '@/features/ingress/device-services'
import { DevicePolicy } from '@/features/qos/device-policy'
import { useTrafficView } from '@/features/topology/traffic'
import { formatBytes } from '@/lib/utils'

export function DevicePage() {
  const { mac } = useParams()
  const devices = useDeviceList()
  const traffic = useGatewayTraffic()
  const flow = useTrafficView(traffic.data, traffic.isError)
  const device = devices.data?.devices.find((d) => d.mac.toLowerCase() === mac?.toLowerCase())
  const usage = device && flow.flowOf(device)

  if (devices.isPending) return <p className="py-16 text-sm text-muted-foreground">Loading device…</p>
  if (devices.isError) return <State title="Could not load devices" detail="Try again when the router is reachable." />
  if (!device) return <State title="Device not found" detail="It may have been forgotten or is no longer in the device list." />

  return (
    <div className="space-y-4 pb-12">
      <DeviceInlineEditor key={device.mac} device={device} services={
        <DeviceServices
          mac={device.mac}
          name={device.name}
          addresses={[device.fixed_ip ?? '', ...(device.ips ?? [])]}
          add={`/access/ingress?device=${encodeURIComponent(device.mac)}`}
        />
      } total={<span className="inline-flex flex-wrap justify-end gap-3 tabular-nums" title="Traffic through this router since counters were reset">
        <span className="inline-flex items-center gap-0.5" aria-label="Download total"><span aria-hidden className="text-base font-normal text-muted-foreground">↓</span><MetricValue value={flow.counting ? formatBytes(usage?.down ?? 0) : null} /></span>
        <span className="inline-flex items-center gap-0.5" aria-label="Upload total"><span aria-hidden className="text-base font-normal text-muted-foreground">↑</span><MetricValue value={flow.counting ? formatBytes(usage?.up ?? 0) : null} /></span>
      </span>} activity={<DeviceActivity addresses={device.ips ?? []} downRate={usage?.downRate} upRate={usage?.upRate} />} traffic={
        <DevicePolicy mac={device.mac} downRate={usage?.downRate} upRate={usage?.upRate}>
            {!flow.counting && (
              <p className="text-sm text-muted-foreground">{traffic.isError ? 'Traffic counters are unavailable right now.' : 'Traffic counting is off or unavailable.'}</p>
            )}
        </DevicePolicy>
      } />

      <ClientQueries addresses={device.ips ?? []} />

      <DeviceInspection key={device.mac} mac={device.mac} addresses={device.ips ?? []} />
    </div>
  )
}

function State({ title, detail }: { title: string; detail: string }) {
  return <div className="space-y-3 py-16 text-center">
    <Heading as="h1" size="lg">{title}</Heading>
    <p className="text-sm text-muted-foreground">{detail}</p>
    <Button variant="outline" nativeButton={false} render={<Link to="/">Back to overview</Link>} />
  </div>
}
