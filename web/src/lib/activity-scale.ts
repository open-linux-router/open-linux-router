export const activityFloor = 10 * 1024 ** 2

export function activityThresholds(totals: number[]): number[] {
  const maximum = totals.reduce((max, total) => Number.isFinite(total) && total >= activityFloor ? Math.max(max, total) : max, 0)
  if (!maximum) return []
  const reference = Math.max(maximum, 1024 ** 3)
  return [0.25, 0.5, 0.75].map(fraction => reference * fraction)
}

export function activityLevel(total: number, thresholds: number[]): number {
  if (total <= 0 || !Number.isFinite(total)) return 0
  if (total < activityFloor) return 1
  // Boundaries belong to the lighter tier.
  return 2 + thresholds.filter(threshold => total > threshold).length
}
