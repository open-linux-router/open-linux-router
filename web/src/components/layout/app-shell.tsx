import { t, useLocale } from '@/lib/i18n'
import { useLayoutEffect } from 'react'
import { Link, NavLink, Outlet, useLocation } from 'react-router'

import { Logo } from '@/components/layout/logo'
import { SECTIONS, isOverviewPanel, sectionOf } from '@/components/layout/sections'
import { ThemeToggle } from '@/components/layout/theme-toggle'
import { LanguageToggle } from '@/components/layout/language-toggle'
import { TokenButton } from '@/components/layout/token-button'
import { cn } from '@/lib/utils'

export function AppShell() {
  useLocale()
  const { pathname, hash } = useLocation()
  const section = sectionOf(pathname)
  const landing = section && pathname === section.to
  // A section keeps its place in the navigation when the address belongs to a
  // state of it rather than to the section's own path — /map and /quality are
  // still Overview.
  const owns = (to: string) => section?.to === to || section?.navTo === to

  useLayoutEffect(() => {
    // A fullscreen state of the overview keeps the page where it was, so
    // closing it lands on the card it was opened from.
    if (!hash && !isOverviewPanel(pathname)) window.scrollTo({ top: 0, left: 0, behavior: 'instant' })
  }, [pathname, hash])

  return (
    <div className="flex min-h-svh flex-col bg-background text-foreground">
      <a href="#main-content" className="fixed top-2 left-4 z-50 rounded-lg bg-primary-fill px-4 py-3 text-primary-foreground -translate-y-24 focus:translate-y-0">{t('Skip to content')}</a>
      <header className="sticky top-0 z-20 border-b bg-card/95 backdrop-blur-xl">
        <div className="mx-auto flex h-16 max-w-[80rem] items-center gap-2.5 px-4 sm:px-6 xl:px-8">
          <Link to="/" className="inline-flex min-h-11 shrink-0 items-center gap-2.5 rounded-md focus-visible:outline-2 focus-visible:outline-ring" aria-label={t('Open Linux Router overview')}>
            <Logo className="size-5 shrink-0" aria-hidden />
            <span className="font-semibold tracking-tight">Open Linux Router</span>
          </Link>
          <nav className="ml-6 hidden min-w-0 self-stretch lg:block" aria-label={t('Sections')}>
            <ul className="flex h-full gap-1">
              {SECTIONS.filter(s => !s.nested).map(({ to, label, end }) => (
                <li key={to} className="flex items-center">
                  <NavLink to={to} end={end} className={({ isActive }) => cn(
                    'flex h-10 items-center rounded-lg px-3 text-sm transition-colors duration-150 focus-visible:outline-2 focus-visible:-outline-offset-4 focus-visible:outline-ring',
                    (isActive || owns(to))
                      ? 'bg-primary/10 font-medium text-primary dark:bg-primary/15'
                      : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground',
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
          {landing && !section.nested && <h1 className="sr-only">{pathname === '/' ? t('Network overview') : section.label}</h1>}
          <Outlet />
        </div>
      </main>
      <footer>
        <div className="mx-auto flex max-w-[80rem] flex-wrap items-center justify-center gap-x-4 gap-y-2 px-4 pt-8 pb-[calc(6rem+env(safe-area-inset-bottom))] text-center text-xs text-muted-foreground lg:items-end lg:pb-3">
          <span>© {new Date().getFullYear()} Open Linux Router</span>
          <span>{__APP_VERSION__}</span>
          <a href="https://github.com/open-linux-router/open-linux-router" target="_blank" rel="noreferrer" className="inline-flex min-h-11 items-center transition-colors duration-150 hover:text-foreground lg:items-end">GitHub</a>
          <Link to="/credits" className="inline-flex min-h-11 items-center transition-colors duration-150 hover:text-foreground lg:items-end">{t('Credits')}</Link>
          <ThemeToggle />
          <LanguageToggle />
        </div>
      </footer>
      <nav aria-label={t('Sections')} className="fixed inset-x-0 bottom-0 z-20 border-t bg-card/95 pb-[env(safe-area-inset-bottom)] backdrop-blur-xl lg:hidden">
        <ul className="grid grid-cols-5">
          {SECTIONS.filter(s => !s.nested).map(({ to, label, icon: Icon, end }) => (
            <li key={to} className="min-w-0">
              <NavLink to={to} end={end} className={({ isActive }) => cn(
                'flex min-h-16 min-w-0 flex-col items-center justify-center gap-1 text-[0.625rem] font-medium transition-colors focus-visible:outline-2 focus-visible:-outline-offset-4 focus-visible:outline-ring',
                (isActive || owns(to)) ? 'text-primary' : 'text-muted-foreground hover:text-foreground',
              )}><Icon className="size-5" aria-hidden />{label}</NavLink>
            </li>
          ))}
        </ul>
      </nav>
    </div>
  )
}
