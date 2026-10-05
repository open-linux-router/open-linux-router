export interface DeviceRank {
  rate: number
  lastSeen: number
}

/** A moving device precedes quiet ones; quiet devices use last-heard time. */
export function compareDeviceRanks(a: DeviceRank, b: DeviceRank): number {
  return Number(b.rate > 0) - Number(a.rate > 0) ||
    (a.rate > 0 ? b.rate - a.rate : b.lastSeen - a.lastSeen)
}
