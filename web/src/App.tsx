import { Link, Navigate, Route, Routes, useLocation } from 'react-router'

import { AppShell } from '@/components/layout/app-shell'
import { Button } from '@/components/ui/button'
import { AuthGate } from '@/components/layout/auth-gate'
import { DhcpAdvancedPage } from '@/routes/dhcp/advanced'
import { DhcpDetailsPage } from '@/routes/dhcp/details'
import { DhcpRangesPage } from '@/routes/dhcp/ranges'
import { DhcpReservationsPage } from '@/routes/dhcp/reservations'
import { DnsAdvancedPage } from '@/routes/dns/advanced'
import { DnsDetailsPage } from '@/routes/dns/details'
import { DnsBlockingPage } from '@/routes/dns/blocking'
import { DnsEnforcementPage } from '@/routes/dns/enforcement'
import { DnsListeningPage } from '@/routes/dns/listening'
import { DnsNamesPage } from '@/routes/dns/names'
import { DnsResolvingPage } from '@/routes/dns/resolving'
import { AccessPage } from '@/routes/access'
import { SocksOutPage } from '@/routes/access/socks-out'
import { AdminAccessPage } from '@/routes/admin-access'
import { AdvancedPage } from '@/routes/advanced/index'
import { DdnsPage } from '@/routes/advanced/ddns'
import { FilteringPage } from '@/routes/access/filtering'
import { ForwardsPage } from '@/routes/access/forwards'
import { IngressPage } from '@/routes/access/ingress'
import { IptvPage } from '@/routes/advanced/iptv'
import { RemotePage } from '@/routes/access/remote'
import { OverviewPage } from '@/routes/overview'
import { ToolsPage } from '@/routes/tools'
import { ToolPage } from '@/routes/tool-page'
import { DevicePage } from '@/routes/device'
import { GroupPage } from '@/routes/group'
import { GatewayPage } from '@/routes/gateway/index'
import { GatewayInterfacesPage } from '@/routes/gateway/interfaces'
import { GatewayInterfacePage } from '@/routes/gateway/interface'
import { GatewayExitsPage } from '@/routes/gateway/exits'
import { FirewallPage } from '@/routes/firewall'
import { GatewayUnmanagedPage } from '@/routes/gateway/unmanaged'
import { GatewayUsagePage } from '@/routes/gateway/usage'
import { CreditsPage } from '@/routes/credits'

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
            <Route path="interfaces" element={<GatewayInterfacesPage />} />
            <Route path="interfaces/:name" element={<GatewayInterfacePage />} />
            <Route path="networks" element={<Navigate to="/gateway/dhcp/details" replace />} />
            <Route path="exits" element={<GatewayExitsPage />} />
            <Route path="usage" element={<GatewayUsagePage />} />
            <Route path="ipv6" element={<Navigate to="/gateway/interfaces#forwarding" replace />} />
            <Route path="unmanaged" element={<GatewayUnmanagedPage />} />
            <Route path="dhcp">
              <Route path="details" element={<DhcpDetailsPage />} />
              <Route index element={<Navigate to="/gateway#dhcp" replace />} />
              <Route path="ranges" element={<DhcpRangesPage />} />
              <Route path="reservations" element={<DhcpReservationsPage />} />
              <Route path="advanced" element={<DhcpAdvancedPage />} />
            </Route>

            {/* Detailed settings remain nested; the status and activity are on Gateway. */}
            <Route path="dns">
              <Route path="details" element={<DnsDetailsPage />} />
              <Route index element={<Navigate to="/gateway#dns" replace />} />
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
          <Route path="settings/access" element={<AdminAccessPage />} />
          <Route path="access/socks5" element={<SocksOutPage />} />
          <Route path="access/remote" element={<RemotePage />} />
          <Route path="access/ingress" element={<IngressPage />} />
          <Route path="access/forwards" element={<ForwardsPage />} />
          <Route path="access/filtering" element={<FilteringPage />} />
          <Route path="tools" element={<ToolsPage />} />
          <Route path="tools/speedtest" element={<ToolPage tool="speedtest" />} />
          <Route path="tools/link-test" element={<ToolPage tool="link-test" />} />
          <Route path="tools/nat" element={<ToolPage tool="nat" />} />
          <Route path="tools/ping" element={<ToolPage tool="ping" />} />
          <Route path="tools/traceroute" element={<ToolPage tool="traceroute" />} />
          <Route path="credits" element={<CreditsPage />} />
          <Route path="advanced">
            <Route index element={<AdvancedPage />} />
            <Route path="forwards" element={<AccessRedirect slug="forwards" />} />
            <Route path="filtering" element={<AccessRedirect slug="filtering" />} />
            <Route path="ddns" element={<DdnsPage />} />
            <Route path="remote" element={<AccessRedirect slug="remote" />} />
            <Route path="ingress" element={<AccessRedirect slug="ingress" />} />
            <Route path="iptv" element={<IptvPage />} />
          </Route>

          {/* The addresses that moved. Redirected rather than deleted: a
              bookmark or a link in someone's notes should land where the thing
              went, not on a 404 that makes it look removed. docs/install.md has
              named /dhcp/interfaces in print since 0.1.0, so that one will be
              followed by people reading an older copy for a while yet. */}
          <Route path="internet" element={<Navigate to="/gateway" replace />} />
          <Route path="networks" element={<Navigate to="/gateway" replace />} />
          <Route path="networks/settings" element={<Navigate to="/gateway/interfaces" replace />} />
          <Route path="gateway/firewall" element={<Navigate to="/firewall" replace />} />
          <Route path="firewall/unmanaged" element={<AccessRedirect slug="filtering" />} />
          <Route path="gateway/forwards" element={<AccessRedirect slug="forwards" />} />
          <Route path="gateway/filtering" element={<AccessRedirect slug="filtering" />} />
          <Route path="remote" element={<AccessRedirect slug="remote" />} />
          <Route path="ingress" element={<AccessRedirect slug="ingress" />} />
          <Route path="devices" element={<Navigate to="/" replace />} />
          <Route path="dhcp/interfaces" element={<Navigate to="/gateway#interfaces" replace />} />
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

function AccessRedirect({ slug }: { slug: string }) {
  const { search, hash } = useLocation()
  return <Navigate to={`/access/${slug}${search}${hash}`} replace />
}
