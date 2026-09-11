import { Link, Navigate, Route, Routes } from 'react-router'

import { AppShell } from '@/components/layout/app-shell'
import { Button } from '@/components/ui/button'
import { AuthGate } from '@/components/layout/auth-gate'
import { DhcpPage } from '@/routes/dhcp'
import { DnsPage } from '@/routes/dns'
import { FirewallPage } from '@/routes/firewall'
import { OverviewPage } from '@/routes/overview'
import { GatewayPage } from '@/routes/gateway'

export function App() {
  return (
    <AuthGate>
      <Routes>
        <Route element={<AppShell />}>
          <Route index element={<OverviewPage />} />
          <Route path="gateway" element={<GatewayPage />} />
          <Route path="dhcp" element={<DhcpPage />} />
          <Route path="dns" element={<DnsPage />} />
          <Route path="firewall" element={<FirewallPage />} />

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
