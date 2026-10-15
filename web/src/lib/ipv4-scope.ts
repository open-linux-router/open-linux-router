/** Classify only explicit private/shared ranges; other IPv4s are not assumed public. */
export function ipv4Scope(address: string): 'private' | 'cgnat' | 'other' {
  const parts = address.split('/')[0].split('.')
  if (parts.length !== 4 || parts.some(p => !/^\d{1,3}$/.test(p) || Number(p) > 255)) return 'other'
  const [a, b] = parts.map(Number)
  if (a === 10 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168)) return 'private'
  if (a === 100 && b >= 64 && b <= 127) return 'cgnat'
  return 'other'
}
