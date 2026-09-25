import { SettingsList } from '@/components/layout/settings-list'
import { useDialConfig } from '@/features/dial/queries'
import { useIngressConfig } from '@/features/ingress/queries'
import { useForwardsConfig } from '@/features/nat/queries'
import { useRemoteConfig, useRemotePeers } from '@/features/remote/queries'
import type { RemotePeer } from '@/lib/api-types'
import type { RemoteConfig } from '@/lib/config-types'

/**
 * Advanced, on the page you land on: a row per feature, each saying what it is
 * doing now.
 *
 * Nothing else, unlike every other landing page. The four have no status in
 * common to summarise — a port forward's rules, a published name, a tunnel and a
 * certificate fail in four unrelated ways — so each page keeps its own status
 * strip and this one only has to say which of them are in use. A value that is
 * still loading is left blank rather than guessed.
 */
export function AdvancedPage() {
  const gateway = useForwardsConfig()
  const dial = useDialConfig()
  const remote = useRemoteConfig()
  const peers = useRemotePeers()
  const ingress = useIngressConfig()

  const forwards = gateway.data?.forwards ?? []
  const records = dial.data?.records ?? []
  const devices = peers.data?.peers ?? []
  const services = ingress.data?.services ?? []

  return (
    <SettingsList
      section="/advanced"
      heading={null}
      rows={[
        {
          slug: 'forwards',
          value: gateway.data && (
            // Forwards ride on the gateway's switch; "Off" is that switch, and
            // is what an operator needs to know before counting forwards.
            !gateway.data.enabled ? 'Off' : forwards.length ? count(forwards.length, 'port') : 'None'
          ),
        },
        {
          slug: 'ddns',
          value: dial.data && (records.length ? records.map((r) => r.name).join(', ') : 'None'),
        },
        {
          slug: 'remote',
          value: remote.data && describeRemote(remote.data, devices),
        },
        {
          slug: 'ingress',
          value:
            ingress.data &&
            (!ingress.data.enabled
              ? 'Off'
              : services.length
                ? count(services.length, 'address', 'addresses')
                : 'None'),
        },
      ]}
    />
  )
}

/** Both ways in, since either can be on without the other. */
function describeRemote(config: RemoteConfig, devices: RemotePeer[]) {
  const parts: string[] = []
  if (config.wireguard.enabled) {
    const online = devices.filter((p) => p.online).length
    parts.push(`${count(devices.length, 'device')}, ${online} connected`)
  }
  if (config.shadowsocks.enabled) parts.push('proxy on')
  return parts.length ? parts.join(' · ') : 'Off'
}

function count(n: number, one: string, many = `${one}s`) {
  return `${n} ${n === 1 ? one : many}`
}
