import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/**
 * Powers of 1024 with the short suffixes, matching every other tool on the box.
 *
 * Shared rather than copied: two screens now show byte counts from the same
 * endpoint — the gateway's per-device list and the overview's total — and a
 * second convention would make two numbers about the same traffic disagree.
 */
export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  const units = ['KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  let value = n / 1024
  let i = 0
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024
    i++
  }
  return `${value.toFixed(1)} ${units[i]}`
}

/**
 * A rate, given in bytes per second, written the way a router is read: in
 * bits. Link speeds and every provider's plan are quoted in Mbps, so a byte
 * rate beside them would make the operator do the ×8 in their head.
 */
export function formatRate(bytesPerSecond: number): string {
  const bits = bytesPerSecond * 8
  if (bits < 1000) return `${Math.round(bits)} bps`
  const units = ['kbps', 'Mbps', 'Gbps', 'Tbps']
  let value = bits / 1000
  let i = 0
  while (value >= 1000 && i < units.length - 1) {
    value /= 1000
    i++
  }
  return `${value < 10 ? value.toFixed(1) : Math.round(value)} ${units[i]}`
}
