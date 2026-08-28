// Response shapes for the read and apply surfaces.
//
// KNOWN GAP, worth fixing rather than living with: these are hand-written,
// while config-types.ts is generated. design.md §3.2 rule 3 makes the Go struct
// the single source for every surface, and §6.2 says the read surface includes
// observed resources "declared alongside config" — but core reflects only the
// module's *config* struct today, so the view types in internal/dhcp/view.go
// reach TypeScript by being retyped here. A field renamed in Go breaks this
// file silently at runtime rather than loudly at compile time.
//
// The fix is to have core publish response schemas next to the config schema
// and extend scripts/gen-types.mjs to consume them. Until then, this file and
// internal/dhcp/view.go must be changed together.

import type { Problem } from '@/lib/api'

/** What applying a change will cost (design.md §5.3.3, internal/dhcp Impact). */
export type Impact = 'none' | 'reload' | 'restart' | 'disruptive'

/** What the backend needs after the files are written. */
export type ServiceAction = 'none' | 'start' | 'stop' | 'reload' | 'restart'

export type ChangeKind = 'create' | 'update' | 'delete'

export interface Change {
  path: string
  kind: ChangeKind
  impact: Impact
  diff: string
}

/** The answer to "what would applying this do?" — internal/dhcp planView. */
export interface Plan {
  backend: string
  changes: Change[]
  action: ServiceAction
  impact: Impact
  /**
   * The boot-time state the unit will be moved to, absent when it already
   * matches. Separate from `action` because "running now" and "running after a
   * reboot" are different promises: a unit that is active but not enabled looks
   * healthy until the power goes out.
   */
  enable?: boolean
  reasons?: string[]
  empty: boolean
  warnings?: Problem[]
}

/** One unit of work and how it went. */
export interface Step {
  description: string
  done: boolean
  error?: string
}

/**
 * The result of an apply. Carries the steps whether it succeeded or not:
 * design.md §5.3.2 has no rollback, so a half-finished change stays
 * half-finished and the honest thing is to show which steps landed.
 */
export interface ApplyResult {
  plan: Plan
  steps?: Step[]
  error?: { message: string; problems?: Problem[] }
}

/** What systemd knows about a unit — core.UnitStatus. */
export interface UnitStatus {
  unit: string
  active: boolean
  /** Whether it starts at boot — not the same question as `active`. */
  enabled: boolean
  /**
   * Whether the unit file exists at all. Distinguished from `enabled` because
   * the two need different answers: disabled is something olr fixes on the next
   * apply, missing means the package is incomplete and applying will not help.
   */
  installed: boolean
  state: string
  sub_state?: string
  since?: string
  main_pid?: number
}

export interface DhcpStatus {
  enabled: boolean
  service?: UnitStatus
  service_error?: string
  drifted: boolean
  drift?: Plan
  drift_error?: string
  as_of: string
}

export interface Lease {
  ip: string
  mac?: string
  iaid?: string
  hostname?: string
  client_id?: string
  /** null for a lease that never expires. */
  expires: string | null
  active: boolean
}

export interface PoolUsage {
  interface: string
  size: number
  active: number
  expired: number
  free: number
  percent: number
}

export interface DhcpLeases {
  leases: Lease[]
  usage: PoolUsage[]
  problems?: Problem[]
  as_of: string
}
