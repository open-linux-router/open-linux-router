/**
 * The headline a switched-on module gets while its saved settings no longer
 * validate. One object, so a page can tell it apart from its own summaries —
 * StuckSettings, in the file beside this one, says why the state needs one.
 */
export const STUCK_SUMMARY = {
  headline: 'Running on old settings',
  detail: 'What is saved cannot be put in force — see below.',
  dot: 'bg-warning',
}
