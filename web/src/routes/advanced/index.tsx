import { Launcher, LauncherSection, LauncherTile } from '@/components/layout/launcher'
import type { LauncherState } from '@/components/layout/launcher'
import { ADVANCED_GROUPS } from '@/features/advanced/catalog'
import type { AdvancedTile } from '@/features/advanced/catalog'
import { adminAccessSummary, ddnsSummary, forwardsSummary, iptvSummary, loading, unavailable } from '@/features/advanced/summary'
import { useDialConfig, useDialStatus } from '@/features/dial/queries'
import { useFirewallStatus } from '@/features/firewall/queries'
import { firewallState } from '@/features/firewall/summary'
import { useGatewayConfig } from '@/features/gateway/queries'
import { useIptvConfig, useIptvStatus } from '@/features/iptv/queries'
import { useForwardsStatus } from '@/features/nat/queries'
import { useAdminAccess } from '@/features/system/queries'

/**
 * Advanced, on the page you land on: one tile per feature, each saying what it
 * is doing now, grouped by what kind of thing it is.
 *
 * Everything here is low-level plumbing or the machine itself, which is why
 * these features are not on a section's own page. The tile is the whole visit
 * to a feature that only reports a state, and the line under its name is what
 * saves opening nine pages to find the one that is wrong.
 */
export function AdvancedPage() {
  const firewall = useFirewallStatus()
  const gateway = useGatewayConfig()
  const forwards = useForwardsStatus()
  const dial = useDialConfig()
  const dialStatus = useDialStatus()
  const iptv = useIptvConfig()
  const iptvStatus = useIptvStatus()
  const admin = useAdminAccess()

  const states: Record<NonNullable<AdvancedTile['state']>, LauncherState> = {
    // The firewall page owns the long explanation; the tile takes its short
    // form from the same conditions rather than deciding them again here.
    firewall: firewall.isError ? unavailable : firewall.data ? firewallState(firewall.data) : loading,
    forwards: forwardsSummary(gateway, forwards),
    ddns: ddnsSummary(dial, dialStatus),
    iptv: iptvSummary(iptv, iptvStatus),
    admin: adminAccessSummary(admin),
  }

  return (
    <Launcher>
      {ADVANCED_GROUPS.map(group => (
        <LauncherSection key={group.id} id={group.id} title={group.title}>
          {group.tiles.map(tile => (
            <LauncherTile
              key={tile.slug}
              name={tile.name}
              description={tile.description}
              mark={{ kind: 'letter', letter: tile.letter, hue: group.hue }}
              state={tile.state ? states[tile.state] : undefined}
              to={tile.to}
            />
          ))}
        </LauncherSection>
      ))}
    </Launcher>
  )
}
