import type { FirewallStatus } from '@/lib/api-types'

/**
 * What the firewall is doing, in the two registers the interface needs it.
 *
 * `firewallSummary` is the page: a headline, the sentence that explains it and
 * the status dot. `firewallState` is the launcher tile: one line, no sentence,
 * because Advanced has nine other features to answer for. Both read the same
 * conditions so a tile cannot say "On" while the page says the rules were never
 * verified.
 */
export function firewallSummary(st: FirewallStatus): {
  headline: string
  detail: string
  dot: string
} {
  if (!st.enabled) {
    return {
      headline: 'Off',
      detail:
        'Anything that can reach this router may connect to it, and to devices with public IPv6 addresses.',
      dot: 'bg-muted-foreground/40',
    }
  }
  if (!st.known) {
    return {
      headline: 'Status unknown',
      detail: st.problem ?? 'The router could not read its own firewall rules.',
      dot: 'bg-warning',
    }
  }
  if (st.native) {
    return { headline: st.problem ? 'Native rules not verified' : st.drifted ? 'Native rules changed' : 'On — native nftables rules', detail: st.problem ?? (st.drifted ? 'The kernel table differs from the saved native rules.' : 'The saved native table is in force. Review its chains and policies on the Rules page.'), dot: st.problem || st.drifted ? 'bg-warning' : 'bg-success' }
  }
  if (st.drifted || st.problem) {
    return {
      headline: 'Rules not verified',
      detail: 'The stored setting is on, but the configured protection could not be verified.',
      dot: 'bg-warning',
    }
  }
  const blocked = st.blocked_input + st.blocked_forward
  return {
    headline: 'On',
    detail:
      blocked > 0
        ? `${blocked.toLocaleString()} packets from outside turned away since the rules were last written.`
        : 'Connections from outside are blocked unless something here asked for them.',
    dot: 'bg-success',
  }
}

/** The same answer as one line, for the Advanced directory tile. */
export function firewallState(st: FirewallStatus): { text: string; attention?: boolean } {
  if (!st.enabled) return { text: 'Off' }
  if (!st.known) return { text: 'Status unknown', attention: true }
  if (st.native) return st.problem || st.drifted ? { text: 'Native rules not verified', attention: true } : { text: 'On · native rules' }
  if (st.drifted || st.problem) return { text: 'Rules not verified', attention: true }
  return { text: 'On' }
}
