import type { SVGProps } from 'react'

/**
 * olr's mark, drawn inline for the bar.
 *
 * The same geometry as public/favicon.svg, which is the source of truth — keep
 * the two in step. Inline rather than an <img> of the favicon so the bar costs
 * no extra request and the mark is painted with the first frame.
 *
 * It keeps its own two colours in both themes, the way the tab icon does. A
 * brand mark tinted to text-muted-foreground would stop being the mark.
 */
export function Logo(props: SVGProps<SVGSVGElement>) {
  return (
    <svg viewBox="0 0 100 100" {...props}>
      <path fill="#13a7f5" d="M50 8A42 42 0 0 0 50 92V69A19 19 0 0 1 50 31Z" />
      <path fill="#fd605e" d="M50 8A42 42 0 0 1 50 92V69A19 19 0 0 0 50 31Z" />
      <circle fill="#13a7f5" cx="50" cy="19.5" r="11.5" />
      <circle fill="#fd605e" cx="50" cy="80.5" r="11.5" />
    </svg>
  )
}
