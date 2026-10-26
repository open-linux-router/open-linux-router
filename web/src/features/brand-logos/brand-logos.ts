// The brand marks a device or service can wear, served by the router itself.
//
// They used to be hot-linked from raw.githubusercontent.com while a page was
// drawn, which is the request that sat pending in the network panel whenever the
// browser had no route to GitHub — on a box whose whole job is networking.
// olrd now carries the library (see internal/brandlogo), so a mark is one more
// URL on the router and works with no internet at all.
//
// Two calls, both on demand: `brandLogoSrc` for one mark, and a search that
// asks the daemon rather than downloading six thousand slugs to filter in the
// browser. This file is the only place that knows the shape of either.

import { useEffect, useState } from 'react'

/** Where the daemon serves the bundled catalogue. See internal/brandlogo. */
const PREFIX = '/brand-logos'

/**
 * The slug inside a stored `thesvg:` value, or '' for any other icon.
 *
 * The prefix keeps naming where the artwork comes from — a mark by a project
 * that publishes its own logo — not where it is fetched from.
 */
export function brandLogoSlug(icon?: string): string {
  return icon?.startsWith('thesvg:') ? icon.slice('thesvg:'.length) : ''
}

/** One bundled mark. */
export function brandLogoSrc(slug: string): string {
  return `${PREFIX}/${encodeURIComponent(slug)}`
}

/** The URL that draws a stored `thesvg:` value, or undefined for anything else. */
export function brandLogoHref(icon?: string): string | undefined {
  const slug = brandLogoSlug(icon)
  return slug ? brandLogoSrc(slug) : undefined
}

/**
 * Answers already seen, so re-typing a prefix in a picker costs no request.
 * Small and bounded: a session searches a handful of brands, not a library.
 */
const answers = new Map<string, string[]>()

/** Search the bundled catalogue. An empty query asks nothing. */
export async function searchBrandLogos(query: string, signal?: AbortSignal): Promise<string[]> {
  const q = query.trim().toLowerCase()
  if (!q) return []
  const cached = answers.get(q)
  if (cached) return cached
  const response = await fetch(`${PREFIX}?q=${encodeURIComponent(q)}`, { signal })
  if (!response.ok) return []
  const body: unknown = await response.json()
  const slugs = (body as { slugs?: unknown }).slugs
  if (!Array.isArray(slugs)) return []
  const result = slugs.filter((slug): slug is string => typeof slug === 'string')
  if (answers.size > 64) answers.clear()
  answers.set(q, result)
  return result
}

/**
 * The catalogue's answers as a picker types.
 *
 * Debounced, because every keystroke is otherwise a request to a router's own
 * HTTP server, and cancelled outright once the query moves on — a slow answer
 * for "gr" must not land under "grafana".
 *
 * Nothing is set synchronously in the effect. Whether there is an answer, and
 * whether one is still coming, are both derived from the query on each render;
 * the single piece of state exists only to make the arrival of an answer
 * re-render the picker.
 */
export function useBrandLogoSearch(query: string): { slugs: string[]; loading: boolean } {
  const [answer, setAnswer] = useState<{ query: string; slugs: string[] }>()
  const q = query.trim().toLowerCase()

  useEffect(() => {
    if (!q || answers.has(q)) return
    const controller = new AbortController()
    const timer = setTimeout(() => {
      searchBrandLogos(q, controller.signal)
        .then((found) => {
          if (!controller.signal.aborted) setAnswer({ query: q, slugs: found })
        })
        .catch(() => {
          // A failed search is an answer with nothing in it, so the picker can
          // say so instead of waiting forever. The next keystroke asks again.
          if (!controller.signal.aborted) setAnswer({ query: q, slugs: [] })
        })
    }, 120)
    return () => {
      clearTimeout(timer)
      controller.abort()
    }
  }, [q])

  if (!q) return { slugs: [], loading: false }
  const known = answers.get(q)
  if (known) return { slugs: known, loading: false }
  if (answer?.query === q) return { slugs: answer.slugs, loading: false }
  return { slugs: [], loading: true }
}
