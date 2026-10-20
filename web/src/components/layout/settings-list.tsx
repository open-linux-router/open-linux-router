import { List, ListRow } from '@/components/ui/list'
import { Heading } from '@/components/ui/heading'
import { groupOf } from '@/components/layout/sections'

/**
 * The way into everything a section can be told to do.
 *
 * One row per settings group: what it is on the left, what it currently says on
 * the right, and a page behind it. The value is the load-bearing part — a list
 * of six labels would make an operator open all six to find the one that is
 * wrong, which is the long page again with extra clicks. "Blocking · no rules"
 * and "Where it answers · 192.168.1.91:53" answer most visits without leaving
 * the landing page at all.
 *
 * Labels and explanations come from the section table; the values come from the
 * page, because only the page has the config. A row is omitted by leaving it
 * out of `rows` — the gateway's foreign-routing page has nothing on it until
 * somebody else's rules exist, and a row leading to an empty page is a small
 * broken promise.
 */
export function SettingsList({
  section,
  heading = 'Settings',
  rows,
}: {
  /** The section's path, e.g. `/dns`. */
  section: string
  /**
   * Null for a list whose rows are not settings but the whole of it — the
   * gateway's external routing rules, where a "Settings" heading would label
   * somebody else's rules as ours.
   */
  heading?: string | null
  rows: {
    slug: string
    /** What this setting currently says, in the operator's words. */
    value?: React.ReactNode
  }[]
}) {
  return (
    <section className="space-y-2">
      {heading && <Heading size="sm" className="px-1">{heading}</Heading>}
      <List>
        {rows.map(({ slug, value }) => {
          const { group } = groupOf(section, slug)
          return (
            <ListRow
              key={slug}
              title={group.label}
              wrap
              trailing={value}
              trailingTone={value != null && value !== false ? 'value' : 'muted'}
              to={group.to ?? `${section}/${slug}`}
            />
          )
        })}
      </List>
    </section>
  )
}
