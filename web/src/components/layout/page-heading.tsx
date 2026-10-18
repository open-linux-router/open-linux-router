import { ChevronLeft, ChevronRight } from 'lucide-react'
import { Link } from 'react-router'
import { Heading } from '@/components/ui/heading'

export function PageHeading({ title, description, back, breadcrumbs, action, id }: {
  id?: string
  title: React.ReactNode
  description?: React.ReactNode
  back?: { to: string; label: string }
  breadcrumbs?: { to: string; label: string }[]
  action?: React.ReactNode
}) {
  return (
    <header className="space-y-3">
      {breadcrumbs ? (
        <nav aria-label="Breadcrumb" className="flex flex-wrap items-center gap-1 text-sm text-muted-foreground">
          {breadcrumbs.map((item, index) => (
            <span key={item.to} className="inline-flex items-center gap-1">
              {index > 0 && <ChevronRight className="size-3.5" aria-hidden />}
              <Link to={item.to} className="inline-flex min-h-11 items-center rounded-md px-1 hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring">{item.label}</Link>
            </span>
          ))}
        </nav>
      ) : back && (
        <Link to={back.to} className="-ml-1 inline-flex min-h-11 items-center gap-1 rounded-md px-1 text-sm text-muted-foreground hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring">
          <ChevronLeft className="size-4" aria-hidden />{back.label}
        </Link>
      )}
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0 space-y-2">
          <Heading as="h1" size="lg" id={id}>{title}</Heading>
          {description && <p className="max-w-prose text-sm leading-relaxed text-muted-foreground">{description}</p>}
        </div>
        {action}
      </div>
    </header>
  )
}
