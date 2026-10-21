/**
 * The three verdicts a status line is allowed to state, and the two ways the
 * page draws one.
 *
 * Shared rather than copied: the interface cards and the two stacks below them
 * put a state beside a name, and two vocabularies for "fine / look here /
 * nothing to say" would make the same box say two things.
 */
export const tone = {
  good: 'text-success-foreground',
  caution: 'text-warning-foreground',
  quiet: 'text-muted-foreground',
} as const

export const dot = {
  good: 'bg-success',
  caution: 'bg-warning',
  quiet: 'bg-muted-foreground/40',
} as const

export interface Health {
  label: string
  tone: keyof typeof tone
}

/** A stage with nothing to report is still a stage; this is how it says so. */
export const reading: Health = { label: 'Reading…', tone: 'quiet' }
