import { ChevronRight, Gauge, Network, Radar, Route, Cable } from 'lucide-react'
import { Link } from 'react-router'

import { TOOL_DEFINITIONS } from '@/features/tools/catalog'

const TOOL_ICONS = { speedtest: Gauge, 'link-test': Cable, nat: Network, ping: Radar, traceroute: Route }

export function ToolsPage() {
  return (
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
      {TOOL_DEFINITIONS.map(tool => {
        const Icon = TOOL_ICONS[tool.slug]
        return <Link key={tool.slug} to={`/tools/${tool.slug}`} className="group flex min-h-26 min-w-0 items-center gap-4 panel-surface interactive-surface p-4 sm:min-h-32 sm:p-5">
          <span className="flex size-12 shrink-0 items-center justify-center rounded-xl bg-accent text-primary sm:size-14"><Icon className="size-6 sm:size-7" strokeWidth={1.75} aria-hidden /></span>
          <h2 className="min-w-0 flex-1 text-lg leading-6 font-semibold">{tool.directoryLabel}</h2>
          <ChevronRight className="size-4 shrink-0 text-muted-foreground group-hover:text-primary" aria-hidden />
        </Link>
      })}
    </div>
  )
}
