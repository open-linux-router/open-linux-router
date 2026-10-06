import { Cable, CircleHelp, Network, Router } from 'lucide-react'

import type { InterfaceRow } from '@/lib/api-types'
import { cn } from '@/lib/utils'

export function interfaceState(row: InterfaceRow) {
  if (!row.present) return { label: 'Not found', tone: 'text-destructive', dot: 'bg-destructive' }
  if (row.loopback) return { label: 'Loopback', tone: 'text-muted-foreground', dot: 'bg-muted-foreground' }
  if (!row.up) return { label: 'Down', tone: 'text-muted-foreground', dot: 'bg-muted-foreground' }
  if (!row.running) return { label: 'No cable', tone: 'text-warning-foreground', dot: 'bg-warning' }
  if (!row.prefixes?.length) return { label: 'No address', tone: 'text-warning-foreground', dot: 'bg-warning' }
  return { label: 'Connected', tone: 'text-success-foreground', dot: 'bg-success' }
}

export function InterfaceVisual({ row, uplink = false }: { row: InterfaceRow; uplink?: boolean }) {
  const Icon = row.loopback ? CircleHelp : uplink ? Router : row.network ? Network : Cable
  const state = interfaceState(row)
  return (
    <span className="relative flex size-11 shrink-0 items-center justify-center rounded-xl border bg-muted/40 text-foreground" aria-hidden>
      <Icon className="size-5" strokeWidth={1.7} />
      <span className={cn('absolute -bottom-0.5 -right-0.5 size-3 rounded-full border-2 border-card', state.dot)} />
    </span>
  )
}
