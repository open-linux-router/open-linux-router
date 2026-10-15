export const activityFloor = 10 * 1024 ** 2

export function activityThresholds(totals: number[]): number[] {
  const sorted = totals.filter(total => Number.isFinite(total) && total >= activityFloor).sort((a, b) => a - b)
  if (!sorted.length) return []
  return [0.25, 0.5, 0.75].map(fraction => {
    const index = (sorted.length - 1) * fraction
    const lower = Math.floor(index)
    return sorted[lower] + (sorted[Math.ceil(index)] - sorted[lower]) * (index - lower)
  })
}

export function activityLevel(total: number, thresholds: number[]): number {
  if (total <= 0 || !Number.isFinite(total)) return 0
  if (total < activityFloor) return 1
  // Equal totals share a color; a flat or single-sample history stays light green.
  return 2 + thresholds.filter(threshold => total > threshold).length
}
