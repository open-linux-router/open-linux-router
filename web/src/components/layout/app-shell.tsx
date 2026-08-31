import { Activity, Laptop, Network, Router } from 'lucide-react'
import { NavLink, Outlet } from 'react-router'

import { ThemeToggle } from '@/components/layout/theme-toggle'
import { TokenButton } from '@/components/layout/token-button'
import { cn } from '@/lib/utils'

const NAV = [
  { to: '/', label: 'Overview', icon: Activity, end: true },
  // Devices before Addresses: §4.4 makes the device the object an operator
  // actually goes looking for, and a fixed address is a property of one.
  { to: '/devices', label: 'Devices', icon: Laptop, end: false },
  { to: '/dhcp', label: 'Addresses', icon: Network, end: false },
]

export function AppShell() {
  return (
    <div className="min-h-svh bg-background text-foreground">
      <header className="sticky top-0 z-20 border-b bg-background/75 backdrop-blur-xl">
        <div className="mx-auto flex h-14 max-w-6xl items-center gap-2.5 px-4">
          <Router className="size-5 shrink-0 text-muted-foreground" aria-hidden />
          <span className="font-semibold tracking-tight">Router</span>
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
                      'flex min-h-10 items-center gap-2.5 rounded-lg px-3 text-sm transition-colors',
                      isActive
                        ? 'bg-accent font-medium text-accent-foreground'
                        : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground',
                    )
                  }
                >
                  {({ isActive }) => (
                    <>
                      <Icon className={cn('size-4', isActive && 'text-primary')} aria-hidden />
                      {label}
                    </>
                  )}
                </NavLink>
              </li>
            ))}
          </ul>
        </nav>

        {/* The bottom bar is fixed, so the last card needs room to clear it. */}
        <main className="min-w-0 flex-1 pb-24 sm:pb-0">
          <Outlet />
        </main>
      </div>

      <MobileTabBar />
    </div>
  )
}

/**
 * The only way to change section on a phone.
 *
 * The sidebar above is `sm:block`, which meant that below 640px the app had no
 * navigation at all — every section but the one you landed on was unreachable
 * without typing a URL. A bottom bar is the platform-native answer at this
 * width and keeps the targets under the thumb.
 */
function MobileTabBar() {
  return (
    <nav
      aria-label="Sections"
      className="fixed inset-x-0 bottom-0 z-20 border-t bg-background/92 pb-[env(safe-area-inset-bottom)] backdrop-blur-xl sm:hidden"
    >
      <ul className="flex">
        {NAV.map(({ to, label, icon: Icon, end }) => (
          <li key={to} className="flex-1">
            <NavLink
              to={to}
              end={end}
              className={({ isActive }) =>
                cn(
                  // 56px clears the 44px minimum with the label stacked under
                  // the glyph.
                  'flex min-h-14 flex-col items-center justify-center gap-0.5 text-[0.7rem] font-medium transition-colors',
                  isActive ? 'text-primary' : 'text-muted-foreground',
                )
              }
            >
              <Icon className="size-5" aria-hidden />
              {label}
            </NavLink>
          </li>
        ))}
      </ul>
    </nav>
  )
}
