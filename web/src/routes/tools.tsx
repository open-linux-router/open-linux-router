import { ArrowUpRight } from 'lucide-react'
import { Link } from 'react-router'

import { TOOL_DEFINITIONS } from '@/features/tools/catalog'

export function ToolsPage() {
  return (
    <div className="space-y-6">
      <p className="max-w-prose text-sm leading-relaxed text-muted-foreground">A closer look at your connection. Choose a tool to test speed, troubleshoot reachability or explore a route.</p>
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {TOOL_DEFINITIONS.map(tool => (
          <Link key={tool.slug} to={`/tools/${tool.slug}`} className="group flex min-w-0 flex-col overflow-hidden rounded-2xl border bg-card text-card-foreground transition-colors duration-200 hover:border-primary/40 focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring motion-reduce:transition-none">
            <div className="relative flex h-48 items-center justify-center overflow-hidden bg-[#f7f7f7] sm:h-52">
              <img src={`/tool-illustrations/${tool.slug}.webp`} alt="" width={384} height={384} className="size-48 object-contain sm:size-52" decoding="async" />
              <span className="absolute right-4 top-4 flex size-9 items-center justify-center rounded-full border border-black/5 bg-white/80 text-neutral-500 transition-colors group-hover:text-primary motion-reduce:transition-none"><ArrowUpRight className="size-4" aria-hidden /></span>
            </div>
            <div className="flex flex-1 flex-col p-5 sm:p-6">
              <p className="text-xs font-medium text-muted-foreground">{tool.category}</p>
              <h2 className="mt-2 text-lg font-semibold tracking-tight">{tool.label}</h2>
              <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{tool.description}</p>
              <p className="mt-auto pt-6 text-xs font-medium text-primary">{tool.detail}</p>
            </div>
          </Link>
        ))}
      </div>
    </div>
  )
}
