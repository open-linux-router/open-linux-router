import { Link, Navigate, Route, Routes, useLocation } from 'react-router'

import { AppShell } from '@/components/layout/app-shell'
import { Button } from '@/components/ui/button'
import { AuthGate } from '@/components/layout/auth-gate'
import { DhcpPage } from '@/routes/dhcp/index'
import { DhcpAdvancedPage } from '@/routes/dhcp/advanced'
import { DhcpRangesPage } from '@/routes/dhcp/ranges'
import { DhcpReservationsPage } from '@/routes/dhcp/reservations'
import { DnsPage } from '@/routes/dns/index'
import { DnsAdvancedPage } from '@/routes/dns/advanced'
import { DnsBlockingPage } from '@/routes/dns/blocking'
import { DnsEnforcementPage } from '@/routes/dns/enforcement'
import { DnsListeningPage } from '@/routes/dns/listening'
import { DnsNamesPage } from '@/routes/dns/names'
import { DnsResolvingPage } from '@/routes/dns/resolving'
import { AccessPage } from '@/routes/access'
import { SocksOutPage } from '@/routes/socks-out'
import { AdvancedPage } from '@/routes/advanced/index'
import { DdnsPage } from '@/routes/advanced/ddns'
import { FilteringPage } from '@/routes/advanced/filtering'
import { ForwardsPage } from '@/routes/advanced/forwards'
import { IngressPage } from '@/routes/advanced/ingress'
import { IptvPage } from '@/routes/advanced/iptv'
import { RemotePage } from '@/routes/advanced/remote'
import { NetworksPage } from '@/routes/gateway/networks'
import { OverviewPage } from '@/routes/overview'
import { ToolsPage } from '@/routes/tools'
import { DevicePage } from '@/routes/device'
import { GroupPage } from '@/routes/group'
import { GatewayPage } from '@/routes/gateway/index'
import { GatewayInterfacePage } from '@/routes/gateway/interface'
import { GatewayExitsPage } from '@/routes/gateway/exits'
import { GatewayIPv6Page } from '@/routes/gateway/ipv6'
import { FirewallPage } from '@/routes/firewall'
import { GatewayUnmanagedPage } from '@/routes/gateway/unmanaged'
import { GatewayUsagePage } from '@/routes/gateway/usage'

export function App() {
  return (
    <AuthGate>
      <Routes>
        <Route element={<AppShell />}>
          <Route index element={<OverviewPage />} />
          <Route path="devices/:mac" element={<DevicePage />} />
          <Route path="groups/:name" element={<GroupPage />} />
          <Route path="gateway">
            <Route index element={<GatewayPage />} />
            <Route path="interfaces/:name" element={<GatewayInterfacePage />} />
            <Route path="networks" element={<NetworksPage />} />
            <Route path="exits" element={<GatewayExitsPage />} />
            <Route path="usage" element={<GatewayUsagePage />} />
            <Route path="ipv6" element={<GatewayIPv6Page />} />
            <Route path="unmanaged" element={<GatewayUnmanagedPage />} />
            <Route path="dhcp">
              <Route index element={<DhcpPage />} />
              <Route path="ranges" element={<DhcpRangesPage />} />
              <Route path="reservations" element={<DhcpReservationsPage />} />
              <Route path="advanced" element={<DhcpAdvancedPage />} />
            </Route>

            {/* DHCP and DNS keep their own status and settings pages inside Gateway.
                Their group labels and explanations live in sections.ts. */}
            <Route path="dns">
              <Route index element={<DnsPage />} />
              <Route path="blocking" element={<DnsBlockingPage />} />
              <Route path="names" element={<DnsNamesPage />} />
              <Route path="resolving" element={<DnsResolvingPage />} />
              <Route path="listening" element={<DnsListeningPage />} />
              <Route path="enforcement" element={<DnsEnforcementPage />} />
              <Route path="advanced" element={<DnsAdvancedPage />} />
            </Route>
          </Route>
          <Route path="firewall" element={<FirewallPage />} />
          <Route path="access" element={<AccessPage />} />
          <Route path="access/socks5" element={<SocksOutPage />} />
          <Route path="tools" element={<ToolsPage />} />
          <Route path="advanced">
            <Route index element={<AdvancedPage />} />
            <Route path="forwards" element={<ForwardsPage />} />
            <Route path="filtering" element={<FilteringPage />} />
            <Route path="ddns" element={<DdnsPage />} />
            <Route path="remote" element={<RemotePage />} />
            <Route path="ingress" element={<IngressPage />} />
            <Route path="iptv" element={<IptvPage />} />
          </Route>

          {/* The addresses that moved. Redirected rather than deleted: a
              bookmark or a link in someone's notes should land where the thing
              went, not on a 404 that makes it look removed. docs/install.md has
              named /dhcp/interfaces in print since 0.1.0, so that one will be
              followed by people reading an older copy for a while yet. */}
          <Route path="internet" element={<Navigate to="/gateway" replace />} />
          <Route path="networks" element={<Navigate to="/gateway/networks" replace />} />
          <Route path="gateway/firewall" element={<Navigate to="/firewall" replace />} />
          <Route path="firewall/unmanaged" element={<Navigate to="/advanced/filtering" replace />} />
          <Route path="gateway/forwards" element={<Navigate to="/advanced/forwards" replace />} />
          <Route path="gateway/filtering" element={<Navigate to="/advanced/filtering" replace />} />
          <Route path="remote" element={<Navigate to="/advanced/remote" replace />} />
          <Route path="ingress" element={<Navigate to="/advanced/ingress" replace />} />
          <Route path="devices" element={<Navigate to="/" replace />} />
          <Route path="dhcp/interfaces" element={<Navigate to="/gateway/networks" replace />} />
          <Route path="dhcp/*" element={<LegacySectionRedirect />} />
          <Route path="dns/*" element={<LegacySectionRedirect />} />

          <Route path="*" element={<NotFound />} />
        </Route>
      </Routes>
    </AuthGate>
  )
}

function NotFound() {
  return (
    <div className="space-y-3 py-16 text-center">
      <h1 className="text-lg font-medium">Page not found</h1>
      <p className="text-sm text-muted-foreground">
        That address does not match anything in this app.
      </p>
      {/* An error page with no way out is a dead end, which on a phone means
          reaching for the URL bar. */}
      <Button variant="outline" size="sm" render={<Link to="/">Go to Overview</Link>} />
    </div>
  )
}

function LegacySectionRedirect() {
  const { pathname, search, hash } = useLocation()
  return <Navigate to={`/gateway${pathname}${search}${hash}`} replace />
}
