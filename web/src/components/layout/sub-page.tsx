import { PageHeading } from '@/components/layout/page-heading'
import { groupOf } from '@/components/layout/sections'

export function SubPage({ section, slug, children, action }: {
  section: string
  slug: string
  children: React.ReactNode
  action?: React.ReactNode
}) {
  const { section: parent, group } = groupOf(section, slug)
  const nested = parent.nested
  const parentTo = nested ? `${parent.to}/details` : parent.to
  const breadcrumbs = nested
    ? [{ to: '/gateway', label: 'Gateway' }, ...(slug === 'details' ? [] : [{ to: parentTo, label: parent.label }])]
    : undefined

  return (
    <div className="space-y-8">
      <PageHeading title={group.label} description={group.blurb}
        back={nested ? undefined : { to: parentTo, label: parent.label }}
        breadcrumbs={breadcrumbs} action={action} />
      {children}
    </div>
  )
}
