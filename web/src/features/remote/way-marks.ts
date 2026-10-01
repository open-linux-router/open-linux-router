import shadowsocks from '@/assets/way-marks/shadowsocks.svg'
import wireguard from '@/assets/way-marks/wireguard.svg'

/**
 * The mark for the way somebody reached this router.
 *
 * A separate vocabulary from the device icons on purpose. An operating system's
 * mark is *identity* — it says this machine runs Debian — and is therefore
 * stored on the device (see Icon and OS_MARKS). Which way a tunnel or a proxy
 * arrived is not stored on anything: it is read from the status of the thing
 * that is running, so a key here is only ever a lookup the UI does mid-render.
 * Nothing validates it, because nothing is written down.
 *
 * Full colour, and the same exception to the no-logos rule as the OS marks,
 * for the same reason: "a picture of WireGuard" is a grey box, and the knot is
 * the only thing that says which of the three ways in this connection used.
 *
 * Two marks, not three: SOCKS5 has none, and borrows the Network glyph. That is
 * a gap the ladder already handles — an undrawn mark falls back to a line
 * glyph rather than to a stand-in picture — so it needs no entry here.
 */
export type WayKey = 'wireguard' | 'shadowsocks' | 'socks5'

/**
 * WireGuard's knot from thesvg.org (MIT), Shadowsocks' paper plane from the
 * project's own artwork. Both in the house red the two brands share, which is
 * also why they sit together without either looking borrowed.
 */
export const WAY_MARKS: Partial<Record<WayKey, string>> = {
  wireguard,
  shadowsocks,
}
