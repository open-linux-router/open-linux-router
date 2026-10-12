import { ChevronRight } from 'lucide-react'
import { Link } from 'react-router'

import { TOOL_DEFINITIONS } from '@/features/tools/catalog'

export function ToolsPage() {
  return (
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
      {TOOL_DEFINITIONS.map(tool => (
        <Link key={tool.slug} to={`/tools/${tool.slug}`} className="group flex min-h-26 min-w-0 items-center gap-4 rounded-xl border bg-card p-4 text-card-foreground shadow-xs transition-colors duration-150 hover:border-primary/50 hover:bg-accent/30 active:bg-accent/50 focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring sm:min-h-40 sm:p-5">
          <img src={`/tool-illustrations/${tool.slug}.webp`} alt="" width={80} height={80} className="size-16 shrink-0 object-contain sm:size-20" decoding="async" />
          <h2 className="min-w-0 flex-1 text-lg leading-6 font-semibold">{tool.directoryLabel}</h2>
          <ChevronRight className="size-4 shrink-0 text-muted-foreground group-hover:text-primary" aria-hidden />
        </Link>
      ))}
    </div>
  )
}
