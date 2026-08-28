import { Activity, Network, Router } from 'lucide-react'
import { NavLink, Outlet } from 'react-router'

import { ThemeToggle } from '@/components/layout/theme-toggle'
import { TokenButton } from '@/components/layout/token-button'
import { cn } from '@/lib/utils'

const NAV = [
  { to: '/', label: 'Overview', icon: Activity, end: true },
  { to: '/dhcp', label: 'DHCP', icon: Network, end: false },
]

export function AppShell() {
  return (
    <div className="min-h-svh bg-background text-foreground">
      <header className="sticky top-0 z-10 border-b bg-background/80 backdrop-blur">
        <div className="mx-auto flex h-14 max-w-6xl items-center gap-3 px-4">
          <Router className="size-5 shrink-0" aria-hidden />
          <span className="font-semibold tracking-tight">open-linux-router</span>
          <div className="ml-auto flex items-center gap-1">
            <TokenButton />
            <ThemeToggle />
          </div>
        </div>
      </header>

      <div className="mx-auto flex max-w-6xl gap-8 px-4 py-6">
        <nav className="hidden w-44 shrink-0 sm:block" aria-label="Sections">
          <ul className="sticky top-20 space-y-1">
            {NAV.map(({ to, label, icon: Icon, end }) => (
              <li key={to}>
                <NavLink
                  to={to}
                  end={end}
                  className={({ isActive }) =>
                    cn(
                      'flex items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors',
                      isActive
                        ? 'bg-accent font-medium text-accent-foreground'
                        : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground',
                    )
                  }
                >
                  <Icon className="size-4" aria-hidden />
                  {label}
                </NavLink>
              </li>
            ))}
          </ul>
        </nav>

        <main className="min-w-0 flex-1">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
