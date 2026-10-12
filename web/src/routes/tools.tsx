import { ChevronRight } from 'lucide-react'
import { Link } from 'react-router'

import { TOOL_DEFINITIONS } from '@/features/tools/catalog'

export function ToolsPage() {
  return (
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
      {TOOL_DEFINITIONS.map(tool => (
        <Link key={tool.slug} to={`/tools/${tool.slug}`} className="group flex min-w-0 flex-col overflow-hidden rounded-2xl border bg-card text-card-foreground shadow-xs transition-colors duration-150 hover:border-primary/50 active:bg-accent/50 focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring">
          <div className="flex items-center justify-between gap-4 border-b bg-muted/40 px-5 py-3">
            <span className="text-xs font-medium text-muted-foreground">{tool.category}</span>
            <img src={`/tool-illustrations/${tool.slug}.webp`} alt="" width={128} height={128} className="size-28 object-contain" decoding="async" />
          </div>
          <div className="flex flex-1 flex-col p-5 sm:p-6">
            <h2 className="text-base leading-6 font-semibold">{tool.label}</h2>
            <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{tool.description}</p>
            <div className="mt-auto flex items-center justify-between gap-3 pt-5">
              <span className="text-[0.8125rem] text-muted-foreground">{tool.detail}</span>
              <ChevronRight className="size-4 shrink-0 text-primary" aria-hidden />
            </div>
          </div>
        </Link>
      ))}
    </div>
  )
}
