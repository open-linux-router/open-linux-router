import { t, useLocale } from '@/lib/i18n'
import { Cable, CircleHelp, Network, Router } from 'lucide-react'

import type { InterfaceRow } from '@/lib/api-types'
import { cn } from '@/lib/utils'

export function interfaceState(row: InterfaceRow) {
  if (!row.present) return { get label() { return t('Not found') }, tone: 'text-destructive', dot: 'bg-destructive' }
  if (row.loopback) return { get label() { return t('Loopback') }, tone: 'text-muted-foreground', dot: 'bg-muted-foreground' }
  if (!row.up) return { get label() { return t('Down') }, tone: 'text-muted-foreground', dot: 'bg-muted-foreground' }
  if (!row.running) return { get label() { return t('No cable') }, tone: 'text-warning-foreground', dot: 'bg-warning' }
  if (!row.prefixes?.length) return { get label() { return t('No address') }, tone: 'text-warning-foreground', dot: 'bg-warning' }
  return { get label() { return t('Connected') }, tone: 'text-success-foreground', dot: 'bg-success' }
}

export function InterfaceVisual({ row, uplink = false }: { row: InterfaceRow; uplink?: boolean }) {
  useLocale()
  const Icon = row.loopback ? CircleHelp : uplink ? Router : row.network ? Network : Cable
  const state = interfaceState(row)
  return (
    <span className="relative flex size-11 shrink-0 items-center justify-center rounded-xl border bg-muted/40 text-foreground" aria-hidden>
      <Icon className="size-5" strokeWidth={1.7} />
      <span className={cn('absolute -bottom-0.5 -right-0.5 size-3 rounded-full border-2 border-card', state.dot)} />
    </span>
  )
}
