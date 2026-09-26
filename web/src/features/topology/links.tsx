import { AnimatePresence, motion, type Transition } from 'motion/react'

import type { Link, Rail } from '@/features/topology/layout'
import { cn } from '@/lib/utils'

/**
 * Every line on the map, in one SVG under the nodes.
 *
 * A line is always the same shape — out of the bottom of its parent, straight
 * down, one smooth turn halfway, straight down into the top of its child — so
 * a steeper line only ever means a child further to the side. Written as the
 * same four-point cubic every time, which is also what lets the lines animate:
 * `d` is interpolated number by number, and two paths with the same commands
 * interpolate into a path the whole way.
 *
 * Neutral, like everything on the map. Thickness is the traffic; colour would
 * only be a second, weaker way of saying it.
 */
export function Links({ links, rail, transition }: { links: Link[]; rail?: Rail; transition: Transition }) {
  return (
    <svg aria-hidden className="pointer-events-none absolute inset-0 size-full overflow-visible">
      <AnimatePresence initial={false}>
        {links.map((l) => {
          const d = curve(l)
          return (
            <motion.path
              key={l.key}
              initial={{ d, opacity: 0, strokeWidth: l.width }}
              animate={{ d, opacity: 1, strokeWidth: l.width }}
              exit={{ opacity: 0, transition: { duration: 0.15 } }}
              transition={transition}
              fill="none"
              stroke="currentColor"
              strokeLinecap="round"
              strokeDasharray={l.dashed ? '2 5' : undefined}
              className={cn(l.faint ? 'text-foreground/12' : 'text-foreground/20')}
            />
          )
        })}
      </AnimatePresence>

      {rail && (
        <>
          <motion.path
            initial={false}
            animate={{ d: `M ${rail.x} ${rail.y0} L ${rail.x} ${rail.y1}` }}
            transition={transition}
            fill="none"
            stroke="currentColor"
            strokeWidth={1.5}
            className="text-border"
          />
          <AnimatePresence initial={false}>
            {rail.dots.map((dot) => (
              <motion.circle
                key={dot.key}
                initial={{ cy: dot.y, opacity: 0 }}
                animate={{ cy: dot.y, opacity: 1 }}
                exit={{ opacity: 0 }}
                transition={transition}
                cx={rail.x}
                r={3.5}
                strokeWidth={1.5}
                stroke="currentColor"
                className="fill-card text-muted-foreground/60"
              />
            ))}
          </AnimatePresence>
        </>
      )}
    </svg>
  )
}

function curve(l: Link) {
  const m = (l.y0 + l.y1) / 2
  return `M ${l.x0} ${l.y0} C ${l.x0} ${m} ${l.x1} ${m} ${l.x1} ${l.y1}`
}
