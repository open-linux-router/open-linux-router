import { Link, NavLink, Outlet, useLocation } from 'react-router'

import { Logo } from '@/components/layout/logo'
import { SECTIONS, sectionOf } from '@/components/layout/sections'
import { ThemeToggle } from '@/components/layout/theme-toggle'
import { TokenButton } from '@/components/layout/token-button'
import { cn } from '@/lib/utils'

export function AppShell() {
  const { pathname } = useLocation()
  const section = sectionOf(pathname)
  const landing = section && pathname === section.to

  return (
    <div className="flex min-h-svh flex-col bg-background text-foreground">
      <a href="#main-content" className="fixed top-2 left-4 z-50 rounded-lg bg-primary-fill px-4 py-3 text-primary-foreground -translate-y-24 focus:translate-y-0">Skip to content</a>
      <header className="sticky top-0 z-20 border-b bg-card/95 backdrop-blur-xl">
        <div className="mx-auto flex h-16 max-w-[80rem] items-center gap-2.5 px-4 sm:px-6 xl:px-8">
          <Link to="/" className="inline-flex min-h-11 shrink-0 items-center gap-2.5 rounded-md focus-visible:outline-2 focus-visible:outline-ring" aria-label="Open Linux Router overview">
            <Logo className="size-5 shrink-0" aria-hidden />
            <span className="font-semibold tracking-tight">Open Linux Router</span>
          </Link>
          <nav className="ml-6 hidden min-w-0 self-stretch lg:block" aria-label="Sections">
            <ul className="flex h-full gap-1">
              {SECTIONS.filter(s => !s.nested).map(({ to, label, end }) => (
                <li key={to}>
                  <NavLink to={to} end={end} className={({ isActive }) => cn(
                    'relative flex h-full items-center px-3 text-sm transition-colors duration-150 after:absolute after:inset-x-3 after:bottom-0 after:h-0.5 after:rounded-full focus-visible:outline-2 focus-visible:-outline-offset-4 focus-visible:outline-ring',
                    isActive ? 'font-medium text-primary after:bg-primary' : 'text-muted-foreground hover:text-foreground',
                  )}>{label}</NavLink>
                </li>
              ))}
            </ul>
          </nav>
          <div className="ml-auto flex items-center gap-1"><TokenButton /></div>
        </div>
      </header>
      <main id="main-content" tabIndex={-1} className="mx-auto w-full max-w-[80rem] flex-1 px-4 py-6 outline-none sm:px-6 sm:py-8 xl:px-8">
        <div className="min-w-0">
          {landing && <h1 className="sr-only">{pathname === '/' ? 'Network overview' : section.label}</h1>}
          <Outlet />
        </div>
      </main>
      <footer>
        <div className="mx-auto flex max-w-[80rem] flex-wrap items-center justify-center gap-x-4 gap-y-2 px-4 pt-8 pb-[calc(6rem+env(safe-area-inset-bottom))] text-center text-xs text-muted-foreground lg:pb-2">
          <span>© {new Date().getFullYear()} Open Linux Router</span>
          <span>{__APP_VERSION__}</span>
          <a href="https://github.com/open-linux-router/open-linux-router" target="_blank" rel="noreferrer" className="inline-flex min-h-11 items-center hover:text-foreground hover:underline underline-offset-4">GitHub</a>
          <Link to="/credits" className="inline-flex min-h-11 items-center hover:text-foreground hover:underline underline-offset-4">Credits</Link>
          <ThemeToggle />
        </div>
      </footer>
      <nav aria-label="Sections" className="fixed inset-x-0 bottom-0 z-20 border-t bg-card/95 pb-[env(safe-area-inset-bottom)] backdrop-blur-xl lg:hidden">
        <ul className="grid grid-cols-6">
          {SECTIONS.filter(s => !s.nested).map(({ to, label, icon: Icon, end }) => (
            <li key={to} className="min-w-0">
              <NavLink to={to} end={end} className={({ isActive }) => cn(
                'flex min-h-16 min-w-0 flex-col items-center justify-center gap-1 text-[0.625rem] font-medium transition-colors focus-visible:outline-2 focus-visible:-outline-offset-4 focus-visible:outline-ring',
                isActive ? 'text-primary' : 'text-muted-foreground hover:text-foreground',
              )}><Icon className="size-5" aria-hidden />{label}</NavLink>
            </li>
          ))}
        </ul>
      </nav>
    </div>
  )
}
