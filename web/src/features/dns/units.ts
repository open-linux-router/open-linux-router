import type { DnsStatus, DnsService } from '@/lib/api-types'

/**
 * The two backends this module drives, named once.
 *
 * Keeping the strings here rather than indexing `status.services` by position:
 * the API does promise an order (resolver first, then relay — the order they
 * have to come up in), but a page that silently mislabels which daemon is down
 * is worse than one that shows an unknown unit by its own name.
 */
export const RESOLVER_UNIT = 'olr-dns.service'
export const RELAY_UNIT = 'olr-dnsd.service'

/**
 * What to call each backend in a sentence aimed at a person.
 *
 * The split is the one thing an operator has to understand about this module:
 * olr-dnsd is what devices talk to, and unbound behind it is what actually goes
 * and finds the answer. "DNS is broken" has two causes and only one of them is
 * ours (internal/dns/print.go).
 */
const UNIT_LABEL: Record<string, string> = {
  [RESOLVER_UNIT]: 'the resolver',
  [RELAY_UNIT]: 'the DNS server',
}

export function unitLabel(unit: string): string {
  return UNIT_LABEL[unit] ?? unit
}

/** Sentence-initial form, since "the resolver restarted" needs a capital here. */
export function UnitLabel(unit: string): string {
  const label = unitLabel(unit)
  return label.charAt(0).toUpperCase() + label.slice(1)
}

export function serviceOf(status: DnsStatus | undefined, unit: string): DnsService | undefined {
  return status?.services.find((s) => s.unit === unit)
}
