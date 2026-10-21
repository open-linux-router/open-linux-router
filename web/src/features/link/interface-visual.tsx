import { t, useLocale } from '@/lib/i18n'
import { Cable, CircleHelp, Network, Router } from 'lucide-react'

import { dotClass } from '@/components/ui/status'
import type { InterfaceRow } from '@/lib/api-types'
import { linkHealth, type Health } from '@/lib/state'
import { cn } from '@/lib/utils'

/**
 * The interface's answer, in the same four words the interface cards and the
 * Gateway summary use. It used to be a fifth enum that said "Down" where the
 * cards said "No cable".
 */
export function interfaceState(row: InterfaceRow): Health {
  if (row.loopback) return { tone: 'off', label: t('Loopback') }
  return linkHealth(row)
}

export function InterfaceVisual({ row, uplink = false }: { row: InterfaceRow; uplink?: boolean }) {
  useLocale()
  const Icon = row.loopback ? CircleHelp : uplink ? Router : row.network ? Network : Cable
  const state = interfaceState(row)
  return (
    <span className="relative flex size-11 shrink-0 items-center justify-center rounded-xl border bg-muted/40 text-foreground" aria-hidden>
      <Icon className="size-5" strokeWidth={1.7} />
      <span className={cn('absolute -bottom-0.5 -right-0.5 size-3 rounded-full border-2 border-card', dotClass(state.tone))} />
    </span>
  )
}
