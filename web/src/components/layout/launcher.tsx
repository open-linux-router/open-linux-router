import { Link } from 'react-router'

import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

/**
 * The app launcher: a grid of one-tile-per-destination directories (Access,
 * Advanced), not a list of settings.
 *
 * A directory answers "where do I go" before it answers "what does it say", so
 * the tile is an icon, a short name, and — where a feature has a state worth
 * knowing from the landing page — one compact line under the name. The state is
 * visible rather than hover-only: a tooltip is not available to a finger, and
 * the value is the reason the operator opened this page at all.
 *
 * Every tile is one link with one destination. A destination OLR does not
 * configure may open the project's own site instead; there is no tile whose
 * click opens a dialog.
 */
export function Launcher({ children }: { children: React.ReactNode }) {
  return <div className="space-y-8">{children}</div>
}

/** One headed row of tiles. The id names the heading the tiles are labelled by. */
export function LauncherSection({ id, title, children }: { id: string; title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-4" aria-labelledby={`launcher-${id}`}>
      <h2 id={`launcher-${id}`} className="text-2xl leading-8 font-semibold">{title}</h2>
      <div className="grid grid-cols-3 gap-x-3 gap-y-6 sm:grid-cols-5 lg:grid-cols-7 xl:grid-cols-8">{children}</div>
    </section>
  )
}

/** A feature's current answer, in the same shape the section summaries use. */
export type LauncherState = { text: string; attention?: boolean }

/**
 * What sits on the 64px backing.
 *
 * Bundled project artwork stays on white so brand colours survive both themes.
 * A protocol or an OLR feature has no mark to borrow, so it wears its initial
 * on a tinted tile — the hue is what keeps two tiles sharing an initial apart.
 * The icon variant is the fallback for a directory entry with neither.
 */
export type LauncherMark =
  | { kind: 'logo'; src: string }
  | { kind: 'letter'; letter: string; hue: number }
  | { kind: 'icon'; children: React.ReactNode }

const MARK_BACKING = {
  logo: 'bg-white group-hover:bg-accent group-active:bg-accent',
  letter: 'text-white group-hover:brightness-110 group-active:brightness-95',
  icon: 'bg-accent/60 text-primary group-hover:bg-accent group-active:bg-accent',
}

export function LauncherTile({
  name,
  description,
  mark,
  state,
  notes,
  to,
  href,
}: {
  name: string
  /** One sentence for the tooltip; never the only place a function is stated. */
  description: string
  mark: LauncherMark
  /** Shown under the name, then repeated in the tooltip and the accessible name. */
  state?: LauncherState
  /** Tooltip-only state lines, for a directory that keeps its values on hover. */
  notes?: LauncherState[]
  to?: string
  href?: string
}) {
  const lines = [...(state ? [state] : []), ...(notes ?? [])]
  const summary = lines[0]
  const label = `${name} · ${summary?.text ?? 'Not integrated'}`
  return (
    <Tooltip>
      <TooltipTrigger
        render={to ? <Link to={to} /> : href ? <a href={href} target="_blank" rel="noopener noreferrer" /> : <span />}
        aria-label={label}
        className="group flex min-w-0 flex-col items-center gap-2 rounded-xl px-1 py-2 text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-4 focus-visible:ring-offset-background"
      >
        <span
          className={cn('flex size-16 shrink-0 items-center justify-center rounded-2xl p-3 transition duration-150', MARK_BACKING[mark.kind])}
          style={mark.kind === 'letter' ? { background: `oklch(0.52 0.12 ${mark.hue})` } : undefined}
        >
          {mark.kind === 'logo'
            ? <img src={mark.src} alt="" className="max-h-full max-w-full object-contain" />
            : mark.kind === 'letter'
              ? <span aria-hidden className="text-[28px] leading-none font-semibold tracking-tight">{mark.letter}</span>
              : mark.children}
        </span>
        {/* The label owns the tile's width, so a long state truncates inside
            the grid cell instead of spilling into the neighbouring tile. */}
        <span className="flex w-full min-w-0 flex-col items-center gap-0.5">
          <span className="max-w-full break-words text-center text-[13px] font-medium sm:text-sm">{name}</span>
          {state && (
            <span className={cn('max-w-full truncate text-xs leading-4', state.attention ? 'text-warning-foreground' : 'text-muted-foreground')}>
              {state.text}
            </span>
          )}
        </span>
      </TooltipTrigger>
      <TooltipContent className="block space-y-2 text-sm leading-relaxed">
        <p>{description}</p>
        {lines.length ? lines.map((line, index) => <p key={index}>{line.text}</p>) : <p>Not integrated · External setup</p>}
      </TooltipContent>
    </Tooltip>
  )
}
