import type { Item, Layout, LayoutInput } from './layout'

/**
 * The band above the router, and the two primitives every line and every box on
 * the map is measured with.
 *
 * It is a pass of its own rather than a part of each layout because it is the
 * same band on all of them: the wide map centres it on the router, the narrow
 * one starts it at the left edge where the router is, and every layout below it
 * only has to be moved down by its height. Nothing here reads the device tree.
 *
 * **Only what is on the far side of this router is in it**, which is the
 * devices dialed in over WireGuard, each joined to the router by a line of its
 * own. The observed default-route next hops used to be here too, one card
 * apiece, and that was a row of boxes above the router saying what one line of
 * Link's status says better — which interface, which family, which address —
 * while pushing every group on the map a third of a screen down. So two
 * default routes read as two more things in the network. Where traffic leaves
 * is not its shape; the devices that reached it from outside are.
 */

// A band box is a device row with its own outline: the detail node's row height
// and a width inside its 232–288 range, so a peer and a device two rows below it
// are the same object in two places. The width leans long because the state
// column and the picture both take their share of it and what is left is the
// name — the one thing on the card a person recognises.
const CARD_H = 56
const CARD_W = 272
const BAND_ROW_GAP = 28
const BAND_DROP = 48

/** Line thickness, from a share of the heaviest sibling's traffic. */
export const EDGE_MIN = 1.5
export const EDGE_MAX = 5

export interface Box {
  x: number
  y: number
  w: number
  h: number
}

export interface Link {
  key: string
  x0: number
  y0: number
  x1: number
  y1: number
  width: number
  /** For links to something that is not a group of the operator's. */
  dashed?: boolean
  /** For links to groups set aside while another is focused. */
  faint?: boolean
}

export function chunk<T>(list: T[], size: number): T[][] {
  const out: T[][] = []
  for (let i = 0; i < list.length; i += size) out.push(list.slice(i, i + size))
  return out
}

/**
 * A line from the bottom of one box to the top-centre of another.
 *
 * Lines leave from along the parent's bottom edge, shifted toward where they
 * are heading, rather than all from one point: from one point the thick ones
 * lie on top of each other for their first inch.
 */
export function link(key: string, from: Box, to: Box, width: number, opts: { dashed?: boolean; faint?: boolean } = {}): Link {
  const px = from.x + from.w / 2
  const cx = to.x + to.w / 2
  const reach = Math.max(0, from.w / 2 - 16)
  const x0 = px + Math.max(-reach, Math.min(reach, (cx - px) * 0.15))
  // A dashed line is drawn no thicker than a hair over the thin one: thick
  // dashes read as a row of beads, not as a line with a weight.
  const w = opts.dashed ? Math.min(width, 2) : width
  return { key: `l:${key}`, x0, y0: from.y + from.h, x1: cx, y1: to.y, width: w, ...opts }
}

/**
 * Thickness from a share of the heaviest sibling's traffic, square-rooted so a
 * group doing a tenth of the work still reads as a line and not a hair. With
 * no rate every line is the thin one, which is the honest drawing of "we do
 * not know".
 */
export function edgeWidth(weight: number, heaviest: number, rated: boolean) {
  if (!rated || heaviest <= 0) return EDGE_MIN
  return EDGE_MIN + (EDGE_MAX - EDGE_MIN) * Math.sqrt(Math.max(0, weight) / heaviest)
}

/** One card in the band over the router, before it has a place. */
type BandNode = { kind: 'dialin'; key: string; w: number; h: number }

/**
 * Everything above the router, in rows, and everything else moved down to make
 * room. Done after the rest is laid out rather than inside each layout, because
 * it is the same band on every one of them.
 *
 * A band line is the thin one: a peer's card carries no traffic rate, and
 * thickness here would be a measurement the map does not have — every line in
 * this band used to be drawn at the widest one instead, which said nothing at
 * all. A peer that is asleep, or has never connected, is not a fault of the
 * network, and its card says which of the four states it is in.
 */
export function withBand(below: Layout, input: LayoutInput): Layout {
  const A = input.width
  const gap = below.narrow ? 12 : 40

  const boxes: Omit<BandNode, 'w' | 'h'>[] = []
  for (const peer of input.dialIn?.peers ?? []) boxes.push({ kind: 'dialin', key: `p:${peer.key}` })

  const rows: BandNode[][] = []
  if (boxes.length) {
    const perRow = Math.max(1, Math.floor((A + gap) / (CARD_W + gap)))
    for (const row of chunk(boxes, perRow)) {
      const w = Math.min(CARD_W, (A - (row.length - 1) * gap) / row.length)
      rows.push(row.map((node) => ({ ...node, w, h: CARD_H })))
    }
  }
  if (rows.length === 0) return below

  const heights = rows.map((row) => Math.max(...row.map((n) => n.h)))
  const band = heights.reduce((a, b) => a + b, 0) + (rows.length - 1) * BAND_ROW_GAP + BAND_DROP

  const down = (b: Box): Box => ({ ...b, y: b.y + band })
  const router = down(below.router)
  const centre = router.x + router.w / 2
  const items: Item[] = []
  const links: Link[] = []

  // Each row is centred on the router, with one direct line per box in it.
  const placed: { node: BandNode; box: Box }[][] = []
  let y = 0
  rows.forEach((row, r) => {
    const rowW = row.reduce((sum, n) => sum + n.w, 0) + (row.length - 1) * gap
    const wanted = centre - rowW / 2
    const x0 = below.narrow ? 0 : Math.max(0, Math.min(A - rowW, wanted))
    let x = x0
    placed.push(
      row.map((node) => {
        const box = { x, y, w: node.w, h: node.h }
        x += node.w + gap
        return { node, box }
      }),
    )
    y += heights[r] + BAND_ROW_GAP
  })

  for (const row of placed) {
    for (const { node, box } of row) {
      items.push({ kind: 'dialin', key: node.key, box })
    }
  }

  for (const row of placed) {
    for (const { node, box } of row) {
      links.push(link(node.key, box, router, EDGE_MIN))
    }
  }

  for (const item of below.items) items.push({ ...item, box: down(item.box) } as Item)
  for (const l of below.links) links.push({ ...l, y0: l.y0 + band, y1: l.y1 + band })
  const rail = below.rail && {
    ...below.rail,
    y0: below.rail.y0 + band,
    y1: below.rail.y1 + band,
    dots: below.rail.dots.map((d) => ({ ...d, y: d.y + band })),
  }
  return { ...below, height: below.height + band, router, items, links, rail }
}
