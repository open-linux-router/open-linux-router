import { Link, Route, Routes } from 'react-router'

import { AppShell } from '@/components/layout/app-shell'
import { Button } from '@/components/ui/button'
import { AuthGate } from '@/components/layout/auth-gate'
import { DevicesPage } from '@/routes/devices'
import { DhcpPage } from '@/routes/dhcp'
import { OverviewPage } from '@/routes/overview'

export function App() {
  return (
    <AuthGate>
      <Routes>
        <Route element={<AppShell />}>
          <Route index element={<OverviewPage />} />
          <Route path="devices" element={<DevicesPage />} />
          <Route path="dhcp" element={<DhcpPage />} />
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
