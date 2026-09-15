import { Link, Navigate, Route, Routes } from 'react-router'

import { AppShell } from '@/components/layout/app-shell'
import { Button } from '@/components/ui/button'
import { AuthGate } from '@/components/layout/auth-gate'
import { DhcpPage } from '@/routes/dhcp/index'
import { DhcpAdvancedPage } from '@/routes/dhcp/advanced'
import { DhcpInterfacesPage } from '@/routes/dhcp/interfaces'
import { DhcpRangesPage } from '@/routes/dhcp/ranges'
import { DhcpReservationsPage } from '@/routes/dhcp/reservations'
import { DnsPage } from '@/routes/dns/index'
import { DnsAdvancedPage } from '@/routes/dns/advanced'
import { DnsBlockingPage } from '@/routes/dns/blocking'
import { DnsEnforcementPage } from '@/routes/dns/enforcement'
import { DnsListeningPage } from '@/routes/dns/listening'
import { DnsNamesPage } from '@/routes/dns/names'
import { DnsResolvingPage } from '@/routes/dns/resolving'
import { FirewallPage } from '@/routes/firewall/index'
import { FirewallUnmanagedPage } from '@/routes/firewall/unmanaged'
import { IngressPage } from '@/routes/ingress'
import { NetworksPage } from '@/routes/networks'
import { OverviewPage } from '@/routes/overview'
import { GatewayPage } from '@/routes/gateway/index'
import { GatewayExitsPage } from '@/routes/gateway/exits'
import { GatewayUnmanagedPage } from '@/routes/gateway/unmanaged'
import { GatewayUsagePage } from '@/routes/gateway/usage'

export function App() {
  return (
    <AuthGate>
      <Routes>
        <Route element={<AppShell />}>
          <Route index element={<OverviewPage />} />
          <Route path="networks" element={<NetworksPage />} />
          <Route path="gateway">
            <Route index element={<GatewayPage />} />
            <Route path="exits" element={<GatewayExitsPage />} />
            <Route path="usage" element={<GatewayUsagePage />} />
            <Route path="unmanaged" element={<GatewayUnmanagedPage />} />
          </Route>
          <Route path="dhcp">
            <Route index element={<DhcpPage />} />
            <Route path="ranges" element={<DhcpRangesPage />} />
            <Route path="reservations" element={<DhcpReservationsPage />} />
            <Route path="interfaces" element={<DhcpInterfacesPage />} />
            <Route path="advanced" element={<DhcpAdvancedPage />} />
          </Route>

          {/* A section is a landing page and one page per settings group. The
              groups are listed in components/layout/sections.ts, which is also
              where their labels and explanations come from — a route added here
              without an entry there throws on render rather than drawing a page
              with an empty heading. */}
          <Route path="dns">
            <Route index element={<DnsPage />} />
            <Route path="blocking" element={<DnsBlockingPage />} />
            <Route path="names" element={<DnsNamesPage />} />
            <Route path="resolving" element={<DnsResolvingPage />} />
            <Route path="listening" element={<DnsListeningPage />} />
            <Route path="enforcement" element={<DnsEnforcementPage />} />
            <Route path="advanced" element={<DnsAdvancedPage />} />
          </Route>
          <Route path="firewall">
            <Route index element={<FirewallPage />} />
            <Route path="unmanaged" element={<FirewallUnmanagedPage />} />
          </Route>
          <Route path="ingress" element={<IngressPage />} />

          {/* The two addresses that moved. Redirected rather than deleted: a
              bookmark or a link in someone's notes should land where the thing
              went, not on a 404 that makes it look removed. */}
          <Route path="internet" element={<Navigate to="/gateway" replace />} />
          <Route path="devices" element={<Navigate to="/" replace />} />

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
