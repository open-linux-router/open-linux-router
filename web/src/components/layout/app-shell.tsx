import { NavLink, Outlet, useLocation } from 'react-router'

import { BRAND_ICON, SECTIONS, sectionOf } from '@/components/layout/sections'
import { ThemeToggle } from '@/components/layout/theme-toggle'
import { TokenButton } from '@/components/layout/token-button'
import { cn } from '@/lib/utils'

export function AppShell() {
  return (
    <div className="flex min-h-svh flex-col bg-background text-foreground">
      <header className="sticky top-0 z-20 border-b bg-background/75 backdrop-blur-xl">
        <div className="mx-auto flex h-14 max-w-6xl items-center gap-2.5 px-4">
          <BRAND_ICON className="size-5 shrink-0 text-muted-foreground" aria-hidden />
          <span className="font-semibold tracking-tight">Router</span>

          <DesktopNav />

          <div className="ml-auto flex items-center gap-1">
            <TokenButton />
            <ThemeToggle />
          </div>
        </div>
      </header>

      <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-6">
        <div className="space-y-6">
          <PageHeader />
          <Outlet />
        </div>
      </main>

      <SiteFooter />
      <MobileTabBar />
    </div>
  )
}

/**
 * Sections, in the bar, on the same line as everything else.
 *
 * These used to be a 176px column beside the content, which cost every viewport
 * that width to show four links. Laid along the bar they cost nothing: the
 * header had empty middle, and the content now gets the whole container.
 *
 * Labels only. The glyphs are still in the tab bar below, where a stacked icon
 * *is* the target; here they would only make four short words into four wide
 * ones, and the bar has to fit brand, sections and two buttons at 640px.
 */
function DesktopNav() {
  return (
    <nav className="ml-4 hidden self-stretch sm:block" aria-label="Sections">
      <ul className="flex h-full items-stretch gap-1">
        {SECTIONS.map(({ to, label, end }) => (
          <li key={to}>
            <NavLink
              to={to}
              end={end}
              className={({ isActive }) =>
                cn(
                  // The current section is underlined against the bar's own
                  // bottom edge rather than filled. A filled pill read as a
                  // button — and, once focused, as a *pressed* one, with the
                  // focus ring drawn round it — where a rule says "you are
                  // here" and nothing else.
                  'relative flex h-full items-center px-3 text-sm transition-colors',
                  'after:absolute after:inset-x-3 after:-bottom-px after:h-0.5 after:rounded-full',
                  'focus-visible:outline-2 focus-visible:-outline-offset-4 focus-visible:outline-ring',
                  isActive
                    ? 'font-medium text-foreground after:bg-foreground'
                    : 'text-muted-foreground hover:text-foreground',
                )
              }
            >
              {label}
            </NavLink>
          </li>
        ))}
      </ul>
    </nav>
  )
}

/**
 * The page's title, for a screen reader only.
 *
 * It used to be drawn — the section's name and a one-line blurb — at the top of
 * every landing page. Directly under a bar that already underlines that same
 * name, it said "Overview" twice and pushed the status strip, the thing a page
 * is opened for, down by two lines. A sighted operator knows where they are
 * from the bar; a screen reader still needs a level-one heading to land on.
 *
 * An address outside the table — the 404 — gets none. It is not a section, and
 * it carries its own heading.
 */
function PageHeader() {
  const { pathname } = useLocation()
  const section = sectionOf(pathname)
  // Only on a section's own landing page. A sub-page renders its own, visible
  // header — its name is not in the bar, and it needs the back link.
  if (!section || pathname !== section.to) return null

  return <h1 className="sr-only">{section.label}</h1>
}

/**
 * What is running, and where it came from.
 *
 * The version is stamped in at build time (see vite.config.ts) rather than read
 * from the API, because there is no endpoint for it and this SPA is embedded in
 * the daemon: they are one artifact and `make web` gives them the same string.
 */
function SiteFooter() {
  return (
    <footer className="border-t">
      {/* The tab bar is fixed over the bottom of the viewport, and the footer is
          what now reaches it. */}
      <div className="mx-auto flex max-w-6xl flex-col gap-1 px-4 pt-6 pb-24 text-xs text-muted-foreground sm:flex-row sm:items-center sm:pb-6">
        <span>Open Linux Router {__APP_VERSION__}</span>
        <a
          href="https://github.com/open-linux-router/open-linux-router"
          target="_blank"
          rel="noreferrer"
          className="underline-offset-4 hover:text-foreground hover:underline sm:ml-auto"
        >
          Source
        </a>
      </div>
    </footer>
  )
}

/**
 * The only way to change section on a phone.
 *
 * The bar's own nav is `sm:block`, which means that below 640px the app would
 * have no navigation at all — every section but the one you landed on
 * unreachable without typing a URL. (It was the sidebar that was `sm:block`
 * before; moving the links into the header changed nothing about this.) A
 * bottom bar is the platform-native answer at this width and keeps the targets
 * under the thumb.
 */
function MobileTabBar() {
  return (
    <nav
      aria-label="Sections"
      className="fixed inset-x-0 bottom-0 z-20 border-t bg-background/92 pb-[env(safe-area-inset-bottom)] backdrop-blur-xl sm:hidden"
    >
      <ul className="flex">
        {SECTIONS.map(({ to, label, icon: Icon, end }) => (
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
