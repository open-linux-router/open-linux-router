import { Activity, Globe, Network, Router, ShieldCheck, Waypoints } from 'lucide-react'
import { NavLink, Outlet, useLocation } from 'react-router'

import { ThemeToggle } from '@/components/layout/theme-toggle'
import { TokenButton } from '@/components/layout/token-button'
import { cn } from '@/lib/utils'

// Five sections: one place to look, four places to change something.
//
// This replaced Overview / Devices / Addresses / DNS / Internet, and the naming
// changed with it. The old rule was to use the word the audience already knows,
// which is why dhcp's section was called "Addresses" — nobody outside
// networking says DHCP. That rule was right while this list *was* the front
// door. It is not any more: the overview answers the everyday questions in
// plain language, so the rest are free to name mechanisms, and the person
// who goes looking for a section called DHCP is exactly the person who wants
// DHCP. Mechanism names in a row also read as one system, where
// "Addresses / DNS / Internet" read as three different registers.
//
// Firewall is the exception that proves the rule, and it is named for what it
// will be rather than what it is: today it holds port forwarding and no
// filtering at all (docs/firewall.md). The blurb carries the whole feature in
// one sentence, which is what stops somebody opening it expecting rules.
//
// Devices is absent because it is not a section: the device list is the body of
// the overview. Filing it under DHCP was considered and rejected — the
// statically-addressed printer has never held a lease, and would have lived on
// a page named for the protocol that has never seen it.
//
// This table is now the only description of a section anywhere. It renders in
// three places — the top bar, the tab bar, and the page's own title — because
// `blurb` moved here out of the four route files, where the same heading markup
// had been written four times and could drift four ways.
const NAV = [
  {
    to: '/',
    label: 'Overview',
    icon: Activity,
    end: true,
    blurb: 'Your network, what it is doing, and anything that needs you.',
  },
  {
    to: '/gateway',
    label: 'Gateway',
    icon: Waypoints,
    end: false,
    blurb:
      'Choose how each network reaches the internet. Everything follows one setting unless you change it for a network of its own.',
  },
  {
    to: '/dhcp',
    label: 'DHCP',
    icon: Network,
    end: false,
    blurb: 'Devices that join your network get an address from this router.',
  },
  {
    to: '/dns',
    label: 'DNS',
    icon: Globe,
    end: false,
    blurb:
      'Every device on your network looks up names through this router. This is what they asked for, and what they were allowed to reach.',
  },
  {
    to: '/firewall',
    label: 'Firewall',
    icon: ShieldCheck,
    end: false,
    blurb:
      'Let something on the internet reach one device on your network. Nothing gets in unless you put it here.',
  },
]

export function AppShell() {
  return (
    <div className="flex min-h-svh flex-col bg-background text-foreground">
      <header className="sticky top-0 z-20 border-b bg-background/75 backdrop-blur-xl">
        <div className="mx-auto flex h-14 max-w-6xl items-center gap-2.5 px-4">
          <Router className="size-5 shrink-0 text-muted-foreground" aria-hidden />
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
    <nav className="ml-4 hidden sm:block" aria-label="Sections">
      <ul className="flex items-center gap-1">
        {NAV.map(({ to, label, end }) => (
          <li key={to}>
            <NavLink
              to={to}
              end={end}
              className={({ isActive }) =>
                cn(
                  'flex min-h-9 items-center rounded-lg px-3 text-sm transition-colors',
                  isActive
                    ? 'bg-accent font-medium text-accent-foreground'
                    : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground',
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
 * The page's title, written once for all of them.
 *
 * Every route opened with its own copy of this markup, which meant four places
 * for a heading level or a text size to go its own way. It is the shell's now:
 * a page renders its content and nothing else.
 *
 * An address outside the table — the 404 — gets no title band. It is not a
 * section, and a heading saying so would only repeat the message the page is
 * already showing.
 */
function PageHeader() {
  const { pathname } = useLocation()
  const section = NAV.find(({ to, end }) => (end ? pathname === to : pathname.startsWith(to)))
  if (!section) return null

  return (
    <header>
      <h1 className="text-2xl font-semibold tracking-tight">{section.label}</h1>
      <p className="text-sm text-muted-foreground">{section.blurb}</p>
    </header>
  )
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
