import { ChevronLeft } from 'lucide-react'
import { Link } from 'react-router'

import { groupOf } from '@/components/layout/sections'

/**
 * One setting, on a page of its own.
 *
 * The header is the whole component, and it is here rather than copied into
 * fifteen route files for the reason the section table gives: a heading written
 * fifteen times is fifteen ways for it to drift.
 *
 * This is also where the long explanation lives now. On the old single-page
 * sections every card carried its paragraph, so the page cost an operator all
 * of them at once; here the paragraph is on the page you opened *because* you
 * wanted to change this thing, which is the one moment it is worth reading.
 *
 * The back link is a real link to the section and not `history.back()`: arriving
 * from a bookmark, from the overview, or from a cross-link in another module all
 * have to lead somewhere sensible, and only one of those has a history to pop.
 */
export function SubPage({
  section,
  slug,
  children,
}: {
  section: string
  slug: string
  children: React.ReactNode
}) {
  const { section: parent, group } = groupOf(section, slug)

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <Link
          to={parent.to}
          className="-ml-1 inline-flex min-h-8 items-center gap-0.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
        >
          <ChevronLeft className="size-4" aria-hidden />
          {parent.label}
        </Link>
        <h1 className="text-2xl font-semibold tracking-tight">{group.label}</h1>
        <p className="max-w-prose text-sm text-muted-foreground">{group.blurb}</p>
      </header>

      {children}
    </div>
  )
}
