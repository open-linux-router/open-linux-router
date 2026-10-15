// URL normalizes compressed IPv6 notation; compare the resulting 128-bit values.
function prefix(value: string): { address: bigint; bits: number } | null {
  try {
    const [ip, mask, extra] = value.trim().split('/')
    const bits = Number(mask)
    if (
      extra !== undefined ||
      mask === undefined ||
      !/^\d+$/.test(mask) ||
      bits < 0 ||
      bits > 128
    )
      return null
    const normalized = new URL(`http://[${ip}]/`).hostname.slice(1, -1)
    const halves = normalized.split('::')
    const left = halves[0] ? halves[0].split(':') : []
    const right = halves[1] ? halves[1].split(':') : []
    const groups =
      halves.length === 2
        ? [
            ...left,
            ...Array(8 - left.length - right.length).fill('0'),
            ...right,
          ]
        : left
    if (groups.length !== 8) return null
    return {
      address: groups.reduce((v, g) => (v << 16n) | BigInt(`0x${g}`), 0n),
      bits,
    }
  } catch {
    return null
  }
}

export function containsSubnet(
  allocation: string | undefined,
  subnet: string,
): boolean {
  if (!allocation) return false
  const outer = prefix(allocation),
    inner = prefix(subnet)
  if (!outer || !inner || inner.bits !== 64 || outer.bits > inner.bits)
    return false
  const shift = BigInt(128 - outer.bits)
  return (
    outer.address >> shift === inner.address >> shift &&
    (inner.address & ((1n << 64n) - 1n)) === 0n
  )
}
