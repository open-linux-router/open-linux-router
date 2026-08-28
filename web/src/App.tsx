import { Route, Routes } from 'react-router'

import { AppShell } from '@/components/layout/app-shell'
import { AuthGate } from '@/components/layout/auth-gate'
import { DhcpPage } from '@/routes/dhcp'
import { OverviewPage } from '@/routes/overview'

export function App() {
  return (
    <AuthGate>
      <Routes>
        <Route element={<AppShell />}>
          <Route index element={<OverviewPage />} />
          <Route path="dhcp" element={<DhcpPage />} />
          <Route path="*" element={<NotFound />} />
        </Route>
      </Routes>
    </AuthGate>
  )
}

function NotFound() {
  return (
    <div className="py-16 text-center">
      <h1 className="text-lg font-medium">Page not found</h1>
      <p className="text-sm text-muted-foreground">
        That address does not match anything in this app.
      </p>
    </div>
  )
}
