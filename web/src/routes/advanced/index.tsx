import { SettingsList } from '@/components/layout/settings-list'
import { useDialConfig } from '@/features/dial/queries'
import { useIptvConfig } from '@/features/iptv/queries'

/**
 * Advanced, on the page you land on: a row per feature, each saying what it is
 * doing now.
 *
 * Remote connections and publishing live under Access. These remaining
 * features keep their own status; there is no shared health to summarise.
 */
export function AdvancedPage() {
  const dial = useDialConfig()
  const iptv = useIptvConfig()

  const records = dial.data?.records ?? []

  return (
    <SettingsList
      section="/advanced"
      heading={null}
      rows={[
        {
          slug: 'ddns',
          value: dial.data && (records.length ? records.map((r) => r.name).join(', ') : 'None'),
        },
        {
          slug: 'iptv',
          value: iptv.data && (iptv.data.enabled ? `On · ${iptv.data.networks?.join(', ') || 'No LAN'}` : 'Off'),
        },
      ]}
    />
  )
}
